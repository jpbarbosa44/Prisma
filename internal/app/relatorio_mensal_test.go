package app

import "testing"

func TestRelatorioMensal(t *testing.T) {
	conn := abreDB(t)
	cria := func(tipo, desc, cat, data string, valor int64) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: tipo, Desc: desc, Cat: cat, Valor: valor, Venc: data, Quitado: true}); err != nil {
			t.Fatal(err)
		}
	}
	cria("receber", "Salário", "salario", "2026-10-05", 500000)
	cria("pagar", "Aluguel", "moradia", "2026-10-10", 150000)
	cria("pagar", "Mercado", "mercado", "2026-10-12", 90000)
	cria("pagar", "Feira", "mercado", "2026-10-20", 30000)
	// meses anteriores: mercado custava 60.000/mês (média dos 3)
	for _, m := range []string{"2026-07-12", "2026-08-12", "2026-09-12"} {
		cria("pagar", "Mercado", "mercado", m, 60000)
	}
	cria("pagar", "Fora do mês", "lazer", "2026-11-01", 99900)

	d, err := RelatorioMensal(conn, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if d.Receitas != 500000 || d.Despesas != 270000 || d.Resultado() != 230000 || d.Poupanca() != 46 {
		t.Errorf("totais: rec=%d desp=%d res=%d poup=%d", d.Receitas, d.Despesas, d.Resultado(), d.Poupanca())
	}
	if len(d.Categorias) != 2 || d.Categorias[0].Rotulo != "moradia" || d.Categorias[1].Valor != 120000 || d.Categorias[1].Media != 60000 {
		t.Errorf("categorias: %+v", d.Categorias)
	}
	if len(d.Maiores) != 3 || d.Maiores[0].Descricao != "Aluguel" {
		t.Errorf("maiores: %+v", d.Maiores)
	}
	if len(d.Mensal) != 6 || d.Mensal[5].Mes != "2026-10" || d.Mensal[5].Desp != 270000 || d.Mensal[0].Mes != "2026-05" {
		t.Errorf("mensal: %+v", d.Mensal)
	}
	if _, err := RelatorioMensal(conn, "outubro"); err == nil {
		t.Error("mês inválido devia dar erro")
	}
}
