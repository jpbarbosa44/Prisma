package app

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/db"
)

// abreDB cria um banco temporário e devolve a conexão migrada.
func abreDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// silencia descarta o stdout durante f, para os testes não poluírem a saída.
func silencia(t *testing.T, f func() error) {
	t.Helper()
	antigo := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := f()
	w.Close()
	os.Stdout = antigo
	io.Copy(io.Discard, r)
	r.Close()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

// capturaSaida executa f com o stdout redirecionado e devolve o texto.
func capturaSaida(t *testing.T, f func() error) string {
	t.Helper()
	antigo := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := f()
	w.Close()
	os.Stdout = antigo
	b, _ := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	return string(b)
}

func TestContaEditar(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Velho', 1000)`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Conta(conn, []string{"editar", "1", "--nome", "Novo", "--saldo", "50,00"})
	})
	var nome string
	var saldo int64
	if err := conn.QueryRow(`SELECT nome, saldo_inicial FROM contas WHERE id = 1`).Scan(&nome, &saldo); err != nil {
		t.Fatal(err)
	}
	if nome != "Novo" || saldo != 5000 {
		t.Errorf("após editar: nome=%q saldo=%d, quer Novo e 5000", nome, saldo)
	}
	// banco não foi tocado: editar sem campos deve falhar
	if err := Conta(conn, []string{"editar", "1"}); err == nil {
		t.Error("editar sem campos deveria falhar")
	}
}

func TestRecorrenciaEditarPropagaPendentes(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "pagar", "--desc", "Aluguel", "--valor", "1300", "--dia", "10",
		})
	})
	silencia(t, func() error {
		return Recorrencia(conn, []string{"editar", "1", "--valor", "1.400,00", "--dia", "15"})
	})
	rows, err := conn.Query(
		`SELECT valor, vencimento FROM lancamentos WHERE recorrencia_id = 1 AND status = 'pendente'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		var valor int64
		var venc string
		if err := rows.Scan(&valor, &venc); err != nil {
			t.Fatal(err)
		}
		if valor != 140000 {
			t.Errorf("pendente com valor %d, quer 140000", valor)
		}
		if venc[8:] != "15" {
			t.Errorf("pendente com vencimento %s, queria dia 15", venc)
		}
	}
	if n == 0 {
		t.Fatal("nenhum pendente gerado para verificar")
	}
}

func TestLancamentosFiltroPorData(t *testing.T) {
	conn := abreDB(t)
	for _, l := range []struct{ desc, venc string }{
		{"Antigo", "2026-05-10"}, {"NoMeio", "2026-06-10"}, {"Futuro", "2026-07-10"},
	} {
		silencia(t, func() error {
			return NovoLancamento(conn, "pagar", []string{"add", "--desc", l.desc, "--valor", "10", "--venc", l.venc})
		})
	}
	saida := capturaSaida(t, func() error {
		return Lancamentos(conn, []string{"--de", "2026-06-01", "--ate", "2026-06-30"})
	})
	if !strings.Contains(saida, "NoMeio") {
		t.Error("filtro --de/--ate deveria incluir NoMeio")
	}
	if strings.Contains(saida, "Antigo") || strings.Contains(saida, "Futuro") {
		t.Errorf("filtro --de/--ate vazou lançamentos fora do intervalo:\n%s", saida)
	}
}

func TestSomaMeses(t *testing.T) {
	casos := []struct {
		data string
		n    int
		quer string
	}{
		{"2026-01-31", 1, "2026-02-28"},
		{"2026-01-15", 2, "2026-03-15"},
		{"2026-10-31", 1, "2026-11-30"},
		{"2026-12-05", 1, "2027-01-05"},
		{"2024-01-31", 1, "2024-02-29"}, // bissexto
	}
	for _, c := range casos {
		if got := somaMeses(c.data, c.n); got != c.quer {
			t.Errorf("somaMeses(%q, %d) = %q, quer %q", c.data, c.n, got, c.quer)
		}
	}
}

func TestPeriodos(t *testing.T) {
	p, err := periodoMes("2026-02")
	if err != nil || p.Inicio != "2026-02-01" || p.Fim != "2026-03-01" {
		t.Errorf("periodoMes(2026-02) = %+v, %v", p, err)
	}
	p, err = periodoSemana("2026-W24")
	if err != nil || p.Inicio != "2026-06-08" || p.Fim != "2026-06-15" {
		t.Errorf("periodoSemana(2026-W24) = %+v, %v", p, err)
	}
	if ref, _ := refDaData("semana", "2026-06-11"); ref != "2026-W24" {
		t.Errorf("refDaData(semana, 2026-06-11) = %q, quer 2026-W24", ref)
	}
	if ref, _ := refDaData("mes", "2026-06-11"); ref != "2026-06" {
		t.Errorf("refDaData(mes, 2026-06-11) = %q, quer 2026-06", ref)
	}
}

func TestSimulaPlanoSemJuros(t *testing.T) {
	plano := simulaPlano(100000, 0, 30000) // R$ 1.000 pagando R$ 300/mês
	if len(plano) != 4 {
		t.Fatalf("esperava 4 parcelas, veio %d", len(plano))
	}
	var total int64
	for _, p := range plano {
		total += p.pago
	}
	if total != 100000 {
		t.Errorf("total pago = %d, quer 100000", total)
	}
	if plano[3].saldoFinal != 0 {
		t.Errorf("saldo final = %d, quer 0", plano[3].saldoFinal)
	}
	if plano[3].pago != 10000 {
		t.Errorf("última parcela = %d, quer 10000", plano[3].pago)
	}
}

func TestSimulaPlanoAporteInsuficiente(t *testing.T) {
	// juros de 10% sobre 10.000 = 1.000/mês; aporte de 500 nunca quita
	plano := simulaPlano(1000000, 10, 50000)
	if len(plano) == 0 || len(plano) > 600 {
		t.Fatalf("simulação fora do teto: %d meses", len(plano))
	}
	if ultimo := plano[len(plano)-1].saldoFinal; ultimo <= 0 {
		t.Errorf("dívida impagável não deveria zerar (saldo final %d)", ultimo)
	}
}

func TestParcelasSomamTotal(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{
			"add", "--desc", "Notebook", "--valor", "1.000,00", "--parcelas", "3", "--venc", "2026-07-10",
		})
	})
	var n int
	var soma int64
	if err := conn.QueryRow(`SELECT COUNT(*), SUM(valor) FROM lancamentos`).Scan(&n, &soma); err != nil {
		t.Fatal(err)
	}
	if n != 3 || soma != 100000 {
		t.Errorf("parcelas: n=%d soma=%d, quer n=3 soma=100000", n, soma)
	}
	var vencUltima string
	if err := conn.QueryRow(
		`SELECT vencimento FROM lancamentos ORDER BY id DESC LIMIT 1`).Scan(&vencUltima); err != nil {
		t.Fatal(err)
	}
	if vencUltima != "2026-09-10" {
		t.Errorf("último vencimento = %q, quer 2026-09-10", vencUltima)
	}
}

func TestTransferenciaAtualizaSaldos(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(
		`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 100000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(
		`INSERT INTO carteiras (nome, saldo_inicial) VALUES ('Bolso', 0)`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Transferir(conn, []string{"--de", "conta:1", "--para", "carteira:1", "--valor", "300,00"})
	})
	sc, err := saldoConta(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := saldoCarteira(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sc != 70000 || sw != 30000 {
		t.Errorf("saldos após transferência: conta=%d carteira=%d, quer 70000 e 30000", sc, sw)
	}
	// transferência não pode aparecer como receita/despesa
	total, err := saldoTotal(conn)
	if err != nil {
		t.Fatal(err)
	}
	if total != 100000 {
		t.Errorf("saldo total = %d, transferência não deveria alterá-lo (quer 100000)", total)
	}
}

func TestRecorrenciaGeraLancamentos(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "receber", "--desc", "Salário", "--valor", "5000", "--dia", "1",
		})
	})
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id = 1 AND status = 'pendente'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// horizonte de 3 meses: gera 3 ou 4 ocorrências dependendo do dia atual
	if n < 3 || n > 4 {
		t.Errorf("recorrência gerou %d lançamentos, esperava 3 ou 4", n)
	}
	// segunda geração não pode duplicar
	g, err := GerarRecorrencias(conn)
	if err != nil {
		t.Fatal(err)
	}
	if g != 0 {
		t.Errorf("segunda geração criou %d lançamentos, deveria ser idempotente", g)
	}
}

func TestEditarLancamento(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Aluguel", "--valor", "1200"})
	})
	silencia(t, func() error {
		return Lancamentos(conn, []string{"editar", "1", "--valor", "1.250,00", "--cat", "moradia"})
	})
	var valor int64
	var cat string
	if err := conn.QueryRow(`SELECT valor, categoria FROM lancamentos WHERE id = 1`).Scan(&valor, &cat); err != nil {
		t.Fatal(err)
	}
	if valor != 125000 || cat != "moradia" {
		t.Errorf("após editar: valor=%d cat=%q, quer 125000 e moradia", valor, cat)
	}
}

func TestImportarCSVeDedupe(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 0)`); err != nil {
		t.Fatal(err)
	}
	csv := "data;descricao;valor\n10/06/2026;PIX RECEBIDO;150,00\n11/06/2026;MERCADO;-89,90\n"
	arq := filepath.Join(t.TempDir(), "extrato.csv")
	if err := os.WriteFile(arq, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Importar(conn, []string{"--arquivo", arq, "--conta", "1"})
	})
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE status = 'quitado'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("importou %d movimentos, quer 2", n)
	}
	saldo, err := saldoConta(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if saldo != 15000-8990 {
		t.Errorf("saldo após importar = %d, quer %d", saldo, 15000-8990)
	}
	// importar de novo não pode duplicar
	silencia(t, func() error {
		return Importar(conn, []string{"--arquivo", arq, "--conta", "1"})
	})
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("reimportação duplicou: %d lançamentos, quer 2", n)
	}
}

func TestResetar(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 1000)`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Conta", "--valor", "10"})
	})
	silencia(t, func() error {
		return Resetar(conn, []string{"--sim"})
	})
	for _, tabela := range []string{"contas", "lancamentos", "transferencias", "recorrencias", "emergencias", "planejamentos", "carteiras"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + tabela).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("após resetar, %s ainda tem %d registro(s)", tabela, n)
		}
	}
	// backup criado ao lado do banco
	dir := filepath.Dir(os.Getenv("PRISMA_DB"))
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	temBackup := false
	for _, e := range entradas {
		if strings.Contains(e.Name(), ".bak-") {
			temBackup = true
		}
	}
	if !temBackup {
		t.Error("resetar não criou o backup")
	}
	// ids recomeçam do 1
	if _, err := conn.Exec(`INSERT INTO contas (nome) VALUES ('Nova')`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := conn.QueryRow(`SELECT id FROM contas WHERE nome = 'Nova'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("após resetar, novo id = %d, quer 1", id)
	}
}

func TestImportarOFX(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 0)`); err != nil {
		t.Fatal(err)
	}
	ofx := `OFXHEADER:100
<OFX>
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260610120000[-3:BRT]
<TRNAMT>-45.90
<MEMO>UBER TRIP
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20260611
<TRNAMT>1200.00
<NAME>TED RECEBIDA
</STMTTRN>
</OFX>`
	arq := filepath.Join(t.TempDir(), "extrato.ofx")
	if err := os.WriteFile(arq, []byte(ofx), 0o644); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Importar(conn, []string{"--arquivo", arq, "--conta", "1"})
	})
	saldo, err := saldoConta(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if saldo != 120000-4590 {
		t.Errorf("saldo após OFX = %d, quer %d", saldo, 120000-4590)
	}
}
