package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prisma/internal/db"
)

// enviaExtrato posta um extrato em /api/importar e decodifica a resposta.
func enviaExtrato(t *testing.T, url, nome, conteudo, destino string, confirmar bool, v any) int {
	t.Helper()
	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	fw, _ := mw.CreateFormFile("arquivo", nome)
	io.WriteString(fw, conteudo)
	mw.WriteField("destino", destino)
	if confirmar {
		mw.WriteField("confirmar", "1")
	}
	mw.Close()
	resp, err := http.Post(url+"/api/importar", mw.FormDataContentType(), &corpo)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(v)
	return resp.StatusCode
}

func TestWebImportarExportar(t *testing.T) {
	srv := servidorTeste(t)
	conn, err := db.Open() // mesmo PRISMA_DB do servidorTeste
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Exec(`INSERT INTO contas (id, nome) VALUES (1, 'Nubank')`)
	extrato := "data;descricao;valor\n01/10/2026;Padaria;-12,50\n02/10/2026;Salario;3.000,00\n03/10/2026;Mercado;-200,00\n"

	var destinos []struct{ Valor, Rotulo string }
	pega(t, srv.URL+"/api/destinos", &destinos)
	if len(destinos) != 1 || destinos[0].Valor != "conta:1" || !strings.Contains(destinos[0].Rotulo, "Nubank") {
		t.Fatalf("destinos = %+v", destinos)
	}

	var previa struct {
		Novos, Duplicados int
		Entradas, Saidas  int64
		Destino           string
	}
	if st := enviaExtrato(t, srv.URL, "extrato.csv", extrato, "conta:1", false, &previa); st != http.StatusOK {
		t.Fatalf("prévia: status %d", st)
	}
	if previa.Novos != 3 || previa.Entradas != 300000 || previa.Saidas != 21250 || previa.Destino != "conta Nubank" {
		t.Fatalf("prévia = %+v", previa)
	}
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 0 {
		t.Fatal("a prévia não pode gravar nada")
	}

	var gravou struct {
		Lote    string
		Criados int
	}
	if st := enviaExtrato(t, srv.URL, "extrato.csv", extrato, "conta:1", true, &gravou); st != http.StatusOK || gravou.Criados != 3 {
		t.Fatalf("gravar: status %d, %+v", st, gravou)
	}
	if !strings.HasPrefix(gravou.Lote, "web-") {
		t.Errorf("lote = %q, quer web-...", gravou.Lote)
	}

	// reimportar o mesmo extrato: tudo duplicado, nada a gravar
	if enviaExtrato(t, srv.URL, "extrato.csv", extrato, "conta:1", false, &previa); previa.Novos != 0 || previa.Duplicados != 3 {
		t.Fatalf("reimportação: %+v", previa)
	}
	var erro struct{ Erro string }
	if st := enviaExtrato(t, srv.URL, "extrato.csv", extrato, "conta:1", true, &erro); st != http.StatusUnprocessableEntity {
		t.Fatalf("regravar tudo duplicado devia falhar, veio %d %+v", st, erro)
	}

	// exportar traz os três
	resp, err := http.Get(srv.URL + "/api/exportar.csv?mes=2026-10")
	if err != nil {
		t.Fatal(err)
	}
	csv, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "prisma-lancamentos-2026-10.csv") ||
		strings.Count(string(csv), "\n") != 4 || !strings.Contains(string(csv), "Padaria") {
		t.Errorf("exportar: %q\n%s", resp.Header.Get("Content-Disposition"), csv)
	}

	// o lote aparece e dá para desfazer
	var lotes []struct {
		Lote  string
		Itens int
	}
	pega(t, srv.URL+"/api/lotes", &lotes)
	if len(lotes) != 1 || lotes[0].Lote != gravou.Lote || lotes[0].Itens != 3 {
		t.Fatalf("lotes = %+v", lotes)
	}
	b, _ := json.Marshal(map[string]string{"lote": gravou.Lote})
	r2, err := http.Post(srv.URL+"/api/desfazer-lote", "application/json", bytes.NewReader(b))
	if err != nil || r2.StatusCode != http.StatusOK {
		t.Fatalf("desfazer: %v %v", err, r2.StatusCode)
	}
	r2.Body.Close()
	conn.QueryRow(`SELECT COUNT(*) FROM lancamentos`).Scan(&n)
	if n != 0 {
		t.Fatalf("desfazer deixou %d lançamentos", n)
	}

	// sem destino e com formato errado: erros claros
	if st := enviaExtrato(t, srv.URL, "extrato.csv", extrato, "", false, &erro); st != http.StatusBadRequest {
		t.Errorf("sem destino: status %d", st)
	}
	if st := enviaExtrato(t, srv.URL, "foto.png", "x", "conta:1", false, &erro); st != http.StatusUnprocessableEntity || !strings.Contains(erro.Erro, "formato") {
		t.Errorf("formato errado: status %d %+v", st, erro)
	}
}

// TestAppSalvaExportacao: no app (a janela não baixa arquivos) a exportação
// vai para a pasta de downloads, sem sobrescrever um CSV já existente.
func TestAppSalvaExportacao(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	downloads := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", downloads)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento) VALUES ('pagar', 'Luz', 100, '2026-10-05')`)
	srv, porta, err := IniciarApp(conn, func() {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	for i := 0; i < 2; i++ {
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/api/exportar?mes=2026-10", porta), "application/json", nil)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("exportar no app: %v %v", err, resp.StatusCode)
		}
		resp.Body.Close()
	}
	arqs, _ := filepath.Glob(filepath.Join(downloads, "prisma-lancamentos-2026-10*.csv"))
	if len(arqs) != 2 {
		t.Fatalf("esperava 2 arquivos (o segundo sem sobrescrever), veio %v", arqs)
	}
	b, _ := os.ReadFile(filepath.Join(downloads, "prisma-lancamentos-2026-10.csv"))
	if !strings.Contains(string(b), "Luz") {
		t.Errorf("CSV exportado:\n%s", b)
	}
}

// TestAppLembretes: no app, /api/versao traz o estado dos lembretes e
// POST /api/lembretes o muda; fora do app, nada disso existe.
func TestAppLembretes(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ligado := true
	srv, porta, err := IniciarAppCom(conn, OpcoesApp{
		AoFocar:       func() {},
		Lembretes:     func() bool { return ligado },
		LigaLembretes: func(b bool) error { ligado = b; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	base := fmt.Sprintf("http://127.0.0.1:%d", porta)
	var v struct{ Lembretes *bool }
	pega(t, base+"/api/versao", &v)
	if v.Lembretes == nil || !*v.Lembretes {
		t.Fatalf("versao.lembretes = %v, quer true", v.Lembretes)
	}
	resp, err := http.Post(base+"/api/lembretes", "application/json", strings.NewReader(`{"ligado":false}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST lembretes: %v %v", err, resp.StatusCode)
	}
	resp.Body.Close()
	if ligado {
		t.Error("POST {ligado:false} devia desligar")
	}

	// a web comum (sem app) não tem o interruptor
	web := servidorTeste(t)
	v.Lembretes = nil
	pega(t, web.URL+"/api/versao", &v)
	if v.Lembretes != nil {
		t.Error("fora do app, /api/versao não devia ter lembretes")
	}
}

// TestAppAtualizar: no app, o aviso de versão ganha o botão (podeAtualizar)
// e POST /api/atualizar chama o atualizador, repassando a mensagem ou o erro.
func TestAppAtualizar(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	falha := false
	srv, porta, err := IniciarAppCom(conn, OpcoesApp{
		AoFocar: func() {},
		Atualizar: func() (string, error) {
			if falha {
				return "", errors.New("sem rede")
			}
			return "Atualizado para v9.9.9. Reabrindo o Prisma…", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	base := fmt.Sprintf("http://127.0.0.1:%d", porta)
	var v struct{ PodeAtualizar bool }
	pega(t, base+"/api/versao", &v)
	if !v.PodeAtualizar {
		t.Error("no app, /api/versao devia ter podeAtualizar")
	}
	for _, f := range []bool{false, true} {
		falha = f
		resp, err := http.Post(base+"/api/atualizar", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		var r struct{ Msg, Erro string }
		json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if !f && (resp.StatusCode != http.StatusOK || !strings.Contains(r.Msg, "v9.9.9")) {
			t.Errorf("atualizar: %d %+v", resp.StatusCode, r)
		}
		if f && (resp.StatusCode != http.StatusBadGateway || r.Erro != "sem rede") {
			t.Errorf("atualizar com falha: %d %+v", resp.StatusCode, r)
		}
	}
}

func TestWebRelatorio(t *testing.T) {
	srv := servidorTeste(t)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em)
		VALUES ('pagar', 'Aluguel <b>', 150000, 'moradia', '2026-10-10', 'quitado', '2026-10-10')`)
	resp, err := http.Get(srv.URL + "/api/relatorio?mes=2026-10")
	if err != nil {
		t.Fatal(err)
	}
	corpo, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(corpo)
	for _, want := range []string{"Fechamento de outubro/2026", "R$ 1.500,00", "moradia", "relatorio?mes=2026-09", "setembro/2026", "Aluguel &lt;b&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("relatório sem %q", want)
		}
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("status %d, tipo %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if r2, _ := http.Get(srv.URL + "/api/relatorio?mes=xx"); r2.StatusCode != http.StatusBadRequest {
		t.Errorf("mês inválido: status %d", r2.StatusCode)
	}
}
