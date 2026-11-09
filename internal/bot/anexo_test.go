package bot

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/app"
	"prisma/internal/db"
)

// TestComprovanteViraAnexo: a foto mandada ao bot fica no Telegram e também
// numa cópia local, que aparece no 📎 da web.
func TestComprovanteViraAnexo(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/f1.jpg"}}`))
		case r.URL.Path == "/file/botX/photos/f1.jpg":
			w.Write(jpeg)
		default:
			w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer srv.Close()
	s := &sessao{conn: conn, cli: &cliente{token: "X", http: srv.Client(), base: srv.URL}, cfg: &config{}}
	cria, _, _, err := app.CriarLancamentos(conn, app.LancamentoParams{Tipo: "pagar", Desc: "Luz", Valor: 100, Venc: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.anexaComprovante(cria[0].ID, "f1"); err != nil {
		t.Fatal(err)
	}
	as, err := app.AnexosDe(conn, cria[0].ID)
	if err != nil || len(as) != 1 || as[0].Tipo != "image/jpeg" {
		t.Fatalf("anexos: %+v %v", as, err)
	}
}
