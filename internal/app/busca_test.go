package app

import (
	"testing"
)

func TestBuscar(t *testing.T) {
	conn := abreDB(t)
	cria := func(p LancamentoParams) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "Pão de Açúcar", Valor: 8990, Cat: "mercado", Venc: "2026-03-10"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Hotel #viagem-2026", Valor: 120000, Cat: "lazer", Venc: "2026-07-02"})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Passagem #viagem", Valor: 80000, Cat: "transporte", Venc: "2026-06-01"})
	cria(LancamentoParams{Tipo: "receber", Desc: "Salário", Valor: 500000, Cat: "salario", Venc: "2026-07-05", Quitado: true})
	conn.Exec(`INSERT INTO contas (nome, banco) VALUES ('Nubank', 'Nu Pagamentos')`)
	conn.Exec(`INSERT INTO metas (nome, alvo) VALUES ('Viagem pro Japão', 1500000)`)

	casos := []struct {
		termo  string
		titulo []string
	}{
		{"acucar", []string{"Pão de Açúcar"}},            // sem acento acha com acento
		{"PÃO DE", []string{"Pão de Açúcar"}},            // caixa; "Japão" tem "pao" mas não "de"
		{"89,90", []string{"Pão de Açúcar"}},             // valor exato
		{"89.5", []string{"Pão de Açúcar"}},              // a menos de 1%
		{"mercado 89,90", []string{"Pão de Açúcar"}},     // termo + valor
		{"mercado 50", nil},                              // valor que não bate
		{"#viagem", []string{"Passagem #viagem"}},        // tag inteira, não #viagem-2026
		{"#viagem-2026", []string{"Hotel #viagem-2026"}}, // a outra tag
		{"viagem", []string{"Hotel #viagem-2026", "Passagem #viagem", "Viagem pro Japão"}},
		{"salario", []string{"Salário"}},
		{"nu pagamentos", []string{"Nubank"}}, // conta pelo banco
		{"", nil},
	}
	for _, c := range casos {
		rs, err := Buscar(conn, c.termo, 0)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range rs {
			got = append(got, r.Titulo)
		}
		if len(got) != len(c.titulo) {
			t.Errorf("%q: %v, quero %v", c.termo, got, c.titulo)
			continue
		}
		for i := range got {
			if got[i] != c.titulo[i] {
				t.Errorf("%q: %v, quero %v", c.termo, got, c.titulo)
				break
			}
		}
	}
	rs, _ := Buscar(conn, "salario", 0)
	if rs[0].Tipo != "lancamento" || !rs[0].Quitado || rs[0].Tela != "Pagar/Receber" || rs[0].Data != "2026-07-05" {
		t.Errorf("detalhes do lançamento: %+v", rs[0])
	}
	if rs, _ := Buscar(conn, "viagem", 1); len(rs) != 1 {
		t.Errorf("limite: %d", len(rs))
	}
}
