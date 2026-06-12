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
	srv := httptest.NewServer((&servidorWeb{telas: novasTelas(conn)}).rotas())
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
