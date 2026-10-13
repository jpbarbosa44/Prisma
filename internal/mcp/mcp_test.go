package mcp

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"prisma/internal/db"
)

// abreSomenteLeitura cria um banco temporário com alguns dados e o reabre como
// o `prisma mcp` faz: pela conexão query_only do Analytics.
func abreSomenteLeitura(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	rw, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	hoje := time.Now().Format("2006-01-02")
	for _, q := range []string{
		`INSERT INTO contas (nome, saldo_inicial) VALUES ('Nubank', 150000)`,
		`INSERT INTO cartoes (nome, limite, dia_fechamento, dia_vencimento) VALUES ('Roxinho', 500000, 1, 10)`,
		`INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id)
		 VALUES ('pagar', 'feira', 12345, 'mercado', '` + hoje + `', 'quitado', '` + hoje + `', 1)`,
		`INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, conta_id)
		 VALUES ('receber', 'salário', 500000, 'salario', '` + hoje + `', 1)`,
	} {
		if _, err := rw.Exec(q); err != nil {
			t.Fatalf("semeando: %v\n%s", err, q)
		}
	}
	rw.Close()
	conn, err := db.AbrirAnalytics()
	if err != nil {
		t.Fatalf("abrindo somente leitura: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// conversa manda as mensagens (uma por linha) e devolve as respostas
// decodificadas, falhando se alguma linha do out não for JSON.
func conversa(t *testing.T, conn *sql.DB, msgs ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Servir(conn, strings.NewReader(strings.Join(msgs, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Servir: %v", err)
	}
	var resps []map[string]any
	for _, linha := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if linha == "" {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(linha), &r); err != nil {
			t.Fatalf("linha não é JSON (stdout poluído?): %q", linha)
		}
		resps = append(resps, r)
	}
	return resps
}

// chamada monta um tools/call.
func chamada(id int, nome string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": nome, "arguments": args},
	})
	return string(b)
}

// resultado extrai o texto e o isError de uma resposta de tools/call.
func resultado(t *testing.T, r map[string]any) (string, bool) {
	t.Helper()
	res, ok := r["result"].(map[string]any)
	if !ok {
		t.Fatalf("resposta sem result: %v", r)
	}
	content := res["content"].([]any)
	texto := content[0].(map[string]any)["text"].(string)
	return texto, res["isError"].(bool)
}

func TestProtocoloBasico(t *testing.T) {
	conn := abreSomenteLeitura(t)
	resps := conversa(t, conn,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"teste","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		chamada(3, "saldo", nil),
		`{"jsonrpc":"2.0","id":4,"method":"nao/existe"}`,
		`{isto não é json`,
	)
	if len(resps) != 5 { // a notificação não tem resposta
		t.Fatalf("esperava 5 respostas, veio %d: %v", len(resps), resps)
	}
	info := resps[0]["result"].(map[string]any)
	if info["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", info["protocolVersion"])
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(ferramentas) {
		t.Errorf("tools/list devolveu %d ferramentas, esperava %d", len(tools), len(ferramentas))
	}
	texto, isErr := resultado(t, resps[2])
	if isErr || !strings.Contains(texto, "Nubank") {
		t.Errorf("saldo: isError=%v texto=%q", isErr, texto)
	}
	if strings.Contains(texto, "\x1b[") {
		t.Errorf("saldo veio com códigos ANSI: %q", texto)
	}
	if e, _ := resps[3]["error"].(map[string]any); e == nil || e["code"].(float64) != erroMetodo {
		t.Errorf("método desconhecido: %v", resps[3])
	}
	if e, _ := resps[4]["error"].(map[string]any); e == nil || e["code"].(float64) != erroParse {
		t.Errorf("JSON inválido: %v", resps[4])
	}
}

// TestTodasAsFerramentasLeem garante que nenhuma ferramenta tenta escrever:
// a conexão é query_only, então uma escrita viraria isError.
func TestTodasAsFerramentasLeem(t *testing.T) {
	conn := abreSomenteLeitura(t)
	args := map[string]map[string]any{
		"fatura":       {"cartao": 1},
		"extrato":      {"conta": 1},
		"simular":      {"valor": "4.000,00", "parcelas": 12, "juros": 1.5, "entrada": 500},
		"consulta_sql": {"sql": "SELECT categoria, SUM(valor) FROM lancamentos GROUP BY categoria"},
		"analise":      {"tipo": "health_score"},
	}
	var msgs []string
	for i, f := range ferramentas {
		msgs = append(msgs, chamada(i+1, f.nome, args[f.nome]))
	}
	for _, tipo := range nomesAnalises {
		msgs = append(msgs, chamada(len(msgs)+1, "analise", map[string]any{"tipo": tipo}))
	}
	resps := conversa(t, conn, msgs...)
	if len(resps) != len(msgs) {
		t.Fatalf("esperava %d respostas, veio %d", len(msgs), len(resps))
	}
	for i, r := range resps {
		texto, isErr := resultado(t, r)
		if isErr {
			t.Errorf("%s → erro: %s", msgs[i], texto)
		}
	}
}

func TestFiltrosELancamentos(t *testing.T) {
	conn := abreSomenteLeitura(t)
	mes := time.Now().Format("2006-01")
	resps := conversa(t, conn,
		chamada(1, "lancamentos", map[string]any{"mes": mes, "categoria": "mercado"}),
		chamada(2, "lancamentos", map[string]any{"tipo": "receber", "so_pendentes": true}),
	)
	if texto, _ := resultado(t, resps[0]); !strings.Contains(texto, "feira") || strings.Contains(texto, "salário") {
		t.Errorf("filtro por categoria: %q", texto)
	}
	if texto, _ := resultado(t, resps[1]); !strings.Contains(texto, "salário") || strings.Contains(texto, "feira") {
		t.Errorf("filtro por tipo/pendentes: %q", texto)
	}
}

func TestArgumentosInvalidos(t *testing.T) {
	conn := abreSomenteLeitura(t)
	casos := []struct {
		nome string
		args map[string]any
	}{
		{"lancamentos", map[string]any{"mes": "abc"}},
		{"lancamentos", map[string]any{"de": "2026-02-31"}},
		{"lancamentos", map[string]any{"tipo": "remover"}},
		{"lancamentos", map[string]any{"extra": 1}},
		{"relatorio", map[string]any{"meses": 99}},
		{"relatorio", map[string]any{"meses": 2.5}},
		{"fatura", nil},
		{"extrato", nil},
		{"extrato", map[string]any{"conta": 1, "carteira": 1}},
		{"simular", map[string]any{"valor": true}},
		{"simular", map[string]any{"valor": "-10"}},
		{"analise", map[string]any{"tipo": "rm -rf"}},
		{"nao_existe", nil},
	}
	var msgs []string
	for i, c := range casos {
		msgs = append(msgs, chamada(i+1, c.nome, c.args))
	}
	resps := conversa(t, conn, msgs...)
	for i, r := range resps {
		if _, ok := r["error"]; ok {
			continue // ferramenta desconhecida é erro de protocolo
		}
		if _, isErr := resultado(t, r); !isErr {
			t.Errorf("%s deveria falhar", msgs[i])
		}
	}
}

func TestConsultaSQLSomenteLeitura(t *testing.T) {
	conn := abreSomenteLeitura(t)
	recusadas := []string{
		"DELETE FROM lancamentos",
		"UPDATE lancamentos SET valor = 0",
		"SELECT 1; DELETE FROM lancamentos",
		"WITH x AS (SELECT 1) DELETE FROM lancamentos", // passa no filtro, barra no query_only
		"",
	}
	var msgs []string
	for i, q := range recusadas {
		msgs = append(msgs, chamada(i+1, "consulta_sql", map[string]any{"sql": q}))
	}
	msgs = append(msgs, chamada(99, "consulta_sql", map[string]any{"sql": "SELECT COUNT(*) AS n FROM lancamentos;"}))
	resps := conversa(t, conn, msgs...)
	for i := range recusadas {
		if _, isErr := resultado(t, resps[i]); !isErr {
			t.Errorf("deveria recusar %q", recusadas[i])
		}
	}
	texto, isErr := resultado(t, resps[len(resps)-1])
	if isErr || !strings.Contains(texto, "n") || !strings.Contains(texto, "2") {
		t.Errorf("SELECT: isError=%v %q", isErr, texto)
	}
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 2 {
		t.Errorf("banco alterado: %d lançamentos", n)
	}
}
