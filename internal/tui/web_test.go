package tui

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/db"
)

// servidorTeste sobe a API web sobre um banco temporário.
func servidorTeste(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(novoServidorWeb(webPessoal, conn).rotas())
	t.Cleanup(srv.Close)
	return srv
}

// pega faz GET e decodifica o JSON da resposta em v.
func pega(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decodificando %s: %v", url, err)
	}
}

func TestWebAPI(t *testing.T) {
	srv := servidorTeste(t)

	t.Run("página principal", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /: status %d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type devia ser HTML, veio %q", ct)
		}
	})

	var telas []struct {
		Titulo string `json:"titulo"`
		Acoes  []struct {
			Tecla   string `json:"tecla"`
			TemForm bool   `json:"temForm"`
		} `json:"acoes"`
	}
	pega(t, srv.URL+"/api/telas", &telas)

	t.Run("menu espelha as telas da TUI", func(t *testing.T) {
		if len(telas) == 0 || telas[0].Titulo != "Saldo" {
			t.Fatalf("esperava a tela Saldo em primeiro, veio %+v", telas)
		}
	})

	// localiza a ação "adicionar" da tela Contas para os passos seguintes
	iContas, jAdd := -1, -1
	for i, tl := range telas {
		if tl.Titulo != "Contas" {
			continue
		}
		iContas = i
		for j, a := range tl.Acoes {
			if a.Tecla == "a" {
				jAdd = j
			}
		}
	}
	if iContas < 0 || jAdd < 0 {
		t.Fatalf("não achei a ação de adicionar conta no menu: %+v", telas)
	}

	var form struct {
		Campos []struct {
			Rotulo      string `json:"rotulo"`
			Obrigatorio bool   `json:"obrigatorio"`
		} `json:"campos"`
	}
	pega(t, fmt.Sprintf("%s/api/form?tela=%d&acao=%d", srv.URL, iContas, jAdd), &form)

	t.Run("form traz os campos da ação", func(t *testing.T) {
		if len(form.Campos) == 0 || form.Campos[0].Rotulo != "nome" || !form.Campos[0].Obrigatorio {
			t.Fatalf("esperava o campo obrigatório \"nome\", veio %+v", form.Campos)
		}
	})

	t.Run("executar cria a conta e o conteúdo a mostra", func(t *testing.T) {
		corpo := map[string]any{
			"tela": iContas, "acao": jAdd,
			"vals": []string{"Nubank", "", "corrente", "1.500,00"},
		}
		b, _ := json.Marshal(corpo)
		resp, err := http.Post(srv.URL+"/api/executar", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatalf("POST executar: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST executar: status %d", resp.StatusCode)
		}

		var r struct {
			Texto string `json:"texto"`
		}
		pega(t, fmt.Sprintf("%s/api/conteudo?tela=%d", srv.URL, iContas), &r)
		if !strings.Contains(r.Texto, "Nubank") || !strings.Contains(r.Texto, "1.500,00") {
			t.Errorf("conteúdo de Contas devia listar a conta criada:\n%s", r.Texto)
		}
	})

	t.Run("obrigatório vazio é recusado", func(t *testing.T) {
		corpo := map[string]any{
			"tela": iContas, "acao": jAdd,
			"vals": []string{"", "", "corrente", ""},
		}
		b, _ := json.Marshal(corpo)
		resp, err := http.Post(srv.URL+"/api/executar", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatalf("POST executar: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("campo obrigatório vazio devia dar 400, veio %d", resp.StatusCode)
		}
	})

	t.Run("tela inválida é recusada", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/conteudo?tela=99")
		if err != nil {
			t.Fatalf("GET conteudo: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("tela inexistente devia dar 400, veio %d", resp.StatusCode)
		}
	})
}

// TestWebAbas confere que a API expõe as abas e a marca listaMensal das telas
// (os atalhos ←/→ e t da TUI) e que o parâmetro "aba" escolhe a visão.
func TestWebAbas(t *testing.T) {
	srv := servidorTeste(t)

	var telas []struct {
		Titulo      string   `json:"titulo"`
		Abas        []string `json:"abas"`
		ListaMensal bool     `json:"listaMensal"`
	}
	pega(t, srv.URL+"/api/telas", &telas)

	// localiza Estatísticas (tem abas) e Pagar/Receber (listaMensal)
	iEstat := -1
	achouMensal := false
	for i, tl := range telas {
		switch tl.Titulo {
		case "Estatísticas":
			iEstat = i
			if len(tl.Abas) < 2 {
				t.Errorf("Estatísticas devia expor abas, veio %v", tl.Abas)
			}
		case "Pagar/Receber":
			if !tl.ListaMensal {
				t.Errorf("Pagar/Receber devia ser listaMensal")
			}
			achouMensal = true
		}
	}
	if iEstat < 0 {
		t.Fatalf("não achei a tela Estatísticas: %+v", telas)
	}
	if !achouMensal {
		t.Errorf("não achei a tela listaMensal Pagar/Receber")
	}

	t.Run("aba seleciona a visão", func(t *testing.T) {
		var r0, r1 struct {
			Texto string `json:"texto"`
		}
		pega(t, fmt.Sprintf("%s/api/conteudo?tela=%d&aba=0&p=--meses&p=6", srv.URL, iEstat), &r0)
		pega(t, fmt.Sprintf("%s/api/conteudo?tela=%d&aba=1&p=--meses&p=6", srv.URL, iEstat), &r1)
		if r0.Texto == "" || r1.Texto == "" {
			t.Fatalf("conteúdo de aba veio vazio (aba0=%q aba1=%q)", r0.Texto, r1.Texto)
		}
		if r0.Texto == r1.Texto {
			t.Errorf("abas diferentes deviam render conteúdos diferentes")
		}
	})

	t.Run("aba inválida cai na primeira", func(t *testing.T) {
		var r0, rX struct {
			Texto string `json:"texto"`
		}
		pega(t, fmt.Sprintf("%s/api/conteudo?tela=%d&aba=0&p=--meses&p=6", srv.URL, iEstat), &r0)
		pega(t, fmt.Sprintf("%s/api/conteudo?tela=%d&aba=99&p=--meses&p=6", srv.URL, iEstat), &rX)
		if r0.Texto != rX.Texto {
			t.Errorf("aba fora do intervalo devia cair na primeira visão")
		}
	})
}

// TestWebAnalytics confere que o servidor do Prisma Analytics sobe com o selo
// (modoAnalytics) e serve as telas exclusivas de análise.
func TestWebAnalytics(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(novoServidorWeb(webAnalytics, conn).rotas())
	t.Cleanup(srv.Close)

	t.Run("versão marca modoAnalytics", func(t *testing.T) {
		var v struct {
			ModoAnalytics bool `json:"modoAnalytics"`
			ModoEmpresa   bool `json:"modoEmpresa"`
		}
		pega(t, srv.URL+"/api/versao", &v)
		if !v.ModoAnalytics || v.ModoEmpresa {
			t.Errorf("esperava modoAnalytics=true e modoEmpresa=false, veio %+v", v)
		}
	})

	t.Run("menu traz as telas de análise", func(t *testing.T) {
		var telas []struct {
			Titulo string `json:"titulo"`
		}
		pega(t, srv.URL+"/api/telas", &telas)
		if len(telas) < 2 || telas[0].Titulo != "Hoje" || telas[1].Titulo != "Health Score" {
			t.Fatalf("esperava Hoje e Health Score em primeiro, veio %+v", telas)
		}
	})
}

// TestWebProtegeLocal garante que a API só atende o navegador local: Host de
// fora (DNS rebinding) e Origin de outro site em POST (CSRF) são recusados.
func TestWebProtegeLocal(t *testing.T) {
	srv := servidorTeste(t)

	t.Run("host de fora é recusado", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/telas", nil)
		req.Host = "ataque.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host forjado devia dar 403, veio %d", resp.StatusCode)
		}
	})

	t.Run("origin de outro site em POST é recusado", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/executar",
			strings.NewReader(`{"tela":0,"acao":0,"vals":[]}`))
		req.Header.Set("Origin", "https://ataque.example.com")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Origin de fora devia dar 403, veio %d", resp.StatusCode)
		}
	})

	t.Run("origin local em POST passa", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/executar",
			strings.NewReader(`{"tela":0,"acao":0,"vals":[]}`))
		req.Header.Set("Origin", srv.URL)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("Origin local não devia ser barrado (veio 403)")
		}
	})
}

// TestSemANSI garante que as cores ANSI da saída da CLI (asciigraph, gráficos de
// viz.go) são removidas antes de servir ao navegador — senão apareceriam como
// lixo no <pre>. Os caracteres de bloco/desenho são preservados.
func TestSemANSI(t *testing.T) {
	in := "  \x1b[33m███\x1b[0m 48/100 \x1b[90m▒▒▒\x1b[0m"
	got := semANSI(in)
	want := "  ███ 48/100 ▒▒▒"
	if got != want {
		t.Fatalf("semANSI = %q, queria %q", got, want)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("sobrou ESC em %q", got)
	}
}

// TestIniciarApp sobe a web do app desktop numa porta livre e confere a rota
// POST /api/focar, que só existe no app: é por ela que uma segunda cópia traz
// a janela aberta para a frente.
func TestIniciarApp(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	focou := make(chan struct{}, 1)
	srv, porta, err := IniciarApp(conn, func() { focou <- struct{}{} })
	if err != nil {
		t.Fatalf("IniciarApp: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if porta == 0 {
		t.Fatal("a porta escolhida devia vir preenchida")
	}

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/api/focar", porta), "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/focar: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/focar: status %d", resp.StatusCode)
	}
	select {
	case <-focou:
	default:
		t.Error("POST /api/focar devia chamar aoFocar")
	}

	// fora do app (prisma --web) a rota não existe
	web := servidorTeste(t)
	resp, err = http.Post(web.URL+"/api/focar", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("sem app, POST /api/focar devia dar 404, veio %d", resp.StatusCode)
	}
}

// TestWebTrocaDeModo cobre o seletor da página: o mesmo servidor atende o
// pessoal, a empresa e o Analytics pela query "modo", abrindo o banco de cada
// um só na primeira vez que é pedido.
func TestWebTrocaDeModo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRISMA_DB", filepath.Join(dir, "pessoal.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	s := novoServidorWeb(webPessoal, conn)
	aberturas := map[string]int{}
	s.abrir = func(modo string) (*sql.DB, bool, error) {
		aberturas[modo]++
		c, err := db.Open() // um banco qualquer serve: o que muda são as telas
		return c, modo != webAnalytics, err
	}
	srv := httptest.NewServer(s.rotas())
	t.Cleanup(srv.Close)
	t.Cleanup(s.fechaExtras)

	titulos := func(modo string) []string {
		t.Helper()
		var telas []struct {
			Titulo string `json:"titulo"`
		}
		pega(t, srv.URL+"/api/telas?modo="+modo, &telas)
		var out []string
		for _, tl := range telas {
			out = append(out, tl.Titulo)
		}
		return out
	}
	tem := func(lista []string, alvo string) bool {
		for _, x := range lista {
			if x == alvo {
				return true
			}
		}
		return false
	}

	if p := titulos(webPessoal); tem(p, "Sócios") {
		t.Errorf("o modo pessoal não devia ter as telas da empresa: %v", p)
	}
	if e := titulos(webEmpresa); !tem(e, "Sócios") {
		t.Errorf("o modo empresa devia ter a tela Sócios: %v", e)
	}
	titulos(webEmpresa)
	if aberturas[webEmpresa] != 1 {
		t.Errorf("o banco da empresa devia abrir uma vez só, abriu %d", aberturas[webEmpresa])
	}
	if a, p := titulos(webAnalytics), titulos(webPessoal); fmt.Sprint(a) == fmt.Sprint(p) {
		t.Errorf("o Analytics devia ter as telas próprias, veio %v", a)
	}

	var v struct {
		Modo        string   `json:"modo"`
		Modos       []string `json:"modos"`
		ModoEmpresa bool     `json:"modoEmpresa"`
	}
	pega(t, srv.URL+"/api/versao?modo=empresa", &v)
	if v.Modo != webEmpresa || !v.ModoEmpresa || len(v.Modos) != 3 {
		t.Errorf("api/versao no modo empresa veio %+v", v)
	}

	resp, err := http.Get(srv.URL + "/api/telas?modo=xyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("modo inválido devia dar 400, veio %d", resp.StatusCode)
	}

	// sem quem abra outros modos (o caso do servidorTeste), só o inicial existe
	so := servidorTeste(t)
	resp, err = http.Get(so.URL + "/api/telas?modo=empresa")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("sem abrir, o modo empresa devia dar 400, veio %d", resp.StatusCode)
	}
}
