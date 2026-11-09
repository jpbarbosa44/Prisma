package app

import (
	"testing"
	"time"
)

func TestRelatorioIR(t *testing.T) {
	conn := abreDB(t)
	antes := relogio
	relogio = func() time.Time { return time.Date(2026, 3, 10, 10, 0, 0, 0, time.Local) }
	defer func() { relogio = antes }()
	conn.Exec(`INSERT INTO contas (id, nome, banco, saldo_inicial) VALUES (1, 'Corrente', 'Nubank', 100000)`)
	cria := func(p LancamentoParams) {
		t.Helper()
		p.ContaID = 1
		p.Quitado = true
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "Dentista", Valor: 30000, Cat: "Dentista", Venc: "2025-04-10"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Consulta", Valor: 20000, Cat: "saúde", Venc: "2025-08-01"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Faculdade", Valor: 400000, Cat: "educacao", Venc: "2025-02-05"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Mercado", Valor: 50000, Cat: "mercado", Venc: "2025-05-05"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Dentista 2026", Valor: 10000, Cat: "dentista", Venc: "2026-01-05"})
	cria(LancamentoParams{Tipo: "receber", Desc: "Salário", Valor: 500000, Cat: "salario", Venc: "2025-06-05"})
	cria(LancamentoParams{Tipo: "receber", Desc: "Reembolso", Valor: 9999, Cat: "reembolso", Venc: "2025-06-05"})

	d, err := RelatorioIR(conn, 0) // 0 = ano passado (2025)
	if err != nil {
		t.Fatal(err)
	}
	if d.Ano != 2025 {
		t.Fatalf("ano: %d", d.Ano)
	}
	saude, educ := d.Deducoes[0], d.Deducoes[1]
	if saude.Total != 50000 || len(saude.Itens) != 2 {
		t.Errorf("saúde (acento e caixa não importam; 2026 fica fora): %+v", saude)
	}
	if educ.Total != 400000 || educ.Aviso == "" {
		t.Errorf("educação acima do limite avisa: %+v", educ)
	}
	if d.TotalDeduz != 450000 {
		t.Errorf("total dedutível: %d", d.TotalDeduz)
	}
	if len(d.Rendimentos) != 1 || d.Rendimentos[0].Categoria != "salario" || d.TotalRend != 500000 {
		t.Errorf("rendimentos (sem reembolso): %+v", d.Rendimentos)
	}
	// corrente: 1.000 em 31/12/2024; em 31/12/2025: 1.000 − 300 − 200 − 4.000 − 500 + 5.000 + 99,99
	if len(d.Bens) != 1 || d.Bens[0].Anterior != 100000 || d.Bens[0].Atual != 100000+500000+9999-500000 {
		t.Errorf("bens: %+v", d.Bens)
	}

	// categorias trocadas pelo usuário
	if err := IRCmd(conn, []string{"categorias", "--saude", "farmacia"}); err != nil {
		t.Fatal(err)
	}
	d, _ = RelatorioIR(conn, 2025)
	if d.Deducoes[0].Total != 0 {
		t.Errorf("saúde só com farmácia: %+v", d.Deducoes[0])
	}
	IRCmd(conn, []string{"categorias", "--saude", "-"})
	if d, _ = RelatorioIR(conn, 2025); d.Deducoes[0].Total != 50000 {
		t.Errorf("de volta ao padrão: %+v", d.Deducoes[0])
	}
}
