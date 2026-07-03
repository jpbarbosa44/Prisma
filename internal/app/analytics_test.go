package app

import (
	"strings"
	"testing"
	"time"

	"prisma/internal/db"
)

// TestAnalyticsSomenteLeitura garante o isolamento de dados (RNF01): a conexão
// do módulo Analytics lê, mas qualquer escrita é barrada.
func TestAnalyticsSomenteLeitura(t *testing.T) {
	conn := abreDB(t) // cria e migra o banco temporário e define PRISMA_DB
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 100000)`); err != nil {
		t.Fatal(err)
	}
	ro, err := db.AbrirAnalytics()
	if err != nil {
		t.Fatalf("abrindo analytics: %v", err)
	}
	defer ro.Close()

	var n int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM contas`).Scan(&n); err != nil {
		t.Fatalf("leitura deveria funcionar: %v", err)
	}
	if n != 1 {
		t.Fatalf("esperava 1 conta, veio %d", n)
	}
	if _, err := ro.Exec(`INSERT INTO contas (nome) VALUES ('Proibido')`); err == nil {
		t.Error("escrita no modo analytics deveria falhar (query_only)")
	}
}

// TestAnalyticsFuncoes confere que as análises do Health Score, Runway e
// Assinaturas Ocultas rodam sem erro sobre um banco com dados mínimos.
func TestAnalyticsFuncoes(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 500000)`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return NovoLancamento(conn, "receber", []string{"add", "--desc", "Salario", "--valor", "5000", "--quitado"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Mercado", "--valor", "300", "--quitado"})
	})

	out := capturaSaida(t, func() error { return AnalyticsHealthScore(conn) })
	if !strings.Contains(out, "HEALTH SCORE") || !strings.Contains(out, "/100") {
		t.Errorf("Health Score sem o cabeçalho/medidor esperado:\n%s", out)
	}
	// todas as análises devem rodar sem erro sobre um banco com dados mínimos
	silencia(t, func() error { return AnalyticsRunway(conn) })
	silencia(t, func() error { return AnalyticsAssinaturasOcultas(conn) })
	silencia(t, func() error { return AnalyticsAnomalias(conn) })
	silencia(t, func() error { return AnalyticsSazonalidade(conn) })
	silencia(t, func() error { return AnalyticsInflacao(conn) })
	silencia(t, func() error { return AnalyticsUtilidades(conn) })
	silencia(t, func() error { return AnalyticsRegra502030(conn) })
	silencia(t, func() error { return AnalyticsPatrimonio(conn) })
	silencia(t, func() error { return AnalyticsMetas(conn, nil) })                          // sem args: instruções
	silencia(t, func() error { return AnalyticsMetas(conn, []string{"50.000,00", "24"}) })  // com meta
	silencia(t, func() error { return AnalyticsSimulador(conn, nil) })                      // sem args
	silencia(t, func() error { return AnalyticsSimulador(conn, []string{"2.000,00", ""}) }) // perda de renda
}

// TestContemAlgumaPalavraInteira garante o casamento por palavra inteira das
// categorias: "gasolina" e "gastos" não podem casar a chave "gas" (senão
// combustível entraria nas contas da casa do RF11).
func TestContemAlgumaPalavraInteira(t *testing.T) {
	casos := []struct {
		cat  string
		quer bool
	}{
		{"gás", true},
		{"gas", true},
		{"conta de luz", true},
		{"internet", true},
		{"gasolina", false},
		{"gastos gerais", false},
		{"luzes de natal", false},
	}
	for _, c := range casos {
		if got := contemAlguma(c.cat, catsUtilidade); got != c.quer {
			t.Errorf("contemAlguma(%q, utilidades) = %v, queria %v", c.cat, got, c.quer)
		}
	}
}

// TestPrimeiroComDado cobre o corte dos meses anteriores ao primeiro movimento
// (não são "gasto zero": o app ainda não era usado).
func TestPrimeiroComDado(t *testing.T) {
	casos := []struct {
		recs, desps []int64
		quer        int
	}{
		{[]int64{0, 0, 100, 0}, []int64{0, 0, 0, 50}, 2},
		{[]int64{0, 0, 0}, []int64{0, 20, 0}, 1},
		{[]int64{5, 0}, []int64{0, 0}, 0},
		{[]int64{0, 0}, []int64{0, 0}, 0}, // sem dado algum: janela como está
	}
	for _, c := range casos {
		if got := primeiroComDado(c.recs, c.desps); got != c.quer {
			t.Errorf("primeiroComDado(%v, %v) = %d, queria %d", c.recs, c.desps, got, c.quer)
		}
	}
}

// TestSaldoDevedorEstimado confere a dívida estimada do Patrimônio Líquido:
// o plano de aportes aplicado aos meses decorridos desde o cadastro.
func TestSaldoDevedorEstimado(t *testing.T) {
	hoje := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	// sem juros: 10.000 com aporte de 1.000 há 3 meses → devendo 7.000
	if got := saldoDevedorEstimado(1000000, 0, 100000, "2026-04-10", hoje); got != 700000 {
		t.Errorf("3 meses de aporte: saldo = %d, queria 700000", got)
	}
	// cadastro no mês corrente: nada amortizado ainda, valor cheio
	if got := saldoDevedorEstimado(1000000, 0, 100000, "2026-07-01", hoje); got != 1000000 {
		t.Errorf("cadastro no mês corrente: saldo = %d, queria 1000000", got)
	}
	// cadastro além do fim do plano: dívida já quitável → 0
	if got := saldoDevedorEstimado(1000000, 0, 100000, "2024-01-01", hoje); got != 0 {
		t.Errorf("plano vencido: saldo = %d, queria 0", got)
	}
	// data de cadastro inválida: cai no valor cheio
	if got := saldoDevedorEstimado(1000000, 0, 100000, "sem-data", hoje); got != 1000000 {
		t.Errorf("data inválida: saldo = %d, queria 1000000", got)
	}
}

// TestAssinaturasOcultasIgnoraFuturo garante que só ocorrências já vencidas
// contam: um --repetir 12 criado hoje não é uma assinatura "oculta".
func TestAssinaturasOcultasIgnoraFuturo(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Aluguel", "--valor", "1.200,00", "--repetir", "12"})
	})
	out := capturaSaida(t, func() error { return AnalyticsAssinaturasOcultas(conn) })
	if !strings.Contains(out, "Nenhuma assinatura oculta") {
		t.Errorf("os meses futuros do --repetir não deviam ser flagrados:\n%s", out)
	}

	// três meses já vencidos de mesmo valor: assinatura provável (datas no dia
	// 1º para o AddDate não normalizar o fim do mês e repetir meses)
	agora := time.Now()
	base := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		venc := base.AddDate(0, -i, 0).Format("02/01/2006")
		silencia(t, func() error {
			return NovoLancamento(conn, "pagar", []string{
				"add", "--desc", "Streaming", "--valor", "39,90", "--venc", venc, "--quitado"})
		})
	}
	out = capturaSaida(t, func() error { return AnalyticsAssinaturasOcultas(conn) })
	if !strings.Contains(out, "Streaming") {
		t.Errorf("despesa igual em 3 meses vencidos devia ser flagrada:\n%s", out)
	}
}
