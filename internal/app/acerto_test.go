package app

import (
	"testing"
)

func TestAcerto(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO contas (id, nome) VALUES (1, 'Nu')`)
	if err := Grupo(conn, []string{"add", "--nome", "Casa", "--pessoas", "Eu, Maria"}); err != nil {
		t.Fatal(err)
	}
	if err := Grupo(conn, []string{"add", "--nome", "Viagem", "--pessoas", "Ana, Eu, Bia"}); err != nil {
		t.Fatal(err)
	}
	cria := func(p LancamentoParams) {
		t.Helper()
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	// casa: eu paguei 300 de mercado (Maria me deve 150); Maria pagou 100 de luz (devo 50)
	cria(LancamentoParams{Tipo: "pagar", Desc: "Mercado", Valor: 30000, Venc: "2026-11-01", GrupoID: 1, RecebePagamento: true, Quitado: true})
	cria(LancamentoParams{Tipo: "pagar", Desc: "Luz", Valor: 10000, Venc: "2026-11-02", GrupoID: 1, PagoPor: "maria"})
	// viagem: eu paguei 900 de hotel (Ana e Bia me devem 300 cada)
	cria(LancamentoParams{Tipo: "pagar", Desc: "Hotel", Valor: 90000, Venc: "2026-11-03", GrupoID: 2, RecebePagamento: true, Quitado: true})

	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: "pagar", Desc: "x", Valor: 100, Venc: "2026-11-02", GrupoID: 1, PagoPor: "Eu"}); err == nil {
		t.Fatal("você não paga para você mesmo")
	}
	if _, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: "pagar", Desc: "x", Valor: 100, Venc: "2026-11-02", PagoPor: "Maria"}); err == nil {
		t.Fatal("pago-por sem grupo")
	}

	ss, err := Acerto(conn, 0)
	if err != nil {
		t.Fatal(err)
	}
	saldo := map[string]SaldoPessoa{}
	for _, s := range ss {
		saldo[s.Pessoa] = s
	}
	if m := saldo["Maria"]; m.TeDeve != 15000 || m.VoceDeve != 5000 || m.Liquido != 10000 {
		t.Fatalf("Maria: %+v", m)
	}
	if a, b := saldo["Ana"], saldo["Bia"]; a.TeDeve != 30000 || b.TeDeve != 30000 {
		t.Fatalf("Ana/Bia: %+v %+v", a, b)
	}
	if _, tem := saldo["Eu"]; tem {
		t.Fatal("você não aparece como devedor")
	}

	// acerta com a Maria: quita o reembolso e a luz
	rec, pag, err := Acertar(conn, 1, "MARIA", 1, "2026-11-10")
	if err != nil || rec != 15000 || pag != 5000 {
		t.Fatalf("acertar Maria: %d %d %v", rec, pag, err)
	}
	if _, _, err := Acertar(conn, 1, "Maria", 1, "2026-11-10"); err == nil {
		t.Fatal("não há mais nada com a Maria")
	}
	// grupo de 3: a Ana acerta só a parte dela
	if rec, _, err := Acertar(conn, 2, "ana", 1, "2026-11-11"); err != nil || rec != 30000 {
		t.Fatalf("acertar Ana: %d %v", rec, err)
	}
	ss, _ = Acerto(conn, 2)
	if len(ss) != 1 || ss[0].Pessoa != "Bia" || ss[0].TeDeve != 30000 {
		t.Fatalf("depois da Ana: %+v", ss)
	}
	if rec, _, err := Acertar(conn, 2, "Bia", 0, "2026-11-12"); err != nil || rec != 30000 {
		t.Fatalf("acertar Bia: %d %v", rec, err)
	}
	if ss, _ := Acerto(conn, 0); len(ss) != 0 {
		t.Fatalf("todos quites: %+v", ss)
	}
	// o dinheiro: entrou 150 + 300 e saiu 50 na conta 1 (a Bia não informou conta)
	var naConta int64
	conn.QueryRow(`SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN valor ELSE -valor END), 0) FROM lancamentos WHERE conta_id = 1 AND status = 'quitado'`).Scan(&naConta)
	if naConta != 15000+30000-10000 { // a luz quitada pesa o valor cheio aqui (valEf divide no saldo)
		t.Fatalf("na conta: %d", naConta)
	}
	if s, _ := saldoConta(conn, 1); s != 15000+30000-5000 {
		t.Fatalf("saldo da conta (com a sua parte da luz): %d", s)
	}
}
