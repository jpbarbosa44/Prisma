package bot

import (
	"strings"
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

func TestParseCorrecao(t *testing.T) {
	c, err := parseCorrecao("27,90", agora)
	if err != nil || c.Valor == nil || *c.Valor != 2790 || c.Cat != nil || c.Desc != nil {
		t.Errorf("parseCorrecao(27,90): %+v, err=%v", c, err)
	}
	c, err = parseCorrecao("#mercado pão integral @ontem !", agora)
	if err != nil || c.Cat == nil || *c.Cat != "mercado" || c.Desc == nil || *c.Desc != "pão integral" ||
		c.Venc == nil || *c.Venc != "2026-06-11" || !c.Quitado || c.Valor != nil {
		t.Errorf("parseCorrecao(completa): %+v, err=%v", c, err)
	}
	if _, err := parseCorrecao("", agora); err == nil {
		t.Error("parseCorrecao(vazia): esperava erro")
	}
}

func TestParseTransferencia(t *testing.T) {
	tr, err := parseTransferencia("200 conta:1 cart:2 saque do mês")
	if err != nil || tr.Valor != 20000 || tr.De != "conta:1" || tr.Para != "carteira:2" || tr.Desc != "saque do mês" {
		t.Errorf("parseTransferencia: %+v, err=%v", tr, err)
	}
	tr, err = parseTransferencia("carteira:1 conta:2 50,75")
	if err != nil || tr.Valor != 5075 || tr.De != "carteira:1" || tr.Para != "conta:2" {
		t.Errorf("parseTransferencia(ordem livre): %+v, err=%v", tr, err)
	}
	for _, msg := range []string{"", "200", "200 conta:1", "conta:1 cart:2"} {
		if _, err := parseTransferencia(msg); err == nil {
			t.Errorf("parseTransferencia(%q): esperava erro", msg)
		}
	}
}

func TestParsePeriodoConsulta(t *testing.T) {
	casos := []struct {
		per  string
		quer []string
	}{
		{"", []string{"--mes", "2026-06"}},
		{"maio", []string{"--mes", "2026-05"}},
		{"dezembro", []string{"--mes", "2025-12"}}, // mês futuro = ano passado
		{"junho", []string{"--mes", "2026-06"}},
		{"3m", []string{"--de", "2026-03-12"}},
		{"2026-01", []string{"--mes", "2026-01"}},
		{"tudo", nil},
	}
	for _, c := range casos {
		tem, err := parsePeriodoConsulta(c.per, agora)
		if err != nil {
			t.Errorf("parsePeriodoConsulta(%q): erro: %v", c.per, err)
			continue
		}
		if strings.Join(tem, " ") != strings.Join(c.quer, " ") {
			t.Errorf("parsePeriodoConsulta(%q): tem %v, quer %v", c.per, tem, c.quer)
		}
	}
	if _, err := parsePeriodoConsulta("sexta", agora); err == nil {
		t.Error("parsePeriodoConsulta(sexta): esperava erro")
	}
}

func TestConsultaCategoria(t *testing.T) {
	if cat, per, ok := consultaCategoria("#mercado"); !ok || cat != "mercado" || per != "" {
		t.Errorf("consultaCategoria(#mercado): %q %q %v", cat, per, ok)
	}
	if cat, per, ok := consultaCategoria("#Mercado maio"); !ok || cat != "mercado" || per != "maio" {
		t.Errorf("consultaCategoria(#Mercado maio): %q %q %v", cat, per, ok)
	}
	// com valor é lançamento, não consulta
	for _, msg := range []string{"#mercado 25,50", "25,50 #mercado", "#mercado pão e leite", "#"} {
		if _, _, ok := consultaCategoria(msg); ok {
			t.Errorf("consultaCategoria(%q): não devia ser consulta", msg)
		}
	}
}
