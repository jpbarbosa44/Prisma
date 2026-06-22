package app

import "testing"

// TestSimulaPlanoComJurosQuita cobre o caso que faltava (os de juros zero e de
// dívida divergente já estão em app_test.go): com juros, mas aporte suficiente,
// a dívida quita — e o total pago supera o principal, sendo a diferença os juros.
func TestSimulaPlanoComJurosQuita(t *testing.T) {
	plano := simulaPlano(100000, 2, 20000) // R$ 1.000,00 a 2% a.m., aporte R$ 200,00
	if len(plano) == 0 {
		t.Fatal("plano vazio")
	}
	if fim := plano[len(plano)-1].saldoFinal; fim != 0 {
		t.Fatalf("esperava quitar, saldo final %d", fim)
	}
	var totPago int64
	for _, p := range plano {
		totPago += p.pago
	}
	if totPago <= 100000 {
		t.Fatalf("com juros o total pago (%d) deveria exceder o principal (100000)", totPago)
	}
}
