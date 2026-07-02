package app

import (
	"database/sql"
	"fmt"
	"testing"
)

func valorDe(t *testing.T, conn *sql.DB, id int64) int64 {
	t.Helper()
	var v int64
	if err := conn.QueryRow(`SELECT valor FROM lancamentos WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("lendo valor de #%d: %v", id, err)
	}
	return v
}

func criaGrupo(t *testing.T, conn *sql.DB, id int, pessoas ...string) {
	t.Helper()
	if _, err := conn.Exec(`INSERT INTO grupos (id, nome) VALUES (?, ?)`, id, fmt.Sprintf("g%d", id)); err != nil {
		t.Fatal(err)
	}
	for _, p := range pessoas {
		if _, err := conn.Exec(`INSERT INTO grupo_pessoas (grupo_id, nome) VALUES (?, ?)`, id, p); err != nil {
			t.Fatal(err)
		}
	}
}

// Bug Alto: editar o valor de uma despesa recebe_pagamento deve re-dividir e
// manter o reembolso vinculado em sincronia.
func TestEditarValorRecebePagamentoRedistribui(t *testing.T) {
	conn := abreDB(t)
	criaGrupo(t, conn, 1, "eu", "voce")
	criados, reemb, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Internet", Valor: 10000, Venc: "2026-06-10",
		GrupoID: 1, RecebePagamento: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := valorDe(t, conn, criados[0].ID); got != 5000 {
		t.Fatalf("minha parte inicial = %d, queria 5000", got)
	}

	if err := lancamentoEditar(conn, []string{fmt.Sprint(criados[0].ID), "--valor", "200,00"}); err != nil {
		t.Fatal(err)
	}
	if got := valorDe(t, conn, criados[0].ID); got != 10000 {
		t.Fatalf("minha parte apos editar = %d, queria 10000 (20000/2)", got)
	}
	if got := valorDe(t, conn, reemb[0].ID); got != 10000 {
		t.Fatalf("reembolso apos editar = %d, queria 10000 (defasado = bug)", got)
	}
}

// Mudar o grupo de uma despesa recebe_pagamento re-divide pelo novo nº de pessoas,
// preservando o total da conta.
func TestEditarGrupoRecebePagamentoRedistribui(t *testing.T) {
	conn := abreDB(t)
	criaGrupo(t, conn, 1, "eu", "voce")        // 2 pessoas
	criaGrupo(t, conn, 2, "eu", "voce", "ele") // 3 pessoas
	criados, reemb, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Aluguel", Valor: 12000, Venc: "2026-06-10",
		GrupoID: 1, RecebePagamento: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := lancamentoEditar(conn, []string{fmt.Sprint(criados[0].ID), "--grupo", "2"}); err != nil {
		t.Fatal(err)
	}
	if got := valorDe(t, conn, criados[0].ID); got != 4000 {
		t.Fatalf("minha parte apos trocar grupo = %d, queria 4000 (12000/3)", got)
	}
	if got := valorDe(t, conn, reemb[0].ID); got != 8000 {
		t.Fatalf("reembolso apos trocar grupo = %d, queria 8000", got)
	}
}

// Mudar só o vencimento de uma despesa recebe_pagamento move o reembolso
// vinculado junto (ele vence com a despesa), preservando os valores.
func TestEditarVencRecebePagamentoMoveReembolso(t *testing.T) {
	conn := abreDB(t)
	criaGrupo(t, conn, 1, "eu", "voce")
	criados, reemb, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Internet", Valor: 10000, Venc: "2026-06-10",
		GrupoID: 1, RecebePagamento: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := lancamentoEditar(conn, []string{fmt.Sprint(criados[0].ID), "--venc", "2026-07-15"}); err != nil {
		t.Fatal(err)
	}
	var venc string
	if err := conn.QueryRow(`SELECT vencimento FROM lancamentos WHERE id = ?`, reemb[0].ID).Scan(&venc); err != nil {
		t.Fatal(err)
	}
	if venc != "2026-07-15" {
		t.Fatalf("vencimento do reembolso = %s, queria 2026-07-15 (defasado = bug)", venc)
	}
	if got := valorDe(t, conn, criados[0].ID); got != 5000 {
		t.Fatalf("minha parte mudou ao editar só o venc: %d, queria 5000", got)
	}
	if got := valorDe(t, conn, reemb[0].ID); got != 5000 {
		t.Fatalf("reembolso mudou ao editar só o venc: %d, queria 5000", got)
	}
}

// Auto-quitar não combina com cartão: o item quitaria sozinho no vencimento da
// fatura sem a fatura ter sido paga. A regra vale ao criar, ao editar e nas
// recorrências.
func TestAutoQuitarNaoValeNoCartao(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO cartoes (id,nome,dia_fechamento,dia_vencimento) VALUES (1,'A',20,28)`)

	_, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Fone", Valor: 10000, Venc: "2026-06-10",
		CartaoID: 1, AutoQuit: true,
	})
	if err == nil {
		t.Fatal("criar com cartão + auto-quitar: esperava erro")
	}

	criados, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Fone", Valor: 10000, Venc: "2026-06-10", CartaoID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lancamentoEditar(conn, []string{fmt.Sprint(criados[0].ID), "--auto-quitar", "sim"}); err == nil {
		t.Fatal("editar item de cartão com --auto-quitar sim: esperava erro")
	}

	err = recorrenciaAdd(conn, []string{"--tipo", "pagar", "--desc", "Streaming",
		"--valor", "50", "--dia", "10", "--cartao", "1", "--auto-quitar"})
	if err == nil {
		t.Fatal("recorrência com cartão + auto-quitar: esperava erro")
	}
}

// Mover um lançamento entre cartões deve preservar a data real da compra (e
// recalcular a fatura a partir dela), não usar o vencimento da fatura anterior.
func TestMoverEntreCartoesPreservaDataCompra(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO cartoes (id,nome,dia_fechamento,dia_vencimento) VALUES (1,'A',20,28)`)
	conn.Exec(`INSERT INTO cartoes (id,nome,dia_fechamento,dia_vencimento) VALUES (2,'B',15,25)`)
	criados, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Tênis", Valor: 30000, Venc: "2026-06-10", CartaoID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := criados[0].ID

	if err := lancamentoEditar(conn, []string{fmt.Sprint(id), "--cartao", "2"}); err != nil {
		t.Fatal(err)
	}
	var dc, vc string
	conn.QueryRow(`SELECT data_compra, vencimento FROM lancamentos WHERE id = ?`, id).Scan(&dc, &vc)
	if dc != "2026-06-10" {
		t.Fatalf("data_compra apos mover = %s, queria 2026-06-10", dc)
	}
	// compra 10/06, cartão B fecha dia 15 → fatura de junho, vence 25/06
	if vc != "2026-06-25" {
		t.Fatalf("vencimento apos mover = %s, queria 2026-06-25 (bug daria 2026-07-25)", vc)
	}
}
