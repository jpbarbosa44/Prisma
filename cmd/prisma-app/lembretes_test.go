package main

import (
	"strings"
	"testing"

	"prisma/internal/app"
)

func TestTextoLembrete(t *testing.T) {
	avisos := []app.Aviso{
		{Descricao: "Luz", Valor: 18000, Situacao: app.SituacaoAtrasada, Tipo: "pagar"},
		{Descricao: "Aluguel", Valor: 150000, Situacao: app.SituacaoHoje, Tipo: "pagar"},
		{Descricao: "Freela", Valor: 80000, Situacao: app.SituacaoProxima, Tipo: "receber"},
		{Descricao: "Fatura Nubank", Valor: 5000, Situacao: app.SituacaoProxima, Tipo: "fatura"},
		{Descricao: "Internet", Valor: 9990, Situacao: app.SituacaoProxima, Tipo: "pagar"},
	}
	titulo, texto := textoLembrete(avisos)
	if titulo != "Prisma: 1 conta(s) atrasada(s)" {
		t.Errorf("título = %q", titulo)
	}
	for _, want := range []string{"Luz R$ 180,00 (atrasada)", "Aluguel R$ 1.500,00 (hoje)", "receber: Freela", "e mais 1"} {
		if !strings.Contains(texto, want) {
			t.Errorf("texto sem %q:\n%s", want, texto)
		}
	}
	if titulo, _ := textoLembrete(avisos[2:3]); titulo != "Prisma: 1 conta(s) vencem amanhã" {
		t.Errorf("só amanhã: %q", titulo)
	}
}
