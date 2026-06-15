package remote

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Connector abre conexões com um servidor Prisma. É o que o db.Open() entrega
// ao sql.OpenDB() no modo cliente. Cada conexão lógica do pool vira uma sessão
// dedicada no servidor.
type Connector struct {
	baseURL string
	token   string
	http    *http.Client
}

// NovoConnector monta o conector a partir da config do cliente.
func NovoConnector(cfg Config) *Connector {
	tr := &http.Transport{}
	if cfg.TLS {
		tr.TLSClientConfig = tlsConfigCliente(cfg.Fingerprint)
	}
	return &Connector{
		baseURL: cfg.baseURL(),
		token:   cfg.Token,
		// Timeout generoso: é LAN, mas uma query grande não pode estourar cedo.
		http: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}
}

// Connect abre uma sessão no servidor e devolve a conexão.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	var resp RespOpen
	if err := c.post(ctx, RotaOpen, nil, &resp); err != nil {
		return nil, err
	}
	return &conexao{c: c, sessionID: resp.SessionID}, nil
}

// Driver devolve o driver associado (exigido pela interface).
func (c *Connector) Driver() driver.Driver { return remoteDriver{} }

// remoteDriver existe só para satisfazer driver.Driver; o caminho real é via
// Connector, então Open não é usado na prática.
type remoteDriver struct{}

func (remoteDriver) Open(name string) (driver.Conn, error) {
	return nil, fmt.Errorf("remote: use db.Open com modo=cliente, não sql.Open")
}

// post envia uma requisição JSON e decodifica a resposta. reqBody pode ser nil.
func (c *Connector) post(ctx context.Context, rota string, reqBody, respBody any) error {
	var corpo io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		corpo = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+rota, corpo)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderToken, c.token)

	res, err := c.http.Do(req)
	if err != nil {
		// Erro de rede: o caso comum é o servidor estar fora do ar.
		return fmt.Errorf("não foi possível falar com o servidor Prisma (%s): %w", c.baseURL, err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var e RespErro
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		_ = json.Unmarshal(body, &e)
		if e.Erro == "" {
			e.Erro = strings.TrimSpace(string(body))
		}
		if res.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("servidor recusou o token (verifique PRISMA_TOKEN)")
		}
		return fmt.Errorf("servidor: %s", e.Erro)
	}
	if respBody == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(respBody)
}

// conexao é uma conexão lógica (driver.Conn): uma sessão no servidor.
type conexao struct {
	c         *Connector
	sessionID string
}

var (
	_ driver.Conn               = (*conexao)(nil)
	_ driver.Pinger             = (*conexao)(nil)
	_ driver.ExecerContext      = (*conexao)(nil)
	_ driver.QueryerContext     = (*conexao)(nil)
	_ driver.ConnBeginTx        = (*conexao)(nil)
	_ driver.ConnPrepareContext = (*conexao)(nil)
)

func (cx *conexao) Ping(ctx context.Context) error {
	return cx.c.post(ctx, RotaPing, ReqSessao{SessionID: cx.sessionID}, nil)
}

func (cx *conexao) Close() error {
	return cx.c.post(context.Background(), RotaClose, ReqSessao{SessionID: cx.sessionID}, nil)
}

// namedToArgs ordena os parâmetros nomeados pela posição. O Prisma só usa
// placeholders posicionais (?), então o Ordinal manda.
func namedToArgs(named []driver.NamedValue) []any {
	args := make([]any, len(named))
	for _, nv := range named {
		if nv.Ordinal >= 1 && nv.Ordinal <= len(args) {
			args[nv.Ordinal-1] = nv.Value
		}
	}
	return args
}

func (cx *conexao) ExecContext(ctx context.Context, query string, named []driver.NamedValue) (driver.Result, error) {
	req := ReqExec{SessionID: cx.sessionID, SQL: query, Args: CodificaArgs(namedToArgs(named))}
	var resp RespExec
	if err := cx.c.post(ctx, RotaExec, req, &resp); err != nil {
		return nil, err
	}
	return resultado{lastID: resp.LastInsertID, linhas: resp.RowsAffected}, nil
}

func (cx *conexao) QueryContext(ctx context.Context, query string, named []driver.NamedValue) (driver.Rows, error) {
	req := ReqExec{SessionID: cx.sessionID, SQL: query, Args: CodificaArgs(namedToArgs(named))}
	var resp RespQuery
	if err := cx.c.post(ctx, RotaQuery, req, &resp); err != nil {
		return nil, err
	}
	return &linhas{colunas: resp.Colunas, dados: resp.Linhas}, nil
}

func (cx *conexao) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := cx.c.post(ctx, RotaBegin, ReqSessao{SessionID: cx.sessionID}, nil); err != nil {
		return nil, err
	}
	return &transacao{cx: cx}, nil
}

// Begin (sem contexto) delega para BeginTx — exigido por driver.Conn.
func (cx *conexao) Begin() (driver.Tx, error) {
	return cx.BeginTx(context.Background(), driver.TxOptions{})
}

func (cx *conexao) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return &comando{cx: cx, sql: query}, nil
}

// Prepare (sem contexto) — exigido por driver.Conn.
func (cx *conexao) Prepare(query string) (driver.Stmt, error) {
	return cx.PrepareContext(context.Background(), query)
}

// transacao implementa driver.Tx; o estado real vive no servidor, atrelado à
// sessão. Commit/Rollback só sinalizam.
type transacao struct{ cx *conexao }

func (t *transacao) Commit() error {
	return t.cx.c.post(context.Background(), RotaCommit, ReqSessao{SessionID: t.cx.sessionID}, nil)
}

func (t *transacao) Rollback() error {
	return t.cx.c.post(context.Background(), RotaRollback, ReqSessao{SessionID: t.cx.sessionID}, nil)
}

// comando implementa driver.Stmt delegando para a conexão. O servidor prepara
// por chamada, então não há ciclo de vida de statement no fio.
type comando struct {
	cx  *conexao
	sql string
}

var (
	_ driver.Stmt             = (*comando)(nil)
	_ driver.StmtExecContext  = (*comando)(nil)
	_ driver.StmtQueryContext = (*comando)(nil)
)

func (s *comando) Close() error  { return nil }
func (s *comando) NumInput() int { return -1 } // não validamos a aridade aqui

func (s *comando) ExecContext(ctx context.Context, named []driver.NamedValue) (driver.Result, error) {
	return s.cx.ExecContext(ctx, s.sql, named)
}

func (s *comando) QueryContext(ctx context.Context, named []driver.NamedValue) (driver.Rows, error) {
	return s.cx.QueryContext(ctx, s.sql, named)
}

// Exec/Query legados (sem contexto) — exigidos por driver.Stmt.
func (s *comando) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), valuesToNamed(args))
}

func (s *comando) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valuesToNamed(args))
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return named
}

// resultado implementa driver.Result.
type resultado struct {
	lastID int64
	linhas int64
}

func (r resultado) LastInsertId() (int64, error) { return r.lastID, nil }
func (r resultado) RowsAffected() (int64, error) { return r.linhas, nil }

// linhas implementa driver.Rows sobre o conjunto já materializado.
type linhas struct {
	colunas []string
	dados   [][]Valor
	pos     int
}

func (l *linhas) Columns() []string { return l.colunas }
func (l *linhas) Close() error      { return nil }

func (l *linhas) Next(dest []driver.Value) error {
	if l.pos >= len(l.dados) {
		return io.EOF
	}
	linha := l.dados[l.pos]
	l.pos++
	for i := range dest {
		if i >= len(linha) {
			dest[i] = nil
			continue
		}
		v, err := linha[i].Decodifica()
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}
