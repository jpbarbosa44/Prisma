package app

import (
	"testing"
	"time"
)

func TestFaturaDe(t *testing.T) {
	d := func(s string) time.Time {
		t, _ := time.Parse("2006-01-02", s)
		return t
	}
	casos := []struct {
		nome             string
		fech, venc       int
		compra           string
		querRef, querVnc string
	}{
		// vencimento depois do fechamento: vence no mesmo mês do fechamento
		{"antes do fechamento", 20, 27, "2026-06-10", "2026-06", "2026-06-27"},
		{"depois do fechamento", 20, 27, "2026-06-25", "2026-07", "2026-07-27"},
		{"no dia do fechamento", 20, 27, "2026-06-20", "2026-06", "2026-06-27"},
		// vencimento antes/igual ao fechamento: vence no mês seguinte
		{"venc no mês seguinte", 25, 5, "2026-06-10", "2026-07", "2026-07-05"},
		{"venc no mês seguinte, após fechar", 25, 5, "2026-06-28", "2026-08", "2026-08-05"},
		// dia preso ao fim do mês: vencimento 31 numa fatura de fevereiro
		{"dia 31 vira fim de fevereiro", 20, 31, "2026-01-25", "2026-02", "2026-02-28"},
	}
	for _, c := range casos {
		ref, venc := faturaDe(c.fech, c.venc, d(c.compra))
		if ref != c.querRef || venc != c.querVnc {
			t.Errorf("%s: faturaDe(%d,%d,%s) = (%s,%s), quer (%s,%s)",
				c.nome, c.fech, c.venc, c.compra, ref, venc, c.querRef, c.querVnc)
		}
	}
}

func TestCartaoNaoMexeSaldoAteAFatura(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 500000)`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Nubank", "--fechamento", "20", "--vencimento", "27", "--conta", "1"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Tênis", "--valor", "400", "--cartao", "1", "--venc", "10/06/2026"})
	})

	// nada pago ainda: o saldo não muda
	if total, _ := saldoTotal(conn); total != 500000 {
		t.Errorf("saldo antes de pagar a fatura = %d, quer 500000 (cartão não debita na hora)", total)
	}
	// paga a fatura de junho: agora o saldo cai
	silencia(t, func() error {
		return Fatura(conn, []string{"pagar", "--cartao", "1", "--ref", "2026-06"})
	})
	if total, _ := saldoTotal(conn); total != 460000 {
		t.Errorf("saldo após pagar a fatura = %d, quer 460000", total)
	}
	if sc, _ := saldoConta(conn, 1); sc != 460000 {
		t.Errorf("saldo da conta = %d, quer 460000 (a fatura debitou a conta do cartão)", sc)
	}
}

func TestCartaoParcelasEspalhamNasFaturas(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Visa", "--fechamento", "20", "--vencimento", "27"})
	})
	if _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "TV", Valor: 30000, Cat: "eletronicos",
		Venc: "2026-06-10", CartaoID: 1, Parcelas: 3,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(`SELECT vencimento, data_compra, valor FROM lancamentos ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	querVenc := []string{"2026-06-27", "2026-07-27", "2026-08-27"}
	querCompra := []string{"2026-06-10", "2026-07-10", "2026-08-10"}
	i := 0
	for rows.Next() {
		var venc, compra string
		var valor int64
		if err := rows.Scan(&venc, &compra, &valor); err != nil {
			t.Fatal(err)
		}
		if i >= 3 {
			t.Fatalf("mais parcelas que o esperado")
		}
		if venc != querVenc[i] || compra != querCompra[i] || valor != 10000 {
			t.Errorf("parcela %d: venc=%s compra=%s valor=%d, quer %s/%s/10000",
				i+1, venc, compra, valor, querVenc[i], querCompra[i])
		}
		i++
	}
	if i != 3 {
		t.Errorf("esperava 3 parcelas, veio %d", i)
	}
}

func TestCartaoCompetenciaNoPlano(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Elo", "--fechamento", "20", "--vencimento", "27"})
	})
	silencia(t, func() error {
		return Plano(conn, []string{"add", "--cat", "eletronicos", "--valor", "1000", "--periodo", "mes", "--ref", "2026-06"})
	})
	// compra de cartão em junho, ainda PENDENTE (fatura não paga)
	if _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Fone", Valor: 30000, Cat: "eletronicos",
		Venc: "2026-06-10", CartaoID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	usos, err := PlanosDaCategoria(conn, "eletronicos", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(usos) != 1 || usos[0].Gasto != 30000 {
		t.Errorf("plano deveria contar o gasto de cartão por data da compra: %+v", usos)
	}
}

func TestCartaoFaturaInicial(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Cartao(conn, []string{
			"add", "--nome", "Master", "--fechamento", "20", "--vencimento", "27", "--fatura-atual", "1.200,00",
		})
	})
	// a fatura inicial vira um lançamento pendente vinculado ao cartão
	var n int
	var valor int64
	if err := conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(valor),0) FROM lancamentos WHERE cartao_id = 1 AND status = 'pendente'`,
	).Scan(&n, &valor); err != nil {
		t.Fatal(err)
	}
	if n != 1 || valor != 120000 {
		t.Errorf("fatura inicial: n=%d valor=%d, quer 1 e 120000", n, valor)
	}
}

func TestCartaoSoPagar(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "X", "--fechamento", "10", "--vencimento", "20"})
	})
	if _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "receber", Desc: "errado", Valor: 100, Venc: "2026-06-10", CartaoID: 1,
	}); err == nil {
		t.Error("cartão numa receita deveria falhar")
	}
}
