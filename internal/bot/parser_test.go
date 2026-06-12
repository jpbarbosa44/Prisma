package bot

import (
	"testing"
	"time"

	"prisma/internal/app"
)

// agora fixa os testes em 12/06/2026 (uma sexta-feira).
var agora = time.Date(2026, 6, 12, 15, 0, 0, 0, time.UTC)

func TestParseMensagem(t *testing.T) {
	casos := []struct {
		msg  string
		quer app.LancamentoParams
	}{
		{
			"25,50 #mercado pão e leite !",
			app.LancamentoParams{Tipo: "pagar", Desc: "pão e leite", Valor: 2550, Cat: "mercado", Venc: "2026-06-12", Quitado: true},
		},
		{
			"+3500 #salario salário de junho @05/07",
			app.LancamentoParams{Tipo: "receber", Desc: "salário de junho", Valor: 350000, Cat: "salario", Venc: "2026-07-05"},
		},
		{
			"899,70 #eletronicos fone novo 3x",
			app.LancamentoParams{Tipo: "pagar", Desc: "fone novo", Valor: 89970, Cat: "eletronicos", Venc: "2026-06-12", Parcelas: 3},
		},
		{
			"1200 #moradia aluguel @05 rep:6",
			app.LancamentoParams{Tipo: "pagar", Desc: "aluguel", Valor: 120000, Cat: "moradia", Venc: "2026-06-05", Repetir: 6},
		},
		{
			// mínimo absoluto: só o valor; descrição herda a categoria default
			"12",
			app.LancamentoParams{Tipo: "pagar", Desc: "geral", Valor: 1200, Venc: "2026-06-12"},
		},
		{
			// só valor e categoria: descrição herda a categoria
			"45 #farmacia",
			app.LancamentoParams{Tipo: "pagar", Desc: "farmacia", Valor: 4500, Cat: "farmacia", Venc: "2026-06-12"},
		},
		{
			// número depois do valor é descrição, não outro valor
			"25,50 #mercado 2 pães",
			app.LancamentoParams{Tipo: "pagar", Desc: "2 pães", Valor: 2550, Cat: "mercado", Venc: "2026-06-12"},
		},
		{
			// marcadores em qualquer ordem; categoria vira minúscula
			"! @ontem #Mercado 99,90 compra do mês",
			app.LancamentoParams{Tipo: "pagar", Desc: "compra do mês", Valor: 9990, Cat: "mercado", Venc: "2026-06-11", Quitado: true},
		},
		{
			"100 #pets ração conta:2 @amanha",
			app.LancamentoParams{Tipo: "pagar", Desc: "ração", Valor: 10000, Cat: "pets", Venc: "2026-06-13", ContaID: 2},
		},
		{
			"30 #lazer pizza cart:1",
			app.LancamentoParams{Tipo: "pagar", Desc: "pizza", Valor: 3000, Cat: "lazer", Venc: "2026-06-12", CartID: 1},
		},
		{
			"1.234,56 #viagem passagem @15/07/2026",
			app.LancamentoParams{Tipo: "pagar", Desc: "passagem", Valor: 123456, Cat: "viagem", Venc: "2026-07-15"},
		},
	}
	for _, c := range casos {
		tem, err := parseMensagem(c.msg, agora)
		if err != nil {
			t.Errorf("parseMensagem(%q): erro inesperado: %v", c.msg, err)
			continue
		}
		if tem != c.quer {
			t.Errorf("parseMensagem(%q):\n tem  %+v\n quer %+v", c.msg, tem, c.quer)
		}
	}
}

func TestParseMensagemErros(t *testing.T) {
	casos := []string{
		"",                     // vazia
		"pão e leite #mercado", // sem valor
		"25,50 @32",            // dia inexistente
		"25,50 @30/02",         // fevereiro não tem dia 30
		"25,50 @sexta",         // marcador de data desconhecido
		"25,555 #mercado três casas decimais",
	}
	for _, msg := range casos {
		if _, err := parseMensagem(msg, agora); err == nil {
			t.Errorf("parseMensagem(%q): esperava erro, veio nil", msg)
		}
	}
}
