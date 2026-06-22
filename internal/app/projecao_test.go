package app

import (
	"testing"
	"time"
)

// TestRecorrenciasNoMesMensalEAnual confere o cálculo da base das projeções:
// uma recorrência mensal conta em qualquer mês vigente; uma anual só no mês de
// aniversário; e a vigência (início) é respeitada.
func TestRecorrenciasNoMesMensalEAnual(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "receber", "--desc", "Salário",
			"--valor", "5000", "--dia", "5", "--inicio", "2026-01-10", "--passados", "manter",
		})
	})
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "pagar", "--desc", "IPVA", "--intervalo", "anual",
			"--valor", "1200", "--dia", "10", "--inicio", "2026-03-10", "--passados", "manter",
		})
	})

	// mensal: presente em março (mês qualquer >= início)
	rec, n, err := recorrenciasNoMes(conn, "receber", "2026-05")
	if err != nil {
		t.Fatal(err)
	}
	if rec != 500000 || n != 1 {
		t.Fatalf("mensal em 2026-05 = %d (n=%d), queria 500000 (n=1)", rec, n)
	}
	// mensal antes do início não conta
	if rec, n, _ := recorrenciasNoMes(conn, "receber", "2025-12"); rec != 0 || n != 0 {
		t.Fatalf("mensal antes do início devia ser 0, veio %d (n=%d)", rec, n)
	}
	// anual: só no mês de aniversário (março)
	if desp, n, _ := recorrenciasNoMes(conn, "pagar", "2026-03"); desp != 120000 || n != 1 {
		t.Fatalf("anual em março = %d (n=%d), queria 120000 (n=1)", desp, n)
	}
	if desp, n, _ := recorrenciasNoMes(conn, "pagar", "2026-07"); desp != 0 || n != 0 {
		t.Fatalf("anual fora de março devia ser 0, veio %d (n=%d)", desp, n)
	}
}

// TestPrevistoMesUsaRecorrenciaAlemDoHorizonte é a regressão da correção que fez
// as projeções considerarem as recorrências: além do horizonte de materialização
// (3 meses), o mês deriva da regra cadastrada (fonte "≈"), em vez de cair para a
// média histórica (que aqui é zero) e projetar errado.
func TestPrevistoMesUsaRecorrenciaAlemDoHorizonte(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "receber", "--desc", "Salário",
			"--valor", "5000", "--dia", "5", "--passados", "manter",
		})
	})

	// mês 1 (dentro do horizonte): vem dos lançamentos já materializados (fonte "")
	dentro := time.Now().AddDate(0, 1, 0).Format("2006-01")
	if p, err := periodoMes(dentro); err == nil {
		val, fonte, err := previstoMes(conn, "receber", p, 0)
		if err != nil {
			t.Fatal(err)
		}
		if val != 500000 || fonte != "" {
			t.Fatalf("mês dentro do horizonte = %d/%q, queria 500000/\"\"", val, fonte)
		}
	}

	// mês 5 (além do horizonte de 3 meses): deriva da recorrência, fonte "≈"
	fora := time.Now().AddDate(0, 5, 0).Format("2006-01")
	p, err := periodoMes(fora)
	if err != nil {
		t.Fatal(err)
	}
	val, fonte, err := previstoMes(conn, "receber", p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if val != 500000 || fonte != "≈" {
		t.Fatalf("mês além do horizonte = %d/%q, queria 500000/\"≈\"", val, fonte)
	}
}
