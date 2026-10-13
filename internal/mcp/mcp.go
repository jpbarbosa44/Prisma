// Package mcp expõe os dados do Prisma a assistentes de IA (Claude Code,
// Claude Desktop e outros clientes MCP) pelo Model Context Protocol, no
// transporte stdio: JSON-RPC 2.0, uma mensagem por linha no stdin/stdout.
//
// É somente leitura: o servidor roda sobre a conexão query_only do Analytics
// e cada ferramenta monta os argumentos das funções do app a partir de
// parâmetros tipados e validados — nada que o modelo escreva vira subcomando.
//
// Cuidado com o stdout: no stdio ele É o canal do protocolo, e as funções do
// app imprimem nele (a captura troca os.Stdout durante a chamada). Por isso
// Servir recebe o writer original já guardado e atende uma requisição por vez.
package mcp

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"prisma/internal/update"
)

// versaoProtocolo é a revisão do MCP que o servidor fala quando o cliente não
// pede outra.
const versaoProtocolo = "2025-06-18"

// instrucoes orientam o modelo sobre como usar as ferramentas.
const instrucoes = `Ferramentas de consulta às finanças pessoais do usuário no Prisma (somente leitura).
Valores aparecem em reais (R$). Datas no formato AAAA-MM-DD; meses como AAAA-MM.
Prefira as ferramentas específicas (saldo, lancamentos, relatorio, previsao, analise...):
elas já aplicam as regras de cartão, parcelas, grupos e transferências. Use consulta_sql
só quando nenhuma delas responder, e consulte schema antes.`

// instrucoesEscrita completam as instruções quando a sessão pode gravar.
const instrucoesEscrita = `

Esta sessão também CADASTRA dados (prisma mcp --escrita). Para importar um extrato ou fatura:
1. ler_extrato com o caminho do arquivo (CSV/OFX/TXT) e interprete as colunas;
2. consulte saldo (contas), cartoes e categorias para reaproveitar o que já existe;
3. importar_lancamentos SEM confirmar: revise a prévia (duplicados, categorias novas);
4. mostre o resumo ao usuário e, com o aval dele (ou se ele já autorizou), repita com confirmar=true.
Prefira categorias existentes; crie novas só quando nada servir, com nomes curtos em minúsculas,
e defina a classe com definir_categoria. Use criar_recorrencia para assinaturas e contas fixas
que vão continuar (o histórico já vem do extrato). Cada lote pode ser revertido com desfazer_importacao.`

func (s *Sessao) instrucoes() string {
	if s.escrita == nil {
		return instrucoes
	}
	return instrucoes + instrucoesEscrita
}

// limiteSaida corta respostas muito grandes (um extrato sem filtro, por ex.)
// para não estourar o contexto do modelo.
const limiteSaida = 60_000

// Códigos de erro do JSON-RPC 2.0.
const (
	erroParse      = -32700
	erroMetodo     = -32601
	erroParametros = -32602
)

type requisicao struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type resposta struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *erroRPC        `json:"error,omitempty"`
}

type erroRPC struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Sessao é o estado do servidor durante uma conexão MCP.
type Sessao struct {
	leitura  *sql.DB // consultas; query_only quando o banco é local
	escrita  *sql.DB // nil = servidor somente leitura
	sqlLivre bool    // consulta_sql só existe sobre uma conexão query_only
	banco    string  // caminho do banco local, para o snapshot antes de gravar ("" = remoto)
	snapshot string  // backup tirado antes do primeiro lote desta sessão
}

// Servir atende um cliente MCP somente leitura sobre conn (query_only).
func Servir(conn *sql.DB, in io.Reader, out io.Writer) error {
	return (&Sessao{leitura: conn, sqlLivre: true}).Servir(in, out)
}

// ServirComEscrita atende um cliente MCP que também pode cadastrar dados
// (`prisma mcp --escrita`). leitura atende as consultas e escrita as
// gravações. sqlLivre só deve ser true se leitura for query_only; banco é o
// arquivo local para o snapshot antes da primeira gravação ("" se remoto).
func ServirComEscrita(leitura, escrita *sql.DB, sqlLivre bool, banco string, in io.Reader, out io.Writer) error {
	s := &Sessao{leitura: leitura, escrita: escrita, sqlLivre: sqlLivre, banco: banco}
	return s.Servir(in, out)
}

// disponivel diz se a ferramenta existe nesta sessão.
func (s *Sessao) disponivel(f ferramenta) bool {
	if f.escreve && s.escrita == nil {
		return false
	}
	return f.nome != "consulta_sql" || s.sqlLivre
}

// Servir atende requisições MCP lidas de in até EOF, escrevendo as respostas
// em out. Processa uma por vez — obrigatório, pois a captura das funções do
// app troca os.Stdout enquanto roda.
func (s *Sessao) Servir(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		linha := strings.TrimSpace(sc.Text())
		if linha == "" {
			continue
		}
		if r := s.trata([]byte(linha)); r != nil {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// trata responde uma mensagem; devolve nil para notificações (sem id).
func (s *Sessao) trata(linha []byte) *resposta {
	var req requisicao
	if err := json.Unmarshal(linha, &req); err != nil {
		return &resposta{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &erroRPC{erroParse, "JSON inválido: " + err.Error()}}
	}
	if len(req.ID) == 0 {
		return nil // notificação (ex.: notifications/initialized): não tem resposta
	}
	r := &resposta{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		versao := p.ProtocolVersion
		if versao == "" {
			versao = versaoProtocolo
		}
		r.Result = map[string]any{
			"protocolVersion": versao,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "prisma", "version": update.Versao},
			"instructions":    s.instrucoes(),
		}
	case "ping":
		r.Result = map[string]any{}
	case "tools/list":
		lista := make([]map[string]any, 0, len(ferramentas))
		for _, f := range ferramentas {
			if !s.disponivel(f) {
				continue
			}
			lista = append(lista, map[string]any{
				"name":        f.nome,
				"description": f.descricao,
				"inputSchema": f.esquema(),
			})
		}
		r.Result = map[string]any{"tools": lista}
	case "tools/call":
		var p struct {
			Name      string     `json:"name"`
			Arguments argumentos `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			r.Error = &erroRPC{erroParametros, "parâmetros inválidos: " + err.Error()}
			break
		}
		f, ok := buscaFerramenta(p.Name)
		if !ok || !s.disponivel(f) {
			r.Error = &erroRPC{erroParametros, "ferramenta desconhecida: " + p.Name}
			break
		}
		r.Result = s.chama(f, p.Arguments)
	default:
		r.Error = &erroRPC{erroMetodo, "método não suportado: " + req.Method}
	}
	return r
}

// chama roda a ferramenta e embrulha o resultado no formato do MCP. Erros da
// ferramenta voltam como resultado com isError (o modelo vê e corrige), não
// como erro de protocolo.
func (s *Sessao) chama(f ferramenta, a argumentos) map[string]any {
	if a == nil {
		a = argumentos{}
	}
	var texto string
	err := f.valida(a)
	if err == nil {
		// toda ferramenta roda sob captura: um Print perdido em alguma função
		// do app iria para o stdout real — o canal do protocolo — e corromperia
		// a resposta. O que escapar assim entra no texto do resultado.
		var solto string
		solto, err = captura(func() error {
			var errF error
			texto, errF = f.rodar(s, a)
			return errF
		})
		if strings.TrimSpace(solto) != "" {
			texto = strings.TrimRight(solto, "\n") + "\n" + texto
		}
	}
	if err != nil {
		texto = strings.TrimSpace(texto)
		if texto != "" {
			texto += "\n\n"
		}
		texto += "erro: " + err.Error()
	}
	texto = semANSI(texto)
	if strings.TrimSpace(texto) == "" {
		texto = "Nada encontrado."
	}
	if len(texto) > limiteSaida {
		texto = texto[:limiteSaida] + "\n\n[saída cortada: refine a consulta com filtros]"
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": texto}},
		"isError": err != nil,
	}
}

// captura redireciona o stdout durante f e devolve o texto impresso — mesma
// técnica do bot e da TUI. Lê o pipe em paralelo para uma saída maior que o
// buffer do pipe não travar f.
func captura(f func() error) (string, error) {
	antigo := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	lido := make(chan string)
	go func() {
		var b strings.Builder
		io.Copy(&b, r)
		r.Close()
		lido <- b.String()
	}()
	os.Stdout = w
	errF := f()
	os.Stdout = antigo
	w.Close()
	return <-lido, errF
}

var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// semANSI remove sequências de cor/estilo do terminal.
func semANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

// Erro de argumento inválido, para as ferramentas.
func errArg(nome, motivo string) error {
	return fmt.Errorf("argumento %q inválido: %s", nome, motivo)
}
