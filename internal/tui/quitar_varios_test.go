package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"prisma/internal/app"
	"prisma/internal/db"
)

func TestLinhaQuitada(t *testing.T) {
	casos := map[string]bool{
		"12  pagar  Luz   contas  10/11/2026  R$ 150,00  -  -  -  quitado em 05/11/2026": true,
		"13  pagar  Água  contas  10/11/2026  R$ 80,00   -  -  -  pendente":              false,
		"14  pagar  Net   contas  01/11/2026  R$ 99,00   -  -  -  ⚠ atrasada":            false,
		"Valor total a pagar: R$ 329,00 | Valor total a receber: R$ 0,00":                false,
	}
	for l, quer := range casos {
		if linhaQuitada(l) != quer {
			t.Errorf("linhaQuitada(%q) = %v, quer %v", l, !quer, quer)
		}
	}
}

// TestTUIMarcaEQuitaVarios: espaço marca linhas na tela Pagar/Receber, o
// formulário de quitar abre com os ids marcados e a ação quita todos.
func TestTUIMarcaEQuitaVarios(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	venc := time.Now().Format("2006-01") + "-28"
	var ids []string
	for _, d := range []string{"Luz", "Água", "Net"} {
		criados, _, _, err := app.CriarLancamentos(conn, app.LancamentoParams{Tipo: "pagar", Desc: d, Valor: 1000, Venc: venc})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fmt.Sprint(criados[0].ID))
	}

	telas := novasTelas(conn, false)
	m := model{conn: conn, telas: telas, params: make([][]string, len(telas)), abaSel: make([]int, len(telas)), selPos: -1}
	tela := -1
	for i, tl := range telas {
		if tl.titulo == "Pagar/Receber" {
			tela = i
		}
	}
	if tela < 0 {
		t.Fatal("tela Pagar/Receber não encontrada")
	}
	m.abreTela(tela)
	if len(m.seleta) != 3 {
		t.Fatalf("esperava 3 linhas selecionáveis, veio %d", len(m.seleta))
	}

	// marca a 1ª e a 3ª (espaço desce sozinho; ↓ pula a 2ª)
	tecla := func(k tea.KeyMsg) {
		mm, _ := m.teclaTela(k)
		m = mm.(model)
	}
	tecla(tea.KeyMsg{Type: tea.KeySpace})
	tecla(tea.KeyMsg{Type: tea.KeyDown})
	tecla(tea.KeyMsg{Type: tea.KeySpace})
	if got := m.idsMarcados(); strings.Join(got, " ") != ids[0]+" "+ids[2] {
		t.Fatalf("marcados = %v, quer [%s %s]", got, ids[0], ids[2])
	}

	tecla(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if m.modo != modoForm {
		t.Fatal("u devia abrir o formulário de quitar")
	}
	if v := m.inputs[0].Value(); v != ids[0]+" "+ids[2] {
		t.Fatalf("campo id = %q, quer %q", v, ids[0]+" "+ids[2])
	}
	m.executaAcao(m.formAcao, []string{m.inputs[0].Value(), ""})
	if m.msgErr {
		t.Fatalf("quitar deu erro: %s", m.msg)
	}

	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE status = 'quitado'`).Scan(&n)
	if n != 2 {
		t.Fatalf("quitados = %d, quer 2", n)
	}
	if len(m.marcados) != 0 {
		t.Error("a recarga depois da ação devia limpar as marcações")
	}
	riscadas := 0
	for _, s := range m.seleta {
		if linhaQuitada(m.linhas[s.linha]) {
			riscadas++
		}
	}
	if riscadas != 2 {
		t.Errorf("linhas quitadas (riscadas) = %d, quer 2", riscadas)
	}

	// linha já quitada (a 1ª) não se marca: o espaço só desce
	tecla(tea.KeyMsg{Type: tea.KeySpace})
	if len(m.marcados) != 0 || m.selPos != 1 {
		t.Errorf("espaço numa quitada: marcados=%v selPos=%d, quer nenhum e 1", m.marcados, m.selPos)
	}
}

// TestWebQuitarVarios: a API marca a ação quitar como multi e aceita vários ids.
func TestWebQuitarVarios(t *testing.T) {
	srv := servidorTeste(t)
	conn, err := db.Open() // mesmo PRISMA_DB do servidorTeste
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var ids []string
	for _, d := range []string{"Luz", "Água"} {
		criados, _, _, err := app.CriarLancamentos(conn, app.LancamentoParams{Tipo: "pagar", Desc: d, Valor: 1000, Venc: "2026-06-10"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fmt.Sprint(criados[0].ID))
	}

	var telas []struct {
		Titulo string `json:"titulo"`
		Acoes  []struct {
			Tecla string `json:"tecla"`
			Multi bool   `json:"multi"`
		} `json:"acoes"`
	}
	pega(t, srv.URL+"/api/telas", &telas)
	tela, acao := -1, -1
	for i, tl := range telas {
		for j, a := range tl.Acoes {
			if tl.Titulo == "Pagar/Receber" && a.Tecla == "u" {
				tela, acao = i, j
				if !a.Multi {
					t.Error("a ação quitar devia vir com multi: true")
				}
			} else if a.Multi {
				t.Errorf("só quitar é multi; veio %s › %s", tl.Titulo, a.Tecla)
			}
		}
	}
	if tela < 0 {
		t.Fatal("ação quitar não encontrada")
	}

	b, _ := json.Marshal(map[string]any{"tela": tela, "acao": acao, "vals": []string{ids[0] + " " + ids[1], ""}})
	resp, err := http.Post(srv.URL+"/api/executar", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("executar: status %d", resp.StatusCode)
	}
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE status = 'quitado'`).Scan(&n)
	if n != 2 {
		t.Fatalf("quitados = %d, quer 2", n)
	}
}
