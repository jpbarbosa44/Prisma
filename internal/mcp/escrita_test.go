package mcp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/db"
)

// sessaoEscrita monta uma sessão como `prisma mcp --escrita` com banco local:
// gravação numa conexão normal, consultas numa query_only.
func sessaoEscrita(t *testing.T) (*Sessao, *sql.DB) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "teste.db")
	t.Setenv("PRISMA_DB", caminho)
	rw, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	for _, q := range []string{
		`INSERT INTO contas (nome, saldo_inicial) VALUES ('Nubank', 100000)`,
		`INSERT INTO cartoes (nome, limite, dia_fechamento, dia_vencimento) VALUES ('Roxinho', 500000, 3, 10)`,
		`INSERT INTO categorias (nome) VALUES ('mercado')`,
	} {
		if _, err := rw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ro, err := db.AbrirAnalytics()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	return &Sessao{leitura: ro, escrita: rw, sqlLivre: true, banco: caminho}, rw
}

func conta(t *testing.T, conn *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEscritaSoComFlag(t *testing.T) {
	conn := abreSomenteLeitura(t)
	resps := conversa(t, conn,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		chamada(2, "importar_lancamentos", map[string]any{"lancamentos": []any{}}),
	)
	for _, tl := range resps[0]["result"].(map[string]any)["tools"].([]any) {
		nome := tl.(map[string]any)["name"].(string)
		if f, _ := buscaFerramenta(nome); f.escreve {
			t.Errorf("sessão somente leitura expôs %q", nome)
		}
	}
	if _, ok := resps[1]["error"]; !ok {
		t.Errorf("importar sem --escrita deveria ser ferramenta desconhecida: %v", resps[1])
	}
}

func TestLerExtratoLatin1(t *testing.T) {
	s, _ := sessaoEscrita(t)
	arq := filepath.Join(t.TempDir(), "extrato.csv")
	// "Padaria São João" em Latin-1 (ã = 0xE3, ã = 0xE3, o = o)
	conteudo := "Data;Descrição;Valor\n05/10/2026;Padaria S\xe3o Jo\xe3o;-12,50\n06/10/2026;Sal\xe1rio;5000,00\n"
	if err := os.WriteFile(arq, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
	resps := conversaSessao(t, s,
		chamada(1, "ler_extrato", map[string]any{"caminho": arq}),
		chamada(2, "ler_extrato", map[string]any{"caminho": "/etc/passwd"}),
	)
	texto, isErr := resultado(t, resps[0])
	if isErr || !strings.Contains(texto, "Padaria São João") || !strings.Contains(texto, "Latin-1") ||
		!strings.Contains(texto, "2 movimento(s)") || !strings.Contains(texto, "Separador provável: ;") {
		t.Errorf("ler_extrato:\n%s", texto)
	}
	if _, isErr := resultado(t, resps[1]); !isErr {
		t.Error("ler_extrato deveria recusar arquivo que não é extrato")
	}
}

func TestImportarPreviaConfirmaEDesfaz(t *testing.T) {
	s, rw := sessaoEscrita(t)
	lote := []any{
		map[string]any{"data": "2026-10-05", "descricao": "Padaria", "valor": "12,50", "tipo": "pagar", "categoria": "Padaria", "conta": 1},
		map[string]any{"data": "2026-10-06", "descricao": "Salário", "valor": 5000, "tipo": "receber", "categoria": "salario", "conta": 1},
		map[string]any{"data": "2026-10-07", "descricao": "Mercado Extra", "valor": "230,00", "tipo": "pagar", "categoria": "mercado", "cartao": 1},
	}
	resps := conversaSessao(t, s, chamada(1, "importar_lancamentos", map[string]any{"lancamentos": lote}))
	texto, isErr := resultado(t, resps[0])
	if isErr || !strings.Contains(texto, "PRÉVIA") || !strings.Contains(texto, "Novos:       3") ||
		!strings.Contains(texto, "Categorias novas: padaria, salario") {
		t.Fatalf("prévia:\n%s", texto)
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos`); n != 0 {
		t.Fatalf("a prévia gravou %d lançamentos", n)
	}

	resps = conversaSessao(t, s,
		chamada(2, "importar_lancamentos", map[string]any{"lancamentos": lote, "confirmar": true}),
		chamada(3, "importar_lancamentos", map[string]any{"lancamentos": lote, "confirmar": true}),
	)
	texto, isErr = resultado(t, resps[0])
	if isErr || !strings.Contains(texto, "3 lançamento(s) criado(s)") {
		t.Fatalf("confirmar:\n%s", texto)
	}
	if s.snapshot == "" {
		t.Error("deveria ter tirado o snapshot antes de gravar")
	} else if _, err := os.Stat(s.snapshot); err != nil {
		t.Errorf("snapshot ausente: %v", err)
	}
	if texto, _ := resultado(t, resps[1]); !strings.Contains(texto, "Nada a gravar") {
		t.Errorf("reimportar deveria ignorar tudo como duplicado:\n%s", texto)
	}
	// conta: quitado; cartão: pendente, com a data da compra preservada
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos WHERE conta_id = 1 AND status = 'quitado'`); n != 2 {
		t.Errorf("lançamentos de conta quitados = %d", n)
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos WHERE cartao_id = 1 AND status = 'pendente' AND data_compra = '2026-10-07'`); n != 1 {
		t.Errorf("compra no cartão não ficou pendente com data_compra")
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM categorias WHERE nome IN ('padaria', 'salario')`); n != 2 {
		t.Errorf("categorias novas no catálogo = %d", n)
	}

	var loteID string
	rw.QueryRow(`SELECT substr(observacao, instr(observacao, '[lote ') + 6, 19) FROM lancamentos LIMIT 1`).Scan(&loteID)
	resps = conversaSessao(t, s,
		chamada(4, "desfazer_importacao", nil),
		chamada(5, "desfazer_importacao", map[string]any{"lote": loteID}),
		chamada(6, "desfazer_importacao", map[string]any{"lote": "'; DROP TABLE x"}),
	)
	if texto, _ := resultado(t, resps[0]); !strings.Contains(texto, loteID) {
		t.Errorf("listagem de lotes sem %s:\n%s", loteID, texto)
	}
	if texto, isErr := resultado(t, resps[1]); isErr || !strings.Contains(texto, "3 lançamento(s) apagado(s)") {
		t.Errorf("desfazer:\n%s", texto)
	}
	if _, isErr := resultado(t, resps[2]); !isErr {
		t.Error("lote com formato inválido deveria ser recusado")
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos`); n != 0 {
		t.Errorf("sobraram %d lançamentos após desfazer", n)
	}
}

func TestImportarRecusaItemInvalidoSemGravar(t *testing.T) {
	s, rw := sessaoEscrita(t)
	bom := map[string]any{"data": "2026-10-05", "descricao": "Padaria", "valor": "12,50", "tipo": "pagar", "conta": 1}
	ruins := []map[string]any{
		{"data": "05/10/2026", "descricao": "x", "valor": "1", "tipo": "pagar"},
		{"data": "2026-10-05", "descricao": "x", "valor": "-1", "tipo": "pagar"},
		{"data": "2026-10-05", "descricao": "x", "valor": "1", "tipo": "pagar", "conta": 99},
		{"data": "2026-10-05", "descricao": "x", "valor": "1", "tipo": "pagar", "conta": 1, "cartao": 1},
		{"data": "2026-10-05", "descricao": "x", "valor": "1", "tipo": "transferir"},
		{"data": "2026-10-05", "descricao": "x", "valor": "1", "tipo": "pagar", "id": 5},
	}
	var msgs []string
	for i, r := range ruins {
		msgs = append(msgs, chamada(i+1, "importar_lancamentos", map[string]any{"lancamentos": []any{bom, r}, "confirmar": true}))
	}
	for i, r := range conversaSessao(t, s, msgs...) {
		if _, isErr := resultado(t, r); !isErr {
			t.Errorf("item inválido %v deveria ser recusado", ruins[i])
		}
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos`); n != 0 {
		t.Errorf("lote com item inválido gravou %d lançamentos", n)
	}
}

func TestCategoriaRecorrenciaEConta(t *testing.T) {
	s, rw := sessaoEscrita(t)
	lote := []any{
		map[string]any{"data": "2026-08-15", "descricao": "Netflix", "valor": "55,90", "tipo": "pagar", "categoria": "streaming", "conta": 1},
		map[string]any{"data": "2026-09-15", "descricao": "Netflix", "valor": "55,90", "tipo": "pagar", "categoria": "streaming", "conta": 1},
	}
	conversaSessao(t, s, chamada(1, "importar_lancamentos", map[string]any{"lancamentos": lote, "confirmar": true}))
	resps := conversaSessao(t, s,
		chamada(2, "definir_categoria", map[string]any{"nome": "Pet", "classe": "necessidade"}),
		chamada(3, "criar_recorrencia", map[string]any{"tipo": "pagar", "descricao": "Netflix", "valor": "55,90", "dia": 15,
			"categoria": "streaming", "conta": 1, "assinatura": true, "vincular_lancamentos": []any{1, 2}}),
		chamada(4, "criar_recorrencia", map[string]any{"tipo": "pagar", "descricao": "Antiga", "valor": 10, "dia": 1, "inicio": "2020-01-01"}),
		chamada(5, "criar_conta", map[string]any{"nome": "Inter", "banco": "Inter", "saldo_inicial": "250,00"}),
	)
	for i, r := range resps {
		texto, isErr := resultado(t, r)
		if (i == 2) != isErr {
			t.Errorf("resposta %d: isError=%v\n%s", i, isErr, texto)
		}
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM categorias WHERE nome = 'pet' AND classe = 'necessidade'`); n != 1 {
		t.Error("definir_categoria não criou pet como necessidade")
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM recorrencias WHERE descricao = 'Netflix' AND assinatura = 1`); n != 1 {
		t.Error("assinatura não criada")
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM lancamentos WHERE id IN (1, 2) AND recorrencia_id IS NOT NULL`); n != 2 {
		t.Errorf("vinculou %d de 2 lançamentos importados", n)
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM recorrencias WHERE descricao = 'Antiga'`); n != 0 {
		t.Error("recorrência com início no passado deveria ser recusada")
	}
	if n := conta(t, rw, `SELECT COUNT(*) FROM contas WHERE nome = 'Inter' AND saldo_inicial = 25000`); n != 1 {
		t.Error("criar_conta não gravou a conta")
	}
}

func TestSemSQLLivreNoModoCliente(t *testing.T) {
	s, rw := sessaoEscrita(t)
	s.leitura, s.sqlLivre, s.banco = rw, false, "" // como no modo cliente
	resps := conversaSessao(t, s,
		chamada(1, "consulta_sql", map[string]any{"sql": "SELECT 1"}),
		chamada(2, "saldo", nil),
	)
	if _, ok := resps[0]["error"]; !ok {
		t.Error("consulta_sql não deveria existir sem conexão query_only")
	}
	if texto, isErr := resultado(t, resps[1]); isErr || !strings.Contains(texto, "Nubank") {
		t.Errorf("saldo no modo cliente:\n%s", texto)
	}
}
