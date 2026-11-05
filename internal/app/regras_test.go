package app

import (
	"strings"
	"testing"
)

func TestCategoriaPorRegra(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Regra(conn, []string{"add", "uber", "transporte"}) })
	silencia(t, func() error { return Regra(conn, []string{"add", "Uber Eats", "delivery"}) })
	silencia(t, func() error { return Regra(conn, []string{"add", "farmácia", "saude"}) })
	casos := map[string]string{
		"UBER *TRIP 1234":         "transporte",
		"Uber   Eats - pedido":    "delivery", // a mais específica vence
		"FARMACIA SAO JOAO":       "saude",    // sem acento casa com acento
		"Farmácia Popular":        "saude",
		"SUBERBO LTDA":            "", // palavra inteira: "suberbo" não é "uber"
		"Eats Uber":               "transporte",
		"posto ipiranga gasolina": "",
	}
	for desc, quer := range casos {
		if got := CategoriaPorRegra(conn, desc); got != quer {
			t.Errorf("CategoriaPorRegra(%q) = %q, quer %q", desc, got, quer)
		}
	}

	// mesmo padrão (outra caixa/acento) atualiza em vez de duplicar
	silencia(t, func() error { return Regra(conn, []string{"add", "FARMACIA", "farmacia"}) })
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM regras_categoria`).Scan(&n)
	if n != 3 || CategoriaPorRegra(conn, "farmácia") != "farmacia" {
		t.Errorf("regra repetida: %d regras, categoria %q", n, CategoriaPorRegra(conn, "farmácia"))
	}
	silencia(t, func() error { return Regra(conn, []string{"remover", "r1"}) })
	if CategoriaPorRegra(conn, "UBER *TRIP") != "" {
		t.Error("remover r1 devia apagar a regra do uber")
	}
}

// TestRegraAplicada: lançamento sem categoria e importação usam a regra; uma
// categoria explícita sempre vence; editar à mão sugere criar a regra.
func TestRegraAplicada(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Regra(conn, []string{"add", "ifood", "delivery"}) })
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "IFOOD *Pizza", "--valor", "50", "--venc", "2026-06-10"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "iFood mercado", "--valor", "80", "--venc", "2026-06-10", "--cat", "mercado"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Padaria", "--valor", "10", "--venc", "2026-06-10"})
	})
	cats := map[string]string{}
	rows, _ := conn.Query(`SELECT descricao, categoria FROM lancamentos`)
	for rows.Next() {
		var d, c string
		rows.Scan(&d, &c)
		cats[d] = c
	}
	rows.Close()
	if cats["IFOOD *Pizza"] != "delivery" || cats["iFood mercado"] != "mercado" || cats["Padaria"] != "geral" {
		t.Fatalf("categorias = %v", cats)
	}

	conn.Exec(`INSERT INTO contas (id, nome) VALUES (1, 'Banco')`)
	p, err := PreverExtrato(conn, "x.csv", []byte("01/06/2026;IFOOD *SUSHI;-40,00\n02/06/2026;TED RECEBIDA;100,00\n"), DestinoExtrato{ContaID: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Itens[0].Categoria != "delivery" || p.Itens[1].Categoria != "importado" {
		t.Errorf("prévia: %+v", p.Itens)
	}

	var id string
	conn.QueryRow(`SELECT id FROM lancamentos WHERE descricao = 'Padaria'`).Scan(&id)
	saida := capturaSaida(t, func() error { return lancamentoEditar(conn, []string{id, "--cat", "padaria"}) })
	if !strings.Contains(saida, "prisma regra add padaria padaria") {
		t.Errorf("editar a categoria devia sugerir a regra:\n%s", saida)
	}
}
