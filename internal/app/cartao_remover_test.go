package app

import "testing"

// Remover um cartão não pode apagar as faturas JÁ PAGAS: elas são histórico e
// já entraram no saldo (saldoTotal soma os quitados). Apagá-las inflaria o saldo.
func TestRemoverCartaoPreservaFaturaPagaEsaldo(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('cc', 100000)`) // R$1.000
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Nu", "--fechamento", "20", "--vencimento", "28", "--conta", "1"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Tênis", "--valor", "300", "--venc", "2026-06-10", "--cartao", "1"})
	})
	var ref string
	conn.QueryRow(`SELECT substr(vencimento,1,7) FROM lancamentos WHERE cartao_id = 1`).Scan(&ref)
	silencia(t, func() error { return Fatura(conn, []string{"pagar", "--cartao", "1", "--ref", ref}) })

	saldoAntes, _ := saldoTotal(conn)
	silencia(t, func() error { return Cartao(conn, []string{"remover", "1"}) })
	saldoDepois, _ := saldoTotal(conn)

	if saldoAntes != saldoDepois {
		t.Fatalf("remover cartão alterou o saldo: %d -> %d", saldoAntes, saldoDepois)
	}
	// a fatura paga continua existindo, agora desvinculada do cartão
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE status = 'quitado' AND cartao_id IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("esperava 1 lançamento pago preservado e desvinculado, veio %d", n)
	}
}

// As faturas em aberto, sim, somem junto com o cartão.
func TestRemoverCartaoApagaFaturaEmAberto(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('cc', 100000)`)
	silencia(t, func() error {
		return Cartao(conn, []string{"add", "--nome", "Nu", "--fechamento", "20", "--vencimento", "28", "--conta", "1"})
	})
	silencia(t, func() error {
		return NovoLancamento(conn, "pagar", []string{"add", "--desc", "Curso", "--valor", "500", "--venc", "2026-06-10", "--cartao", "1"})
	})
	silencia(t, func() error { return Cartao(conn, []string{"remover", "1"}) })
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 0 {
		t.Fatalf("a fatura em aberto deveria sumir com o cartão, sobraram %d", n)
	}
}
