package app

import (
	"testing"
	"time"
)

// Com pouco histórico (só o mês corrente), a média mensal deve refletir o total
// do mês, não ser dividida por 3.
func TestMediasHistoricasPoucoHistorico(t *testing.T) {
	conn := abreDB(t)
	hoje := time.Now().Format("2006-01-02")
	// uma única despesa quitada hoje, de R$ 900
	if _, err := conn.Exec(
		`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, status, quitado_em)
		 VALUES ('pagar','Mercado',90000,?,'quitado',?)`, hoje, hoje); err != nil {
		t.Fatal(err)
	}
	_, desp, err := mediasHistoricas(conn)
	if err != nil {
		t.Fatal(err)
	}
	if desp != 90000 {
		t.Fatalf("média com 1 mês de histórico = %d, queria 90000 (não dividir por 3)", desp)
	}
}

// Com 3 meses de histórico, divide por 3.
func TestMediasHistoricasTresMeses(t *testing.T) {
	conn := abreDB(t)
	now := time.Now()
	for i := 0; i < 3; i++ {
		d := now.AddDate(0, -i, 0).Format("2006-01-02")
		if _, err := conn.Exec(
			`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, status, quitado_em)
			 VALUES ('receber','Salario',300000,?,'quitado',?)`, d, d); err != nil {
			t.Fatal(err)
		}
	}
	rec, _, err := mediasHistoricas(conn)
	if err != nil {
		t.Fatal(err)
	}
	// 3 × R$3.000 espalhados por 3 meses ⇒ média R$3.000
	if rec != 300000 {
		t.Fatalf("média com 3 meses = %d, queria 300000", rec)
	}
}
