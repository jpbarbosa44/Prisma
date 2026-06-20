package app

import (
	"strings"
	"testing"

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
