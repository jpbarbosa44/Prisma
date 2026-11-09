package tui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/db"
	"prisma/internal/seguranca"
)

func servidorSeg(t *testing.T) (*servidorWeb, http.Handler) {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	s := novoServidorWeb(webPessoal, conn)
	return s, s.rotas()
}

// pede faz uma requisição direto no handler, como se viesse de remoto
// (RemoteAddr) para o host dado.
func pede(h http.Handler, metodo, alvo, host, remoto, corpo string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(metodo, alvo, strings.NewReader(corpo))
	r.Host = host
	r.RemoteAddr = remoto
	if corpo != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const local, celular = "127.0.0.1:50000", "192.168.0.20:50000"

func TestWebSemPIN(t *testing.T) {
	_, h := servidorSeg(t)
	if w := pede(h, "GET", "/api/telas", "127.0.0.1:7747", local, ""); w.Code != 200 {
		t.Fatalf("local sem PIN: %d", w.Code)
	}
	// sem PIN, nada que venha de outro aparelho entra
	if w := pede(h, "GET", "/api/telas", "192.168.0.5:8765", celular, ""); w.Code != http.StatusForbidden {
		t.Fatalf("rede sem PIN deveria dar 403: %d", w.Code)
	}
	// /login sem PIN volta para a interface
	if w := pede(h, "GET", "/login", "127.0.0.1:7747", local, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("/login sem PIN: %d", w.Code)
	}
}

func TestWebComPIN(t *testing.T) {
	s, h := servidorSeg(t)
	if err := seguranca.DefinirPIN("2468"); err != nil {
		t.Fatal(err)
	}
	host := "192.168.0.5:8765"
	if w := pede(h, "GET", "/api/telas", host, celular, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sem sessão: %d", w.Code)
	}
	if w := pede(h, "GET", "/?modo=empresa", host, celular, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?volta=%2F%3Fmodo%3Dempresa" {
		t.Fatalf("página sem sessão deveria ir para o login: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, livre := range []string{"/login", "/manifest.webmanifest", "/icone-192.png", "/icone-180.png"} {
		if w := pede(h, "GET", livre, host, celular, ""); w.Code != 200 {
			t.Errorf("%s deveria abrir sem sessão: %d", livre, w.Code)
		}
	}
	if w := pede(h, "POST", "/api/login", host, celular, `{"pin":"1111"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("PIN errado: %d", w.Code)
	}
	w := pede(h, "POST", "/api/login", host, celular, `{"pin":"2468"}`)
	if w.Code != 200 {
		t.Fatalf("PIN certo: %d %s", w.Code, w.Body)
	}
	ck := w.Result().Cookies()
	if len(ck) != 1 || ck[0].Name != cookieSessao || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", ck)
	}
	if w := pede(h, "GET", "/api/telas", host, celular, "", ck[0]); w.Code != 200 {
		t.Fatalf("com sessão: %d", w.Code)
	}
	if w := pede(h, "GET", "/api/versao", host, celular, "", ck[0]); !strings.Contains(w.Body.String(), `"pin":true`) || !strings.Contains(w.Body.String(), `"local":false`) {
		t.Fatalf("versão deveria contar do PIN: %s", w.Body)
	}
	// o celular não liga nem desliga a rede
	if w := pede(h, "POST", "/api/rede", host, celular, `{"ligado":true}`, ck[0]); w.Code != http.StatusForbidden {
		t.Fatalf("rede pelo celular: %d", w.Code)
	}
	// trocar o PIN exige o atual
	if w := pede(h, "POST", "/api/pin", host, celular, `{"atual":"0000","novo":"1357"}`, ck[0]); w.Code != http.StatusForbidden {
		t.Fatalf("trocar sem o atual: %d", w.Code)
	}
	w = pede(h, "POST", "/api/pin", host, celular, `{"atual":"2468","novo":"1357"}`, ck[0])
	if w.Code != 200 {
		t.Fatalf("trocar: %d %s", w.Code, w.Body)
	}
	novo := w.Result().Cookies()
	if w := pede(h, "GET", "/api/telas", host, celular, "", ck[0]); w.Code != http.StatusUnauthorized {
		t.Fatalf("a sessão antiga deveria cair com o PIN novo: %d", w.Code)
	}
	if len(novo) != 1 || pede(h, "GET", "/api/telas", host, celular, "", novo[0]).Code != 200 {
		t.Fatal("quem trocou continua dentro")
	}
	if w := pede(h, "POST", "/api/trancar", host, celular, `{}`, novo[0]); w.Code != 200 {
		t.Fatalf("trancar: %d", w.Code)
	}
	if w := pede(h, "GET", "/api/telas", host, celular, "", novo[0]); w.Code != http.StatusUnauthorized {
		t.Fatalf("depois de trancar: %d", w.Code)
	}
	_ = s
}

func TestWebHostsDaRede(t *testing.T) {
	_, h := servidorSeg(t)
	seguranca.DefinirPIN("2468")
	w := pede(h, "POST", "/api/login", "10.0.0.2:8765", local, `{"pin":"2468"}`)
	ck := w.Result().Cookies()
	if w.Code != 200 || len(ck) != 1 {
		t.Fatalf("login: %d", w.Code)
	}
	casos := []struct {
		host, origem string
		quer         int
	}{
		{"10.0.0.2:8765", "", 400},        // passou das travas: 400 é o corpo inválido
		{"100.101.102.103:8765", "", 400}, // Tailscale
		{"prisma.ataque.com:8765", "", http.StatusForbidden},
		{"8.8.8.8:8765", "", http.StatusForbidden},
		{"10.0.0.2:8765", "http://10.0.0.2:8765", 400},
		{"10.0.0.2:8765", "http://evil.example.com", http.StatusForbidden},
		{"10.0.0.2:8765", "http://10.0.0.3:8765", http.StatusForbidden},
	}
	for _, c := range casos {
		r := httptest.NewRequest("POST", "/api/rede", strings.NewReader("x"))
		r.Host, r.RemoteAddr = c.host, local
		if c.origem != "" {
			r.Header.Set("Origin", c.origem)
		}
		r.AddCookie(ck[0])
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.quer {
			t.Errorf("host %s origem %q: %d, quero %d", c.host, c.origem, rec.Code, c.quer)
		}
	}
}

func TestWebLoginTentativas(t *testing.T) {
	_, h := servidorSeg(t)
	seguranca.DefinirPIN("2468")
	for i := 0; i < 5; i++ {
		pede(h, "POST", "/api/login", "127.0.0.1:7747", "10.0.0.9:1", `{"pin":"0000"}`)
	}
	if w := pede(h, "POST", "/api/login", "127.0.0.1:7747", "10.0.0.9:1", `{"pin":"2468"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("depois de 5 erros, até o certo espera: %d", w.Code)
	}
	if w := pede(h, "POST", "/api/login", "127.0.0.1:7747", local, `{"pin":"2468"}`); w.Code != 200 {
		t.Fatalf("outro endereço não é afetado: %d", w.Code)
	}
}

func TestWebRede(t *testing.T) {
	s, h := servidorSeg(t)
	s.seg.portaRede = 0 // porta livre qualquer
	if err := s.ligaRede(); err == nil {
		t.Fatal("rede sem PIN deveria ser recusada")
	}
	seguranca.DefinirPIN("2468")
	w := pede(h, "POST", "/api/login", "127.0.0.1:7747", local, `{"pin":"2468"}`)
	ck := w.Result().Cookies()[0]
	if w := pede(h, "POST", "/api/rede", "127.0.0.1:7747", local, `{"ligado":true}`, ck); w.Code != 200 || !strings.Contains(w.Body.String(), `"ligado":true`) {
		t.Fatalf("ligar: %d %s", w.Code, w.Body)
	}
	if c, _ := seguranca.Carrega(); !c.Rede {
		t.Fatal("a escolha deveria ficar salva")
	}
	if w := pede(h, "POST", "/api/rede", "127.0.0.1:7747", local, `{"ligado":false}`, ck); !strings.Contains(w.Body.String(), `"ligado":false`) {
		t.Fatalf("desligar: %s", w.Body)
	}
	// remover o PIN desliga a rede junto
	s.ligaRede()
	pede(h, "POST", "/api/pin", "127.0.0.1:7747", local, `{"atual":"2468","novo":""}`, ck)
	if s.estadoRede()["ligado"] != false {
		t.Fatal("sem PIN, a rede cai")
	}
}

func TestWebFocarComPIN(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	focou := 0
	s := novoServidorWeb(webPessoal, conn)
	s.aoFocar = func() { focou++ }
	h := s.rotas()
	seguranca.DefinirPIN("2468")
	if w := pede(h, "POST", "/api/focar", "127.0.0.1:7747", local, ""); w.Code != 200 || focou != 1 {
		t.Fatalf("focar local com PIN: %d, focou %d", w.Code, focou)
	}
	if w := pede(h, "POST", "/api/focar", "192.168.0.5:8765", celular, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("focar pela rede sem sessão: %d", w.Code)
	}
}

func TestWebRelatorioComPINVaiAoLogin(t *testing.T) {
	_, h := servidorSeg(t)
	seguranca.DefinirPIN("2468")
	r := httptest.NewRequest("GET", "/api/relatorio?mes=2026-10", nil)
	r.Host, r.RemoteAddr = "127.0.0.1:7747", local
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?volta=%2Fapi%2Frelatorio%3Fmes%3D2026-10" {
		t.Fatalf("navegação para o relatório: %d %q", w.Code, w.Header().Get("Location"))
	}
	r.Header.Set("Sec-Fetch-Mode", "cors") // fetch da página: 401
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("fetch: %d", w.Code)
	}
}
