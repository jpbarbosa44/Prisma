package app

import (
	"strings"
	"testing"
	"time"
)

func TestPlanoAcumula(t *testing.T) {
	conn := abreDB(t)
	// mercado: R$ 500 por mês, acumulando, de set a nov
	if err := Plano(conn, []string{"add", "--cat", "mercado", "--valor", "500", "--ref", "2026-09", "--repetir", "3", "--acumula"}); err != nil {
		t.Fatal(err)
	}
	gasta := func(v int64, d string) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: "pagar", Desc: "m", Valor: v, Cat: "mercado", Venc: d, Quitado: true}); err != nil {
			t.Fatal(err)
		}
	}
	gasta(30000, "2026-09-10") // sobra 200
	gasta(80000, "2026-10-10") // 500+200 = 700 → estoura 100: não passa adiante
	gasta(45000, "2026-11-05")
	sobraOut, _ := sobraAcumulada(conn, "mercado", "mes", "2026-10")
	sobraNov, _ := sobraAcumulada(conn, "mercado", "mes", "2026-11")
	if sobraOut != 20000 || sobraNov != 0 {
		t.Fatalf("sobras: out=%d nov=%d", sobraOut, sobraNov)
	}
	usos, _ := PlanosDaCategoria(conn, "mercado", "2026-10-15")
	if len(usos) != 1 || usos[0].Limite != 70000 || usos[0].Gasto != 80000 {
		t.Fatalf("outubro: %+v", usos)
	}
	// sem acumular, o limite volta ao base
	conn.Exec(`UPDATE planejamentos SET acumula = 0 WHERE ref = '2026-10'`)
	if usos, _ := PlanosDaCategoria(conn, "mercado", "2026-10-15"); usos[0].Limite != 50000 {
		t.Fatalf("sem acumular: %+v", usos)
	}

	// aviso no lançamento e alertas do mês (novembro: 450 de 500 = 90%)
	if a := AvisoPlano(conn, "mercado", "2026-11-20"); !strings.Contains(a, "90%") {
		t.Fatalf("aviso: %q", a)
	}
	if a := AvisoPlano(conn, "lazer", "2026-11-20"); a != "" {
		t.Fatalf("sem plano, sem aviso: %q", a)
	}
	hoje := time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)
	novos, err := NovosAlertasOrcamento(conn, hoje, "app")
	if err != nil || len(novos) != 1 || novos[0].Pct() != 90 {
		t.Fatalf("novos: %+v %v", novos, err)
	}
	if novos, _ := NovosAlertasOrcamento(conn, hoje, "app"); len(novos) != 0 {
		t.Fatal("o mesmo alerta não repete")
	}
	if novos, _ := NovosAlertasOrcamento(conn, hoje, "bot"); len(novos) != 1 {
		t.Fatal("cada canal avisa uma vez")
	}
	gasta(10000, "2026-11-21") // estoura: alerta novo (nível 100)
	if novos, _ := NovosAlertasOrcamento(conn, hoje, "app"); len(novos) != 1 || novos[0].Gasto <= novos[0].Limite {
		t.Fatalf("estouro: %+v", novos)
	}
}
