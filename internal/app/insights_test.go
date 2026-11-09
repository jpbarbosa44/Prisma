package app

import (
	"strings"
	"testing"
	"time"
)

func TestInsights(t *testing.T) {
	conn := abreDB(t)
	hoje := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	antes := relogio
	relogio = func() time.Time { return hoje }
	defer func() { relogio = antes }()
	cria := func(p LancamentoParams) int64 {
		t.Helper()
		c, _, _, err := CriarLancamentos(conn, p)
		if err != nil {
			t.Fatal(err)
		}
		return c[0].ID
	}
	conn.Exec(`INSERT INTO contas (id, nome, saldo_inicial) VALUES (1, 'Nu', 500000)`)

	// delivery: R$ 100/mês em ago-out, R$ 200 em novembro → +100%
	for _, m := range []string{"08", "09", "10"} {
		cria(LancamentoParams{Tipo: "pagar", Desc: "ifood", Valor: 10000, Cat: "delivery", Venc: "2026-" + m + "-05", Quitado: true})
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "ifood", Valor: 20000, Cat: "delivery", Venc: "2026-11-05", Quitado: true})

	// mercado: 6 compras normais de R$ 100 e uma de R$ 900 há 2 dias
	for _, d := range []string{"06-10", "07-10", "08-10", "09-10", "10-10", "10-20"} {
		cria(LancamentoParams{Tipo: "pagar", Desc: "feira", Valor: 10000, Cat: "mercado", Venc: "2026-" + d, Quitado: true})
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "Atacadão do mês", Valor: 90000, Cat: "mercado", Venc: "2026-11-18", Quitado: true})

	// assinatura: 39,90 → 44,90
	conn.Exec(`INSERT INTO recorrencias (id, tipo, descricao, valor, dia, inicio, assinatura) VALUES (1, 'pagar', 'Netflix', 4490, 15, '2026-01-15', 1)`)
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, status, recorrencia_id) VALUES
		('pagar', 'Netflix', 3990, '2026-10-15', 'quitado', 1), ('pagar', 'Netflix', 4490, '2026-11-15', 'quitado', 1)`)

	// plano de lazer: 90%
	Plano(conn, []string{"add", "--cat", "lazer", "--valor", "100", "--ref", "2026-11"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "cinema", Valor: 9000, Cat: "lazer", Venc: "2026-11-12", Quitado: true})

	// conta grande dia 28: o saldo previsto fica negativo
	cria(LancamentoParams{Tipo: "pagar", Desc: "IPVA", Valor: 900000, Cat: "impostos", Venc: "2026-11-28"})

	is, err := Insights(conn, hoje)
	if err != nil {
		t.Fatal(err)
	}
	tipos := map[string]Insight{}
	for _, i := range is {
		tipos[i.Tipo] = i
	}
	for _, quer := range []struct{ tipo, grav, trecho string }{
		{"saldo", GravAlta, "28/11/2026"},
		{"orcamento", GravMedia, "lazer do mês em 90%"},
		{"categoria", GravMedia, "delivery: R$ 200,00 no mês, 100% acima"},
		{"assinatura", GravMedia, "Netflix ficou mais cara: de R$ 39,90 para R$ 44,90"},
		{"incomum", GravBaixa, "Atacadão do mês"},
	} {
		i, ok := tipos[quer.tipo]
		if !ok {
			t.Errorf("faltou o insight %s; veio %+v", quer.tipo, is)
			continue
		}
		if i.Gravidade != quer.grav || !strings.Contains(i.Texto, quer.trecho) {
			t.Errorf("%s: %+v (queria %q, %s)", quer.tipo, i, quer.trecho, quer.grav)
		}
	}
	if is[0].Gravidade != GravAlta {
		t.Errorf("os graves vêm primeiro: %+v", is[0])
	}
	// banco sem nada: sem insights
	vazio := abreDB(t)
	if is, err := Insights(vazio, hoje); err != nil || len(is) != 0 {
		t.Errorf("banco vazio: %+v %v", is, err)
	}
}
