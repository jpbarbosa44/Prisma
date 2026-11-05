package app

import (
	"strings"
	"testing"
	"time"
)

// TestVencimentos: atrasadas, de hoje e próximas dentro da janela; compras do
// cartão viram uma linha só por fatura; fora da janela e quitadas não entram.
func TestVencimentos(t *testing.T) {
	conn := abreDB(t)
	hoje := time.Now()
	d := func(dias int) string { return hoje.AddDate(0, 0, dias).Format("2006-01-02") }
	cria := func(p LancamentoParams) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	cria(LancamentoParams{Tipo: "pagar", Desc: "Luz", Valor: 100, Venc: d(-2)})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Água", Valor: 200, Venc: d(0)})
	cria(LancamentoParams{Tipo: "receber", Desc: "Freela", Valor: 300, Venc: d(3)})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Longe", Valor: 400, Venc: d(30)})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Paga", Valor: 500, Venc: d(1), Quitado: true})
	// cartão que fecha e vence em dias que põem as duas compras na mesma fatura
	conn.Exec(`INSERT INTO cartoes (id, nome, dia_fechamento, dia_vencimento) VALUES (1, 'Nubank', 1, 2)`)
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, cartao_id) VALUES
		('pagar', 'Mercado', 1000, ?, 1), ('pagar', 'Farmácia', 500, ?, 1)`, d(5), d(5))

	av, err := Vencimentos(conn, hoje, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(av) != 4 {
		t.Fatalf("avisos = %+v, quer 4 (Luz, Água, Freela, Fatura)", av)
	}
	quer := []struct {
		desc, sit string
		valor     int64
	}{
		{"Luz", SituacaoAtrasada, 100},
		{"Água", SituacaoHoje, 200},
		{"Freela", SituacaoProxima, 300},
		{"Fatura Nubank", SituacaoProxima, 1500},
	}
	for i, q := range quer {
		if av[i].Descricao != q.desc || av[i].Situacao != q.sit || av[i].Valor != q.valor {
			t.Errorf("aviso %d = %+v, quer %+v", i, av[i], q)
		}
	}
	if f := av[3]; f.Tipo != "fatura" || f.ID != 0 || f.CartaoID != 1 || f.Ref != d(5)[:7] {
		t.Errorf("fatura = %+v", f)
	}
}

func TestInicio(t *testing.T) {
	conn := abreDB(t)
	p, err := Inicio(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Vazio {
		t.Fatal("banco novo devia vir Vazio (as categorias de fábrica não contam)")
	}
	saida := capturaSaida(t, func() error { return InicioTexto(conn, nil) })
	if want := "Bem-vindo"; !strings.Contains(saida, want) {
		t.Errorf("InicioTexto no banco vazio devia dar boas-vindas:\n%s", saida)
	}

	conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 100000)`)
	hoje := time.Now().Format("2006-01-02")
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: "pagar", Desc: "Aluguel", Valor: 150000, Venc: hoje}); err != nil {
		t.Fatal(err)
	}
	conn.Exec(`INSERT INTO cartoes (id, nome, limite, dia_fechamento, dia_vencimento) VALUES (1, 'Inter', 500000, 10, 20)`)
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, cartao_id) VALUES ('pagar', 'Livro', 5000, ?, 1)`,
		time.Now().AddDate(0, 1, 0).Format("2006-01")+"-20")

	p, err = Inicio(conn)
	if err != nil {
		t.Fatal(err)
	}
	if p.Vazio || p.Saldo != 100000 || p.PendPagar != 150000 {
		t.Errorf("painel = %+v", p)
	}
	if len(p.Avisos) != 1 || p.Avisos[0].Descricao != "Aluguel" || p.Avisos[0].Situacao != SituacaoHoje {
		t.Errorf("avisos = %+v", p.Avisos)
	}
	if len(p.Faturas) != 1 || p.Faturas[0].Nome != "Inter" || p.Faturas[0].Aberto != 5000 || p.Faturas[0].Limite != 500000 {
		t.Errorf("faturas = %+v", p.Faturas)
	}
	saida = capturaSaida(t, func() error { return InicioTexto(conn, nil) })
	for _, want := range []string{"Saldo total", "Aluguel", "vence hoje", "Inter"} {
		if !strings.Contains(saida, want) {
			t.Errorf("InicioTexto devia mostrar %q:\n%s", want, saida)
		}
	}
}
