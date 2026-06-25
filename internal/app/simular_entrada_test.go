package app

import (
	"strings"
	"testing"
)

// A entrada paga à vista pode estourar o saldo no ato, antes de qualquer
// parcela. Esse mergulho inicial não aparece em nenhuma linha da tabela mês a
// mês, então precisa ser classificado como inviável (🔴), não 🟢/⚠.
func TestSimularEntradaMaiorQueSaldoEhVermelho(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('cc', 10000)`) // saldo R$100
	// renda recorrente que mantém os meses no positivo, mas não entra no saldo atual
	if err := recorrenciaAdd(conn, []string{"--tipo", "receber", "--desc", "Salario", "--valor", "130", "--dia", "1", "--passados", "manter"}); err != nil {
		t.Fatal(err)
	}
	out := capturaSaida(t, func() error {
		// entrada de R$200 > saldo de R$100
		return Simular(conn, []string{"--desc", "TV", "--valor", "300", "--entrada", "200", "--parcelas", "5"})
	})
	if !strings.Contains(out, "🔴") {
		t.Fatalf("esperava veredito 🔴 (entrada estoura o saldo), veio:\n%s", out)
	}
	if strings.Contains(out, "🟢") || strings.Contains(out, "abaixo de -") {
		t.Fatalf("não deveria classificar como viável com piso negativo:\n%s", out)
	}
}

// Sanidade: entrada dentro do saldo e meses sempre positivos continua 🟢.
func TestSimularEntradaDentroDoSaldoEhVerde(t *testing.T) {
	conn := abreDB(t)
	conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('cc', 500000)`) // saldo R$5.000
	out := capturaSaida(t, func() error {
		return Simular(conn, []string{"--desc", "TV", "--valor", "300", "--entrada", "100", "--parcelas", "5"})
	})
	if !strings.Contains(out, "🟢") {
		t.Fatalf("esperava 🟢 (folga grande), veio:\n%s", out)
	}
}
