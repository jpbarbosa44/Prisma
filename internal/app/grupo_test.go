package app

import (
	"database/sql"
	"strconv"
	"strings"
	"testing"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// criaGrupoEDespesa cria um grupo de n pessoas e uma despesa quitada de `valor`
// centavos vinculada a ele, devolvendo o id do grupo.
func criaGrupoEDespesa(t *testing.T, conn *sql.DB, pessoas string, valor int64) int64 {
	t.Helper()
	silencia(t, func() error {
		return Grupo(conn, []string{"add", "--nome", "Casal", "--pessoas", pessoas})
	})
	var gid int64
	if err := conn.QueryRow(`SELECT id FROM grupos ORDER BY id DESC LIMIT 1`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: "pagar", Desc: "Mercado", Valor: valor, Cat: "mercado",
		Venc: "2026-06-10", GrupoID: gid, Quitado: true,
	}); err != nil {
		t.Fatalf("criando despesa do grupo: %v", err)
	}
	return gid
}

func TestGrupoAddExigeDuasPessoas(t *testing.T) {
	conn := abreDB(t)
	if err := Grupo(conn, []string{"add", "--nome", "Sozinho", "--pessoas", "Eu"}); err == nil {
		t.Error("grupo com 1 pessoa deveria falhar")
	}
	if err := Grupo(conn, []string{"add", "--pessoas", "Eu, Maria"}); err == nil {
		t.Error("grupo sem --nome deveria falhar")
	}
	silencia(t, func() error {
		return Grupo(conn, []string{"add", "--nome", "Casa", "--pessoas", "Eu, Maria, João"})
	})
	var nome string
	var qtd int
	if err := conn.QueryRow(`
		SELECT g.nome, COUNT(gp.id) FROM grupos g
		JOIN grupo_pessoas gp ON gp.grupo_id = g.id GROUP BY g.id`).Scan(&nome, &qtd); err != nil {
		t.Fatal(err)
	}
	if nome != "Casa" || qtd != 3 {
		t.Errorf("grupo criado: nome=%q qtd=%d, quer Casa e 3", nome, qtd)
	}
}

// TestDespesaDeGrupoDivideValorNoSaldo é o requisito central: uma despesa de R$
// 100 num grupo de 2 pessoas deve pesar só R$ 50 no saldo total.
func TestDespesaDeGrupoDivideValorNoSaldo(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 100000)`); err != nil {
		t.Fatal(err)
	}
	criaGrupoEDespesa(t, conn, "Eu, Maria", 10000) // R$ 100 entre 2

	total, err := saldoTotal(conn)
	if err != nil {
		t.Fatal(err)
	}
	// 100000 - (10000 / 2) = 95000
	if total != 95000 {
		t.Errorf("saldo total = %d, quer 95000 (só a metade da despesa do grupo)", total)
	}
}

func TestDespesaDeGrupoDivideNoRelatorioEPlano(t *testing.T) {
	conn := abreDB(t)
	criaGrupoEDespesa(t, conn, "Eu, Ana, Bia", 30000) // R$ 300 entre 3 = R$ 100 a minha parte

	// relatório: a despesa por categoria deve mostrar a parte efetiva
	saida := capturaSaida(t, func() error { return Relatorio(conn, []string{"--meses", "1"}) })
	if !strings.Contains(saida, "100,00") {
		t.Errorf("relatório deveria mostrar a parte de 100,00, veio:\n%s", saida)
	}
	if strings.Contains(saida, "300,00") {
		t.Errorf("relatório não deveria mostrar o valor cheio 300,00:\n%s", saida)
	}

	// planejamento: o gasto consumido do limite também é a parte efetiva
	silencia(t, func() error {
		return Plano(conn, []string{"add", "--cat", "mercado", "--valor", "800", "--periodo", "mes", "--ref", "2026-06"})
	})
	usos, err := PlanosDaCategoria(conn, "mercado", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(usos) != 1 || usos[0].Gasto != 10000 {
		t.Errorf("plano: gasto = %+v, quer 10000 (a parte efetiva)", usos)
	}
}

func TestDesvincularGrupoVoltaValorCheio(t *testing.T) {
	conn := abreDB(t)
	criaGrupoEDespesa(t, conn, "Eu, Maria", 10000)
	var lid int64
	if err := conn.QueryRow(`SELECT id FROM lancamentos ORDER BY id DESC LIMIT 1`).Scan(&lid); err != nil {
		t.Fatal(err)
	}
	// antes: parte efetiva 5000
	total, _ := saldoTotal(conn)
	if total != -5000 {
		t.Fatalf("antes de desvincular, saldo = %d, quer -5000", total)
	}
	silencia(t, func() error {
		return Lancamentos(conn, []string{"editar", itoa(lid), "--grupo", "0"})
	})
	total, _ = saldoTotal(conn)
	if total != -10000 {
		t.Errorf("após desvincular, saldo = %d, quer -10000 (valor cheio)", total)
	}
}

func TestRemoverGrupoPreservaLancamento(t *testing.T) {
	conn := abreDB(t)
	gid := criaGrupoEDespesa(t, conn, "Eu, Maria", 10000)
	silencia(t, func() error { return Grupo(conn, []string{"remover", itoa(gid)}) })

	// o lançamento continua, agora sem grupo, contando pelo valor cheio
	var n int
	var grupoID sql.NullInt64
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("após remover grupo, lançamentos = %d, quer 1", n)
	}
	if err := conn.QueryRow(`SELECT grupo_id FROM lancamentos LIMIT 1`).Scan(&grupoID); err != nil {
		t.Fatal(err)
	}
	if grupoID.Valid {
		t.Errorf("lançamento ainda aponta para grupo %d, deveria estar nulo", grupoID.Int64)
	}
	// pessoas do grupo somem junto (ON DELETE CASCADE)
	if err := conn.QueryRow(`SELECT COUNT(*) FROM grupo_pessoas`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("pessoas órfãs após remover grupo: %d", n)
	}
}
