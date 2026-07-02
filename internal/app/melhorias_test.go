package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseDataDDMM(t *testing.T) {
	got, err := parseData("10/06")
	if err != nil {
		t.Fatalf("parseData(DD/MM): %v", err)
	}
	want := fmt.Sprintf("%d-06-10", time.Now().Year())
	if got != want {
		t.Fatalf("parseData(\"10/06\") = %q, quer %q", got, want)
	}
}

func TestParseDataDDMMInvalida(t *testing.T) {
	// dias que não existem não podem ser normalizados em silêncio (o time.Parse
	// sem ano cai no ano 0, bissexto, e 29/02 virava 01/03)
	invalidas := []string{"31/06", "31/04", "32/01", "10/13", "0/05"}
	ano := time.Now().Year()
	if !(ano%4 == 0 && (ano%100 != 0 || ano%400 == 0)) {
		invalidas = append(invalidas, "29/02")
	}
	for _, s := range invalidas {
		if got, err := parseData(s); err == nil {
			t.Errorf("parseData(%q) = %q, esperava erro", s, got)
		}
	}
}

func TestRemoverParcelaRaizCascade(t *testing.T) {
	conn := abreDB(t)
	criados, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Notebook", Valor: 90000, Venc: "2026-06-10", Parcelas: 3,
	})
	if err != nil || len(criados) != 3 {
		t.Fatalf("criar parcelas: %v (n=%d)", err, len(criados))
	}
	raiz := fmt.Sprint(criados[0].ID)
	silencia(t, func() error { return Lancamentos(conn, []string{"remover", raiz}) })

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("após remover a parcela raiz restaram %d lançamentos, queria 0", n)
	}
}

func TestRemoverParcelaNaoRaizMantemResto(t *testing.T) {
	conn := abreDB(t)
	criados, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Geladeira", Valor: 60000, Venc: "2026-06-10", Parcelas: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	naoRaiz := fmt.Sprint(criados[1].ID)
	silencia(t, func() error { return Lancamentos(conn, []string{"remover", naoRaiz}) })

	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 2 {
		t.Fatalf("ao remover parcela do meio restaram %d, queria 2", n)
	}
}

func TestQuitarVencidos(t *testing.T) {
	conn := abreDB(t)
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Antiga", Valor: 5000, Venc: "2020-01-01", AutoQuit: true,
	}); err != nil {
		t.Fatal(err)
	}
	// uma sem auto-quitar, também vencida, deve continuar pendente
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Manual", Valor: 5000, Venc: "2020-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	n, err := QuitarVencidos(conn)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("QuitarVencidos quitou %d, queria 1", n)
	}
	var pendentes int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE status = 'pendente'`).Scan(&pendentes)
	if pendentes != 1 {
		t.Fatalf("restaram %d pendentes, queria 1 (a sem auto-quitar)", pendentes)
	}
}

func TestCategoriaAutoCadastro(t *testing.T) {
	conn := abreDB(t)
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "TV", Valor: 100000, Cat: "eletronicos", Venc: "2026-06-10",
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM categorias WHERE nome = 'eletronicos'`).Scan(&n)
	if n != 1 {
		t.Fatalf("categoria nova não foi cadastrada no catálogo (n=%d)", n)
	}
}

func TestRecorrenciaPropagaGrupo(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Grupo(conn, []string{"add", "--nome", "Casa", "--pessoas", "Eu, Maria"})
	})
	silencia(t, func() error {
		return Recorrencia(conn, []string{"add", "--tipo", "pagar", "--desc", "Faxina",
			"--valor", "200", "--dia", "5", "--grupo", "1", "--inicio", "2026-06-01", "--passados", "manter"})
	})
	var comGrupo, total int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id IS NOT NULL`).Scan(&total)
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id IS NOT NULL AND grupo_id = 1`).Scan(&comGrupo)
	if total == 0 || comGrupo != total {
		t.Fatalf("grupo não propagou: %d de %d lançamentos gerados têm grupo", comGrupo, total)
	}
}

func TestCartaoRemoveDespesas(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Visa", "--fechamento", "20", "--vencimento", "27"})
	})
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Tênis", Valor: 40000, Venc: "2026-06-05", CartaoID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error { return Cartao(conn, []string{"remover", "1"}) })

	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 0 {
		t.Fatalf("ao remover o cartão restaram %d despesas, queria 0", n)
	}
}

func TestGrupoSomaMesVigente(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Grupo(conn, []string{"add", "--nome", "Casa", "--pessoas", "Eu, Maria"})
	})
	hoje, _ := parseData("hoje")
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Mercado", Valor: 30000, Venc: hoje, GrupoID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	saida := capturaSaida(t, func() error { return Grupo(conn, []string{"listar"}) })
	if !strings.Contains(saida, "MÊS ATUAL") {
		t.Fatalf("listagem de grupo sem a coluna do mês:\n%s", saida)
	}
	// sua parte do mês = 300/2 = 150,00
	if !strings.Contains(saida, "R$ 150,00") {
		t.Fatalf("soma do mês vigente não apareceu:\n%s", saida)
	}
}

func TestEstatisticasNaoQuebra(t *testing.T) {
	conn := abreDB(t)
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Mercado", Valor: 30000, Cat: "mercado", Venc: "2026-06-05", Quitado: true,
	}); err != nil {
		t.Fatal(err)
	}
	saida := capturaSaida(t, func() error { return Estatisticas(conn, []string{"--meses", "3"}) })
	for _, secao := range []string{"RESUMO POR CATEGORIA", "TENDÊNCIA", "TOP GASTOS", "SAÚDE FINANCEIRA"} {
		if !strings.Contains(saida, secao) {
			t.Fatalf("estatísticas sem a seção %q:\n%s", secao, saida)
		}
	}
}
