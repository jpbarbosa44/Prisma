package app

import (
	"strings"
	"testing"
	"time"
)

func TestMetaCalculos(t *testing.T) {
	hoje := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	m := Meta{Alvo: 120000, Guardado: 30000, Prazo: "2027-04-30"}
	// nov, dez, jan, fev, mar, abr = 6 meses; faltam 900,00 → 150,00/mês
	if m.MesesRestantes(hoje) != 6 || m.PorMes(hoje) != 15000 || m.Percentual() != 25 || m.Falta() != 90000 {
		t.Errorf("meses=%d porMes=%d pct=%d falta=%d", m.MesesRestantes(hoje), m.PorMes(hoje), m.Percentual(), m.Falta())
	}
	vencida := Meta{Alvo: 1000, Prazo: "2026-01-31"}
	if vencida.MesesRestantes(hoje) != 1 || vencida.PorMes(hoje) != 1000 {
		t.Errorf("prazo vencido: falta tudo neste mês, veio %d", vencida.PorMes(hoje))
	}
	if (Meta{Alvo: 1000, Guardado: 1500}).Percentual() != 100 || (Meta{Alvo: 1000}).PorMes(hoje) != 0 {
		t.Error("passou da meta = 100%; sem prazo = sem aporte mensal")
	}
	if p, _ := parsePrazo("2027-02"); p != "2027-02-28" {
		t.Errorf("parsePrazo(2027-02) = %s", p)
	}
	if p, _ := parsePrazo("07/2027"); p != "2027-07-31" {
		t.Errorf("parsePrazo(07/2027) = %s", p)
	}
}

func TestMetaCmd(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return MetaCmd(conn, []string{"add", "--nome", "Viagem", "--valor", "3000", "--prazo", "2030-12"})
	})
	silencia(t, func() error { return MetaCmd(conn, []string{"add", "--nome", "Reserva", "--valor", "1000"}) })
	silencia(t, func() error { return MetaCmd(conn, []string{"aportar", "1", "--valor", "1000"}) })
	silencia(t, func() error { return MetaCmd(conn, []string{"retirar", "1", "--valor", "250"}) })
	saida := capturaSaida(t, func() error { return MetaCmd(conn, []string{"aportar", "2", "--valor", "1000"}) })
	if !strings.Contains(saida, "Meta alcançada") {
		t.Errorf("aportar até o alvo devia comemorar:\n%s", saida)
	}
	metas, err := ListaMetas(conn, false)
	if err != nil || len(metas) != 2 {
		t.Fatalf("metas = %+v, %v", metas, err)
	}
	if metas[0].Nome != "Viagem" || metas[0].Guardado != 75000 || metas[0].Prazo != "2030-12-31" {
		t.Errorf("Viagem = %+v", metas[0])
	}
	saida = capturaSaida(t, func() error { return MetaCmd(conn, []string{"listar"}) })
	for _, want := range []string{"Viagem", "25%", "R$ 750,00", "Reserva", "alcançada"} {
		if !strings.Contains(saida, want) {
			t.Errorf("listar devia mostrar %q:\n%s", want, saida)
		}
	}

	silencia(t, func() error { return MetaCmd(conn, []string{"editar", "1", "--prazo", "-"}) })
	silencia(t, func() error { return MetaCmd(conn, []string{"concluir", "2"}) })
	if metas, _ = ListaMetas(conn, false); len(metas) != 1 || metas[0].Prazo != "" {
		t.Errorf("depois de concluir e tirar o prazo: %+v", metas)
	}
	silencia(t, func() error { return MetaCmd(conn, []string{"remover", "1"}) })
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM metas_aportes WHERE meta_id = 1`).Scan(&n)
	if n != 0 {
		t.Errorf("remover a meta devia apagar os aportes (sobraram %d)", n)
	}
	if err := MetaCmd(conn, []string{"aportar", "99", "--valor", "1"}); err == nil {
		t.Error("meta inexistente devia dar erro")
	}
}
