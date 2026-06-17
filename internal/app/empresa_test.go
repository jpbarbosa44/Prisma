package app

import (
	"strconv"
	"strings"
	"testing"
)

func TestSocioParticipacaoDesigual(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "60"}) })
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Amigo", "--participacao", "40"}) })

	saida := capturaSaida(t, func() error { return Socio(conn, []string{"listar"}) })
	if !strings.Contains(saida, "60.0%") || !strings.Contains(saida, "40.0%") {
		t.Errorf("listagem de sócios não mostra as participações certas:\n%s", saida)
	}
	if strings.Contains(saida, "Aviso") {
		t.Errorf("soma 100%% não deveria gerar aviso:\n%s", saida)
	}
}

func TestSocioSomaDiferenteDe100Avisa(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "60"}) })
	saida := capturaSaida(t, func() error { return Socio(conn, []string{"listar"}) })
	if !strings.Contains(saida, "Aviso") {
		t.Errorf("soma de 60%% deveria avisar que falta completar 100%%:\n%s", saida)
	}
}

func TestCapitalAportarCriaLancamentoEAporte(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "60"}) })
	var socioID int64
	if err := conn.QueryRow(`SELECT id FROM socios LIMIT 1`).Scan(&socioID); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Capital(conn, []string{"aportar", "--socio", strconv.FormatInt(socioID, 10), "--valor", "5000"})
	})

	var tipo, categoria, status string
	var valor int64
	if err := conn.QueryRow(`SELECT tipo, categoria, status, valor FROM lancamentos LIMIT 1`).
		Scan(&tipo, &categoria, &status, &valor); err != nil {
		t.Fatal(err)
	}
	if tipo != "receber" || categoria != "capital" || status != "quitado" || valor != 500000 {
		t.Errorf("lançamento do aporte = tipo=%q cat=%q status=%q valor=%d, quer receber/capital/quitado/500000",
			tipo, categoria, status, valor)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM aportes_capital WHERE socio_id = ?`, socioID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("aportes_capital tem %d linha(s), quer 1", n)
	}

	saida := capturaSaida(t, func() error { return Capital(conn, []string{"listar"}) })
	if !strings.Contains(saida, "5.000,00") {
		t.Errorf("listagem de capital deveria mostrar 5.000,00 aportado:\n%s", saida)
	}
}

func TestLucroDistribuirDivideProporcional(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "60"}) })
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Amigo", "--participacao", "40"}) })

	silencia(t, func() error { return Lucro(conn, []string{"distribuir", "--valor", "1000", "--quitado"}) })

	rows, err := conn.Query(`
		SELECT s.nome, l.valor, l.status FROM distribuicao_socios ds
		JOIN socios s ON s.id = ds.socio_id
		JOIN lancamentos l ON l.id = ds.lancamento_id
		ORDER BY s.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type linha struct {
		nome   string
		valor  int64
		status string
	}
	var linhas []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.nome, &l.valor, &l.status); err != nil {
			t.Fatal(err)
		}
		linhas = append(linhas, l)
	}
	if len(linhas) != 2 {
		t.Fatalf("distribuição gerou %d linha(s), quer 2", len(linhas))
	}
	if linhas[0].valor != 60000 || linhas[1].valor != 40000 {
		t.Errorf("valores = %d e %d, quer 60000 e 40000 (60%%/40%% de 1000,00)", linhas[0].valor, linhas[1].valor)
	}
	if linhas[0].status != "quitado" || linhas[1].status != "quitado" {
		t.Errorf("status = %q e %q, quer quitado/quitado (--quitado)", linhas[0].status, linhas[1].status)
	}
}

func TestLucroDistribuirFalhaSemSomar100(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "60"}) })
	if err := Lucro(conn, []string{"distribuir", "--valor", "1000"}); err == nil {
		t.Error("distribuir com participações somando 60% deveria falhar")
	}
}

func TestLucroDistribuirAvisaSeExcedeLucroAcumulado(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "100"}) })

	// sem nenhum lucro acumulado: distribuir qualquer valor deve avisar
	saida := capturaSaida(t, func() error { return Lucro(conn, []string{"distribuir", "--valor", "1000"}) })
	if !strings.Contains(saida, "Aviso") {
		t.Errorf("distribuir sem lucro acumulado deveria avisar, saída:\n%s", saida)
	}

	// com lucro acumulado suficiente (receita de 2.000,00 quitada), não avisa
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "receber", Desc: "Cliente", Valor: 200000, Cat: "servico", Venc: "2026-06-10", Quitado: true,
	}); err != nil {
		t.Fatal(err)
	}
	saida = capturaSaida(t, func() error { return Lucro(conn, []string{"distribuir", "--valor", "500"}) })
	if strings.Contains(saida, "Aviso") {
		t.Errorf("distribuir dentro do lucro acumulado não deveria avisar, saída:\n%s", saida)
	}
}

func TestImpostoInvestimentoComParcelasERepetir(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Investimento(conn, []string{"add", "--desc", "Notebook", "--valor", "1200", "--parcelas", "3"})
	})
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE categoria = 'investimento'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("investimento parcelado gerou %d lançamento(s), quer 3", n)
	}

	if err := Imposto(conn, []string{"add", "--desc", "DAS", "--valor", "250", "--recorrente", "--dia", "20", "--parcelas", "3"}); err == nil {
		t.Error("--parcelas com --recorrente deveria falhar")
	}
}

func TestSocioRemoverComAporteFalha(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "100"}) })
	var socioID int64
	if err := conn.QueryRow(`SELECT id FROM socios LIMIT 1`).Scan(&socioID); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Capital(conn, []string{"aportar", "--socio", strconv.FormatInt(socioID, 10), "--valor", "100"})
	})
	if err := Socio(conn, []string{"remover", strconv.FormatInt(socioID, 10)}); err == nil {
		t.Error("remover sócio com aporte vinculado deveria falhar")
	}
}

func TestImpostoEInvestimentoAdd(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Imposto(conn, []string{"add", "--desc", "DAS", "--valor", "250"}) })
	silencia(t, func() error { return Investimento(conn, []string{"add", "--desc", "Notebook", "--valor", "4500"}) })

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE categoria = 'imposto'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("lançamentos de imposto = %d, quer 1", n)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE categoria = 'investimento'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("lançamentos de investimento = %d, quer 1", n)
	}
}

func TestLucroCalcularExcluiCapitalEDistribuicao(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Socio(conn, []string{"add", "--nome", "Você", "--participacao", "100"}) })
	var socioID int64
	if err := conn.QueryRow(`SELECT id FROM socios LIMIT 1`).Scan(&socioID); err != nil {
		t.Fatal(err)
	}
	// aporte de capital não conta como receita
	silencia(t, func() error {
		return Capital(conn, []string{"aportar", "--socio", strconv.FormatInt(socioID, 10), "--valor", "10000"})
	})
	// receita operacional de verdade
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "receber", Desc: "Cliente", Valor: 200000, Cat: "servico", Venc: "2026-06-10", Quitado: true,
	}); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error { return Lucro(conn, []string{"distribuir", "--valor", "1000", "--quitado"}) })

	saida := capturaSaida(t, func() error { return Lucro(conn, []string{"calcular", "--de", "2026-01-01", "--ate", "2026-12-31"}) })
	if !strings.Contains(saida, "2.000,00") {
		t.Errorf("lucro deveria contar a receita de 2.000,00, saída:\n%s", saida)
	}
	if strings.Contains(saida, "100,00") || strings.Contains(saida, "10,00") {
		t.Errorf("lucro não deveria ser afetado pelo aporte/distribuição:\n%s", saida)
	}
}
