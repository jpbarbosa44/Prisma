package remote

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// idleSessao é quanto tempo uma sessão pode ficar sem uso antes do servidor
// fechá-la sozinho — protege contra clientes que caem sem fechar a conexão.
const idleSessao = 10 * time.Minute

// maxCorpo limita o tamanho do corpo de cada requisição. Os comandos do Prisma
// são pequenos; o teto generoso (8 MiB) ainda cobre uma importação grande, mas
// barra um corpo absurdo que esgotaria a memória do servidor.
const maxCorpo = 8 << 20

// Servidor expõe um *sql.DB local para clientes Prisma na rede. Cada sessão
// segura uma *sql.Conn dedicada, preservando a semântica de transação por
// conexão.
type Servidor struct {
	db    *sql.DB
	token string

	mu       sync.Mutex
	sessoes  map[string]*sessao
	encerrar chan struct{}
}

type sessao struct {
	mu     sync.Mutex
	conn   *sql.Conn
	tx     *sql.Tx
	ultimo time.Time
	// ctx vive enquanto a sessão existe. A transação é aberta com ele (e não
	// com o contexto da requisição /begin) porque ela atravessa várias
	// requisições — atrelá-la ao request faria o database/sql dar rollback
	// automático assim que o /begin retornasse.
	ctx      context.Context
	cancelar context.CancelFunc
}

// NovoServidor cria o servidor sobre um banco já aberto.
func NovoServidor(db *sql.DB, token string) *Servidor {
	return &Servidor{
		db:       db,
		token:    token,
		sessoes:  make(map[string]*sessao),
		encerrar: make(chan struct{}),
	}
}

// Handler monta o roteamento HTTP do servidor.
func (s *Servidor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(RotaPing, s.protegido(s.handlePing))
	mux.HandleFunc(RotaOpen, s.protegido(s.handleOpen))
	mux.HandleFunc(RotaClose, s.protegido(s.handleClose))
	mux.HandleFunc(RotaExec, s.protegido(s.handleExec))
	mux.HandleFunc(RotaQuery, s.protegido(s.handleQuery))
	mux.HandleFunc(RotaBegin, s.protegido(s.handleBegin))
	mux.HandleFunc(RotaCommit, s.protegido(s.handleCommit))
	mux.HandleFunc(RotaRollback, s.protegido(s.handleRollback))
	return mux
}

// Serve escuta no endereço e atende até o contexto ser cancelado.
func (s *Servidor) Serve(ctx context.Context, ln net.Listener) error {
	go s.reaper()
	defer close(s.encerrar)

	srv := &http.Server{
		Handler: s.Handler(),
		// ReadHeaderTimeout corta o slowloris (cliente que envia cabeçalhos
		// byte a byte para segurar a conexão); IdleTimeout recolhe conexões
		// keep-alive paradas. Read/Write ficam sem teto de propósito: uma query
		// legítima grande na LAN não pode ser cortada no meio.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	err := srv.Serve(ln)
	s.fechaTudo()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// protegido envolve um handler exigindo o token e o método POST.
func (s *Servidor) protegido(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
			return
		}
		recebido := r.Header.Get(HeaderToken)
		if subtle.ConstantTimeCompare([]byte(recebido), []byte(s.token)) != 1 {
			s.erro(w, http.StatusUnauthorized, "token inválido")
			return
		}
		h(w, r)
	}
}

func (s *Servidor) handlePing(w http.ResponseWriter, r *http.Request) {
	var req ReqSessao
	if !s.decode(w, r, &req) {
		return
	}
	// Ping sem sessão (id vazio) serve de health check de pareamento.
	if req.SessionID == "" {
		s.ok(w, nil)
		return
	}
	ses := s.sessao(req.SessionID)
	if ses == nil {
		s.erro(w, http.StatusNotFound, "sessão desconhecida")
		return
	}
	// sob o lock da sessão: `conn` pode ser zerado por fecha()/reaper, e `ultimo`
	// (escrito aqui) é lido pelo reaper — sem o lock seria corrida de dados.
	ses.mu.Lock()
	defer ses.mu.Unlock()
	ses.toca()
	if ses.conn == nil {
		s.erro(w, http.StatusNotFound, "sessão encerrada")
		return
	}
	if err := ses.conn.PingContext(r.Context()); err != nil {
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ok(w, nil)
}

func (s *Servidor) handleOpen(w http.ResponseWriter, r *http.Request) {
	conn, err := s.db.Conn(r.Context())
	if err != nil {
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := novoID()
	if err != nil {
		conn.Close()
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancelar := context.WithCancel(context.Background())
	s.mu.Lock()
	s.sessoes[id] = &sessao{conn: conn, ultimo: time.Now(), ctx: ctx, cancelar: cancelar}
	s.mu.Unlock()
	s.ok(w, RespOpen{SessionID: id})
}

func (s *Servidor) handleClose(w http.ResponseWriter, r *http.Request) {
	var req ReqSessao
	if !s.decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	ses := s.sessoes[req.SessionID]
	delete(s.sessoes, req.SessionID)
	s.mu.Unlock()
	if ses != nil {
		ses.fecha()
	}
	s.ok(w, nil)
}

func (s *Servidor) handleExec(w http.ResponseWriter, r *http.Request) {
	ses, req, ok := s.sessaoEReq(w, r)
	if !ok {
		return
	}
	args, err := DecodificaArgs(req.Args)
	if err != nil {
		s.erro(w, http.StatusBadRequest, err.Error())
		return
	}
	ses.mu.Lock()
	defer ses.mu.Unlock()
	res, err := ses.exec(r.Context(), req.SQL, args)
	if err != nil {
		s.erro(w, http.StatusBadRequest, err.Error())
		return
	}
	lastID, _ := res.LastInsertId()
	linhas, _ := res.RowsAffected()
	s.ok(w, RespExec{LastInsertID: lastID, RowsAffected: linhas})
}

func (s *Servidor) handleQuery(w http.ResponseWriter, r *http.Request) {
	ses, req, ok := s.sessaoEReq(w, r)
	if !ok {
		return
	}
	args, err := DecodificaArgs(req.Args)
	if err != nil {
		s.erro(w, http.StatusBadRequest, err.Error())
		return
	}
	ses.mu.Lock()
	defer ses.mu.Unlock()
	rows, err := ses.query(r.Context(), req.SQL, args)
	if err != nil {
		s.erro(w, http.StatusBadRequest, err.Error())
		return
	}
	defer rows.Close()

	resp, err := materializa(rows)
	if err != nil {
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ok(w, resp)
}

func (s *Servidor) handleBegin(w http.ResponseWriter, r *http.Request) {
	var req ReqSessao
	if !s.decode(w, r, &req) {
		return
	}
	ses := s.sessao(req.SessionID)
	if ses == nil {
		s.erro(w, http.StatusNotFound, "sessão desconhecida")
		return
	}
	ses.mu.Lock()
	defer ses.mu.Unlock()
	ses.toca()
	if ses.tx != nil {
		s.erro(w, http.StatusConflict, "transação já aberta")
		return
	}
	tx, err := ses.conn.BeginTx(ses.ctx, nil)
	if err != nil {
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	ses.tx = tx
	s.ok(w, nil)
}

func (s *Servidor) handleCommit(w http.ResponseWriter, r *http.Request)   { s.finalizaTx(w, r, true) }
func (s *Servidor) handleRollback(w http.ResponseWriter, r *http.Request) { s.finalizaTx(w, r, false) }

func (s *Servidor) finalizaTx(w http.ResponseWriter, r *http.Request, commit bool) {
	var req ReqSessao
	if !s.decode(w, r, &req) {
		return
	}
	ses := s.sessao(req.SessionID)
	if ses == nil {
		s.erro(w, http.StatusNotFound, "sessão desconhecida")
		return
	}
	ses.mu.Lock()
	defer ses.mu.Unlock()
	ses.toca()
	if ses.tx == nil {
		s.erro(w, http.StatusConflict, "nenhuma transação aberta")
		return
	}
	var err error
	if commit {
		err = ses.tx.Commit()
	} else {
		err = ses.tx.Rollback()
	}
	ses.tx = nil
	if err != nil {
		s.erro(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ok(w, nil)
}

// --- helpers de sessão ---

// exec/query roteiam para a transação aberta, se houver.
func (se *sessao) exec(ctx context.Context, q string, args []any) (sql.Result, error) {
	se.toca()
	if se.tx != nil {
		return se.tx.ExecContext(ctx, q, args...)
	}
	return se.conn.ExecContext(ctx, q, args...)
}

func (se *sessao) query(ctx context.Context, q string, args []any) (*sql.Rows, error) {
	se.toca()
	if se.tx != nil {
		return se.tx.QueryContext(ctx, q, args...)
	}
	return se.conn.QueryContext(ctx, q, args...)
}

func (se *sessao) toca() { se.ultimo = time.Now() }

func (se *sessao) fecha() {
	se.mu.Lock()
	defer se.mu.Unlock()
	if se.tx != nil {
		_ = se.tx.Rollback()
		se.tx = nil
	}
	if se.conn != nil {
		_ = se.conn.Close()
		se.conn = nil
	}
	if se.cancelar != nil {
		se.cancelar()
		se.cancelar = nil
	}
}

func (s *Servidor) sessao(id string) *sessao {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessoes[id]
}

func (s *Servidor) sessaoEReq(w http.ResponseWriter, r *http.Request) (*sessao, ReqExec, bool) {
	var req ReqExec
	if !s.decode(w, r, &req) {
		return nil, req, false
	}
	ses := s.sessao(req.SessionID)
	if ses == nil {
		s.erro(w, http.StatusNotFound, "sessão desconhecida")
		return nil, req, false
	}
	return ses, req, true
}

// materializa lê todas as linhas de um *sql.Rows para a forma de fio.
func materializa(rows *sql.Rows) (RespQuery, error) {
	cols, err := rows.Columns()
	if err != nil {
		return RespQuery{}, err
	}
	resp := RespQuery{Colunas: cols, Linhas: [][]Valor{}}
	for rows.Next() {
		celulas := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range celulas {
			ptrs[i] = &celulas[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return RespQuery{}, err
		}
		linha := make([]Valor, len(cols))
		for i, c := range celulas {
			linha[i] = CodificaValor(c)
		}
		resp.Linhas = append(resp.Linhas, linha)
	}
	return resp, rows.Err()
}

// reaper fecha sessões ociosas periodicamente.
func (s *Servidor) reaper() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.encerrar:
			return
		case <-t.C:
			limite := time.Now().Add(-idleSessao)
			s.mu.Lock()
			for id, ses := range s.sessoes {
				// `ultimo` é escrito sob ses.mu pelos handlers; lê-se igual.
				ses.mu.Lock()
				ocioso := ses.ultimo.Before(limite)
				ses.mu.Unlock()
				if ocioso {
					delete(s.sessoes, id)
					go ses.fecha()
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Servidor) fechaTudo() {
	s.mu.Lock()
	sessoes := s.sessoes
	s.sessoes = make(map[string]*sessao)
	s.mu.Unlock()
	for _, ses := range sessoes {
		ses.fecha()
	}
}

// --- helpers HTTP ---

func (s *Servidor) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCorpo)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		s.erro(w, http.StatusBadRequest, "corpo inválido: "+err.Error())
		return false
	}
	return true
}

func (s *Servidor) ok(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	} else {
		_, _ = w.Write([]byte("{}"))
	}
}

func (s *Servidor) erro(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(RespErro{Erro: msg})
}

func novoID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gerando id de sessão: %w", err)
	}
	return hex.EncodeToString(b), nil
}
