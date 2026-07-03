package app

import (
	"strings"
	"testing"
	"time"

	"prisma/internal/db"
	"prisma/internal/money"
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

// TestAnalyticsNovasFuncoes confere que as análises RF12–RF16 rodam sem erro
// sobre um banco com dados mínimos (e vazio de planos/grupos/cartões).
func TestAnalyticsNovasFuncoes(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return NovoLancamento(conn, "receber", []string{"add", "--desc", "Salario", "--valor", "5000", "--quitado"})
	})
	silencia(t, func() error { return AnalyticsComprometimento(conn) })
	silencia(t, func() error { return AnalyticsAderenciaPlanos(conn) })
	silencia(t, func() error { return AnalyticsTendencias(conn) })
	silencia(t, func() error { return AnalyticsGrupos(conn) })
	silencia(t, func() error { return AnalyticsMelhorDiaCartao(conn) })
}

// TestRetaSlope confere a inclinação da regressão das Tendências.
func TestRetaSlope(t *testing.T) {
	// série perfeitamente linear: +100/mês
	if slope, media := retaSlope([]int64{100, 200, 300, 400}); slope != 100 || media != 250 {
		t.Errorf("retaSlope(linear) = (%.1f, %.1f), queria (100, 250)", slope, media)
	}
	// série constante: inclinação zero
	if slope, _ := retaSlope([]int64{500, 500, 500}); slope != 0 {
		t.Errorf("retaSlope(constante) = %.1f, queria 0", slope)
	}
	// um ponto só: sem inclinação definida (den == 0)
	if slope, media := retaSlope([]int64{700}); slope != 0 || media != 700 {
		t.Errorf("retaSlope(1 ponto) = (%.1f, %.1f), queria (0, 700)", slope, media)
	}
}

// TestPrazoCompra confere o prazo compra→vencimento pelo ciclo do cartão:
// comprar depois do fechamento empurra a compra para a fatura seguinte.
func TestPrazoCompra(t *testing.T) {
	// fecha dia 20, vence dia 27: compra em 10/06 cai na fatura de 27/06 (17
	// dias); compra em 21/06 (pós-fechamento) só vence em 27/07 (36 dias)
	antes := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	depois := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	if p := prazoCompra(20, 27, antes); p != 17 {
		t.Errorf("prazo antes do fechamento = %d, queria 17", p)
	}
	if p := prazoCompra(20, 27, depois); p != 36 {
		t.Errorf("prazo após o fechamento = %d, queria 36", p)
	}
}

// TestAderenciaPlanos monta dois planos mensais já encerrados — um cumprido,
// um estourado — e confere o placar.
func TestAderenciaPlanos(t *testing.T) {
	conn := abreDB(t)
	agora := time.Now()
	base := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC)
	mesPassado := base.AddDate(0, -1, 0)
	ref := mesPassado.Format("2006-01")
	venc := mesPassado.AddDate(0, 0, 9).Format("02/01/2006") // dia 10 do mês passado

	for _, ins := range [][]string{
		{"mercado", "500,00"},
		{"lazer", "100,00"},
	} {
		if _, err := conn.Exec(`INSERT INTO planejamentos (categoria, limite, periodo, ref)
			VALUES (?, ?, 'mes', ?)`, ins[0], mustParse(t, ins[1]), ref); err != nil {
			t.Fatal(err)
		}
	}
	// mercado gasta 400 (cumpre); lazer gasta 250 (estoura 150%)
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Compras", "--valor", "400,00", "--cat", "mercado", "--venc", venc, "--quitado"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Passeio", "--valor", "250,00", "--cat", "lazer", "--venc", venc, "--quitado"})
	})

	out := capturaSaida(t, func() error { return AnalyticsAderenciaPlanos(conn) })
	if !strings.Contains(out, "mercado") || !strings.Contains(out, "1 de 1") {
		t.Errorf("esperava mercado com 1 de 1 cumprido:\n%s", out)
	}
	if !strings.Contains(out, "+150%") {
		t.Errorf("esperava estouro médio de +150%% no lazer:\n%s", out)
	}
}

func mustParse(t *testing.T, s string) int64 {
	t.Helper()
	v, err := money.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
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
