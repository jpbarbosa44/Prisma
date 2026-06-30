package tui

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"prisma/internal/app"
	"prisma/internal/db"
)

// achaAcao devolve a ação de tecla `tecla` na tela de título `titulo`.
func achaAcao(t *testing.T, telas []tela, titulo, tecla string) *acao {
	t.Helper()
	for _, tl := range telas {
		if tl.titulo != titulo {
			continue
		}
		for i := range tl.acoes {
			if tl.acoes[i].tecla == tecla {
				return &tl.acoes[i]
			}
		}
	}
	t.Fatalf("ação %q não encontrada na tela %q", tecla, titulo)
	return nil
}

// resolve aplica a variante da ação para os params dados, como o modelo faz ao
// disparar a tecla.
func resolve(a *acao, params []string) *acao {
	if a.variante != nil {
		if v := a.variante(params); v != nil {
			return v
		}
	}
	return a
}

// cartaoComGasto cria um banco com um cartão (#1) e um gasto nele, devolvendo a
// conexão e o id do gasto.
func cartaoComGasto(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := captura(func() error {
		return app.Cartao(conn, []string{"add", "--nome", "Nubank", "--fechamento", "20", "--vencimento", "27", "--conta", "1"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := captura(func() error {
		return app.NovoLancamento(conn, "pagar", []string{"add", "--desc", "Mercado", "--valor", "350", "--cartao", "1"})
	}); err != nil {
		t.Fatal(err)
	}
	var gastoID int64
	if err := conn.QueryRow(`SELECT id FROM lancamentos WHERE cartao_id = 1`).Scan(&gastoID); err != nil {
		t.Fatal(err)
	}
	return conn, gastoID
}

// TestCartaoXRemoveGastoNaFatura garante o fix: vendo uma fatura, "x" remove o
// GASTO selecionado (um lançamento), não o cartão.
func TestCartaoXRemoveGastoNaFatura(t *testing.T) {
	conn, gastoID := cartaoComGasto(t)
	telas := novasTelas(conn, false)
	x := achaAcao(t, telas, "Cartões", "x")

	// na visão da fatura, x remove o gasto selecionado (id do lançamento)
	naFatura := resolve(x, []string{"fatura", "1", ""})
	if _, err := naFatura.executar([]string{fmt.Sprint(gastoID)}); err != nil {
		t.Fatalf("remover gasto na fatura falhou: %v", err)
	}
	if existeReg(conn, "lancamentos", gastoID) {
		t.Error("o gasto deveria ter sido removido da fatura")
	}
	if !existeReg(conn, "cartoes", 1) {
		t.Error("o cartão NÃO deveria ter sido removido ao apagar um gasto da fatura")
	}

	// na lista (sem params de fatura), x remove o cartão
	naLista := resolve(x, nil)
	if _, err := naLista.executar([]string{"1"}); err != nil {
		t.Fatalf("remover cartão na lista falhou: %v", err)
	}
	if existeReg(conn, "cartoes", 1) {
		t.Error("o cartão deveria ter sido removido na lista")
	}
}

// TestCartaoEEditaGastoNaFatura garante o fix do "erro ao carregar": vendo uma
// fatura, "e" carrega e edita o GASTO selecionado, não tenta buscar um cartão
// pelo id do gasto.
func TestCartaoEEditaGastoNaFatura(t *testing.T) {
	conn, gastoID := cartaoComGasto(t)
	telas := novasTelas(conn, false)
	e := achaAcao(t, telas, "Cartões", "e")

	naFatura := resolve(e, []string{"fatura", "1", ""})
	// carregar deve achar o lançamento (antes dava "sql: no rows" por buscar cartão)
	vals, err := naFatura.carregar(fmt.Sprint(gastoID))
	if err != nil {
		t.Fatalf("carregar o gasto na fatura falhou: %v", err)
	}
	if len(vals) == 0 || vals[0] != "Mercado" {
		t.Fatalf("carregar trouxe %v, quer a descrição do gasto (Mercado) primeiro", vals)
	}
	// editar o valor do gasto
	novo := []string{fmt.Sprint(gastoID), "", "500,00", "", "", "", "", "", "", "", ""}
	if _, err := naFatura.executar(novo); err != nil {
		t.Fatalf("editar o gasto falhou: %v", err)
	}
	var valor int64
	if err := conn.QueryRow(`SELECT valor FROM lancamentos WHERE id = ?`, gastoID).Scan(&valor); err != nil {
		t.Fatal(err)
	}
	if valor != 50000 {
		t.Errorf("valor após editar = %d, quer 50000", valor)
	}

	// na lista, "e" continua editando o cartão (carrega pelo id do cartão)
	naLista := resolve(e, nil)
	if _, err := naLista.carregar("1"); err != nil {
		t.Fatalf("carregar o cartão na lista falhou: %v", err)
	}
}

func existeReg(conn *sql.DB, tabela string, id int64) bool {
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM `+tabela+` WHERE id = ?`, id).Scan(&n)
	return n > 0
}

// TestCartaoPPagaFaturaAtual garante que, vendo uma fatura, "p" paga ESTA fatura
// (cartão da visão), sem pedir o id de novo nem usar o gasto selecionado.
func TestCartaoPPagaFaturaAtual(t *testing.T) {
	conn, gastoID := cartaoComGasto(t)
	telas := novasTelas(conn, false)
	p := achaAcao(t, telas, "Cartões", "p")

	naFatura := resolve(p, []string{"fatura", "1", "", ""})
	if len(naFatura.campos) == 0 || naFatura.campos[0].rotulo == "id" {
		t.Fatalf("na fatura, p não deveria pedir id de cartão; campos=%v", naFatura.campos)
	}
	if _, err := naFatura.executar([]string{"", ""}); err != nil {
		t.Fatalf("pagar a fatura atual falhou: %v", err)
	}
	var status string
	if err := conn.QueryRow(`SELECT status FROM lancamentos WHERE id = ?`, gastoID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "quitado" {
		t.Errorf("gasto status = %q após pagar a fatura, quer quitado", status)
	}
}

// TestCartaoTTrocaRefDoMesmoCartao garante que, vendo uma fatura, "t" troca a
// fatura do MESMO cartão (id vem da visão), sem pedir o cartão de novo.
func TestCartaoTTrocaRefDoMesmoCartao(t *testing.T) {
	conn, _ := cartaoComGasto(t)
	telas := novasTelas(conn, false)
	tt := achaAcao(t, telas, "Cartões", "t")

	naFatura := resolve(tt, []string{"fatura", "1", "2026-06", ""})
	if len(naFatura.campos) == 0 || naFatura.campos[0].rotulo != "fatura" {
		t.Fatalf("na fatura, t deveria pedir só a ref; campos=%v", naFatura.campos)
	}
	got := naFatura.params([]string{"2026-07", "não"})
	want := []string{"fatura", "1", "2026-07", ""}
	if len(got) != len(want) {
		t.Fatalf("params = %v, quer %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("params = %v, quer %v", got, want)
		}
	}
}
