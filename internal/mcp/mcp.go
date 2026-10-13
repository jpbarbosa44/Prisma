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

// Servir atende requisições MCP lidas de in até EOF, escrevendo as respostas
// em out. Processa uma por vez — obrigatório, pois a captura das funções do
// app troca os.Stdout enquanto roda.
func Servir(conn *sql.DB, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		linha := strings.TrimSpace(sc.Text())
		if linha == "" {
			continue
		}
		if r := trata(conn, []byte(linha)); r != nil {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// trata responde uma mensagem; devolve nil para notificações (sem id).
func trata(conn *sql.DB, linha []byte) *resposta {
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
			"instructions":    instrucoes,
		}
	case "ping":
		r.Result = map[string]any{}
	case "tools/list":
		lista := make([]map[string]any, 0, len(ferramentas))
		for _, f := range ferramentas {
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
		if !ok {
			r.Error = &erroRPC{erroParametros, "ferramenta desconhecida: " + p.Name}
			break
		}
		r.Result = chama(conn, f, p.Arguments)
	default:
		r.Error = &erroRPC{erroMetodo, "método não suportado: " + req.Method}
	}
	return r
}

// chama roda a ferramenta e embrulha o resultado no formato do MCP. Erros da
// ferramenta voltam como resultado com isError (o modelo vê e corrige), não
// como erro de protocolo.
func chama(conn *sql.DB, f ferramenta, a argumentos) map[string]any {
	if a == nil {
		a = argumentos{}
	}
	var texto string
	err := f.valida(a)
	if err == nil {
		texto, err = f.rodar(conn, a)
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
