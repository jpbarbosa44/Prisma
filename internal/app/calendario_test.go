package app

import (
	"testing"
	"time"
)

func TestCalendario(t *testing.T) {
	conn := abreDB(t)
	antes := relogio
	relogio = func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local) }
	defer func() { relogio = antes }()
	conn.Exec(`INSERT INTO contas (id, nome, saldo_inicial) VALUES (1, 'Nubank', 100000)`) // R$ 1.000
	cria := func(p LancamentoParams) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "Atrasada", Valor: 10000, Venc: "2026-11-05"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Paga", Valor: 5000, Venc: "2026-11-03", Quitado: true})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Aluguel", Valor: 120000, Venc: "2026-11-15"})
	cria(LancamentoParams{Tipo: "receber", Desc: "Salário", Valor: 300000, Venc: "2026-11-20"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Dezembro", Valor: 999, Venc: "2026-12-01"})
	conn.Exec(`INSERT INTO cartoes (id, nome, dia_fechamento, dia_vencimento) VALUES (1, 'Nubank', 1, 12)`)
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, cartao_id) VALUES
		('pagar', 'Mercado', 20000, '2026-11-12', 1), ('pagar', 'Farmácia', 5000, '2026-11-12', 1)`)

	c, err := Calendario(conn, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Dias) != 30 || c.Hoje != "2026-11-10" {
		t.Fatalf("dias %d hoje %s", len(c.Dias), c.Hoje)
	}
	dia := func(n int) DiaCal { return c.Dias[n-1] }
	if d := dia(12); len(d.Itens) != 1 || d.Itens[0].Tipo != "fatura" || d.Itens[0].Valor != 25000 || d.Pagar != 25000 {
		t.Fatalf("fatura agrupada: %+v", d)
	}
	if d := dia(3); len(d.Itens) != 1 || !d.Itens[0].Quitado || d.Pagar != 0 {
		t.Fatalf("quitada aparece mas não pesa: %+v", d)
	}
	if dia(9).TemSaldo || !dia(10).TemSaldo {
		t.Fatal("saldo previsto só de hoje em diante")
	}
	// saldo de agora: 1.000 − 50 (paga) = 950; a atrasada conta hoje: 850
	if dia(10).Saldo != 85000 {
		t.Fatalf("saldo de hoje: %d", dia(10).Saldo)
	}
	if dia(12).Saldo != 60000 || dia(15).Saldo != -60000 || dia(20).Saldo != 240000 {
		t.Fatalf("saldos: 12=%d 15=%d 20=%d", dia(12).Saldo, dia(15).Saldo, dia(20).Saldo)
	}
	if c.MenorSaldo != -60000 || c.DiaMenor != "2026-11-15" {
		t.Fatalf("menor: %d em %s", c.MenorSaldo, c.DiaMenor)
	}
	if c.Pagar != 10000+120000+25000 || c.Receber != 300000 {
		t.Fatalf("totais: %d %d", c.Pagar, c.Receber)
	}
	// mês seguinte começa do saldo de fim de novembro
	d, _ := Calendario(conn, "2026-12")
	if !d.Dias[0].TemSaldo || d.Dias[0].Saldo != 240000-999 {
		t.Fatalf("dezembro: %+v", d.Dias[0])
	}
	// mês passado: nenhum saldo previsto
	o, _ := Calendario(conn, "2026-10")
	if o.DiaMenor != "" {
		t.Fatal("outubro não tem previsão")
	}
	if _, err := Calendario(conn, "2026-13"); err == nil {
		t.Fatal("mês inválido")
	}
}
