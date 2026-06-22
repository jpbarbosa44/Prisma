package app

import "testing"

// TestCriarLancamentosParceladoAtomico garante que um parcelado entra inteiro e
// coerente: N linhas, todas ligadas à mesma raiz por parcela_grupo, e a soma das
// parcelas igual ao total (a última absorve o resto). Protege a transação de
// CriarLancamentos contra regressões que deixem parcelas órfãs ou sem vínculo.
func TestCriarLancamentosParceladoAtomico(t *testing.T) {
	conn := abreDB(t)
	criados, _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Notebook", Valor: 120000, Venc: "2026-06-10", Parcelas: 12,
	})
	if err != nil {
		t.Fatalf("criar parcelado: %v", err)
	}
	if len(criados) != 12 {
		t.Fatalf("esperava 12 parcelas, veio %d", len(criados))
	}

	var ligadas int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM lancamentos WHERE parcela_grupo = ?`, criados[0].ID).Scan(&ligadas); err != nil {
		t.Fatal(err)
	}
	if ligadas != 12 {
		t.Fatalf("parcela_grupo ligou %d parcelas de 12", ligadas)
	}

	var grupos int
	if err := conn.QueryRow(
		`SELECT COUNT(DISTINCT parcela_grupo) FROM lancamentos WHERE parcela_grupo IS NOT NULL`).Scan(&grupos); err != nil {
		t.Fatal(err)
	}
	if grupos != 1 {
		t.Fatalf("esperava 1 grupo de parcelas, veio %d", grupos)
	}

	var soma int64
	if err := conn.QueryRow(
		`SELECT COALESCE(SUM(valor),0) FROM lancamentos WHERE parcela_grupo = ?`, criados[0].ID).Scan(&soma); err != nil {
		t.Fatal(err)
	}
	if soma != 120000 {
		t.Fatalf("soma das parcelas = %d, queria 120000 (centavos)", soma)
	}
}

// TestGerarRecorrenciasIdempotente garante que materializar de novo não cria
// nada: o número de lançamentos da regra não muda entre execuções. Como cada
// regra grava os inserts e o ultima_ref na mesma transação, rodar GerarRecorrencias
// repetidamente é um no-op — a invariante que a correção de duplicação preserva.
func TestGerarRecorrenciasIdempotente(t *testing.T) {
	conn := abreDB(t)
	// recorrenciaAdd já chama GerarRecorrencias uma vez ao criar a regra
	silencia(t, func() error {
		return Recorrencia(conn, []string{
			"add", "--tipo", "receber", "--desc", "Salário",
			"--valor", "5000", "--dia", "5", "--passados", "manter",
		})
	})

	conta := func() (int, int) {
		t.Helper()
		var doRec, total int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id = 1`).Scan(&doRec); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&total); err != nil {
			t.Fatal(err)
		}
		return doRec, total
	}

	rec1, tot1 := conta()
	if rec1 == 0 {
		t.Fatalf("esperava lançamentos materializados na criação, veio 0")
	}

	// rodar mais duas vezes não pode duplicar nada
	for i := 0; i < 2; i++ {
		if _, err := GerarRecorrencias(conn); err != nil {
			t.Fatalf("GerarRecorrencias (rodada %d): %v", i+2, err)
		}
	}
	rec2, tot2 := conta()
	if rec2 != rec1 || tot2 != tot1 {
		t.Fatalf("GerarRecorrencias duplicou: recorrência %d→%d, total %d→%d", rec1, rec2, tot1, tot2)
	}
}
