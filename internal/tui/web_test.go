package tui

import (
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
	srv := httptest.NewServer((&servidorWeb{telas: novasTelas(conn, false)}).rotas())
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
	srv := httptest.NewServer((&servidorWeb{conn: conn, telas: novasTelasAnalytics(conn), modoAnalytics: true}).rotas())
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
		if len(telas) == 0 || telas[0].Titulo != "Health Score" {
			t.Fatalf("esperava Health Score em primeiro, veio %+v", telas)
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
