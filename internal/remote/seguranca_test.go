package remote_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"prisma/internal/db"
	"prisma/internal/remote"
)

// montaServidor sobe um servidor remoto (sem TLS, via httptest) sobre um banco
// temporário e devolve o servidor de teste e o token aceito. É o ponto de
// partida dos testes que falam HTTP cru com o servidor, sem passar pelo driver.
func montaServidor(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	const token = "tok-seguranca"
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "p.db"))
	local, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { local.Close() })
	ts := httptest.NewServer(remote.NovoServidor(local, token).Handler())
	t.Cleanup(ts.Close)
	return ts, token
}

// req faz um POST com o token informado e o corpo (qualquer struct serializável,
// ou nil) e devolve o status e o corpo da resposta já lido.
func req(t *testing.T, ts *httptest.Server, rota, token string, corpo any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if corpo != nil {
		if err := json.NewEncoder(&buf).Encode(corpo); err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(http.MethodPost, ts.URL+rota, &buf)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set(remote.HeaderToken, token)
	resp, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// abreSessao abre uma sessão e devolve o id (atalho para os testes de fluxo).
func abreSessao(t *testing.T, ts *httptest.Server, token string) string {
	t.Helper()
	st, body := req(t, ts, remote.RotaOpen, token, struct{}{})
	if st != http.StatusOK {
		t.Fatalf("open: status %d (%s)", st, body)
	}
	var r remote.RespOpen
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("decodificando open: %v", err)
	}
	return r.SessionID
}

// TestTokenErrado garante que o servidor recusa (401) quem não tem o segredo, e
// aceita quem tem.
func TestTokenErrado(t *testing.T) {
	ts, token := montaServidor(t)

	if st, _ := req(t, ts, remote.RotaPing, "errado", remote.ReqSessao{}); st != http.StatusUnauthorized {
		t.Errorf("token errado: status %d; quero 401", st)
	}
	if st, _ := req(t, ts, remote.RotaPing, "", remote.ReqSessao{}); st != http.StatusUnauthorized {
		t.Errorf("token vazio: status %d; quero 401", st)
	}
	if st, _ := req(t, ts, remote.RotaPing, token, remote.ReqSessao{}); st != http.StatusOK {
		t.Errorf("token certo: status %d; quero 200", st)
	}
}

// TestMetodoNaoPost garante que só POST é aceito (defesa contra GET de browser
// ou pré-flights bobos chegarem aos handlers).
func TestMetodoNaoPost(t *testing.T) {
	ts, token := montaServidor(t)
	r, _ := http.NewRequest(http.MethodGet, ts.URL+remote.RotaPing, nil)
	r.Header.Set(remote.HeaderToken, token)
	resp, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d; quero 405", resp.StatusCode)
	}
}

// TestSessaoDesconhecida garante 404 ao operar com um id de sessão que não
// existe (ex.: sessão já expirada pelo reaper, ou id forjado).
func TestSessaoDesconhecida(t *testing.T) {
	ts, token := montaServidor(t)
	st, _ := req(t, ts, remote.RotaExec, token, remote.ReqExec{SessionID: "naoexiste", SQL: "SELECT 1"})
	if st != http.StatusNotFound {
		t.Errorf("sessão inexistente: status %d; quero 404", st)
	}
}

// TestTransacaoConflito cobre os 409: abrir transação duas vezes e finalizar
// sem transação aberta.
func TestTransacaoConflito(t *testing.T) {
	ts, token := montaServidor(t)
	sid := abreSessao(t, ts, token)

	if st, _ := req(t, ts, remote.RotaBegin, token, remote.ReqSessao{SessionID: sid}); st != http.StatusOK {
		t.Fatalf("primeiro begin: status %d; quero 200", st)
	}
	if st, _ := req(t, ts, remote.RotaBegin, token, remote.ReqSessao{SessionID: sid}); st != http.StatusConflict {
		t.Errorf("segundo begin: status %d; quero 409", st)
	}
	if st, _ := req(t, ts, remote.RotaCommit, token, remote.ReqSessao{SessionID: sid}); st != http.StatusOK {
		t.Fatalf("commit: status %d; quero 200", st)
	}
	if st, _ := req(t, ts, remote.RotaCommit, token, remote.ReqSessao{SessionID: sid}); st != http.StatusConflict {
		t.Errorf("commit sem tx: status %d; quero 409", st)
	}
}

// TestCorpoGrande garante que um corpo absurdo é barrado (MaxBytesReader) em vez
// de ser lido inteiro na memória.
func TestCorpoGrande(t *testing.T) {
	ts, token := montaServidor(t)
	sid := abreSessao(t, ts, token)

	gigante := strings.Repeat("x", 9<<20) // 9 MiB > teto de 8 MiB
	st, _ := req(t, ts, remote.RotaExec, token,
		remote.ReqExec{SessionID: sid, SQL: "SELECT '" + gigante + "'"})
	if st < 400 {
		t.Errorf("corpo gigante: status %d; quero erro (>=400)", st)
	}
}

// TestConcorrencia exercita o servidor com várias goroutines do cliente ao mesmo
// tempo. Não verifica um valor específico: serve para o `go test -race` flagrar
// corridas de dados no servidor (sessões, reaper, ping).
func TestConcorrencia(t *testing.T) {
	cli, _ := monta(t) // helper de roundtrip_test.go
	if _, err := cli.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('x', 0)`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := cli.Exec(`UPDATE contas SET saldo_inicial = saldo_inicial + 1 WHERE id = 1`); err != nil {
					t.Errorf("goroutine %d exec: %v", n, err)
					return
				}
				var s int64
				if err := cli.QueryRow(`SELECT saldo_inicial FROM contas WHERE id = 1`).Scan(&s); err != nil {
					t.Errorf("goroutine %d query: %v", n, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	var saldo int64
	if err := cli.QueryRow(`SELECT saldo_inicial FROM contas WHERE id = 1`).Scan(&saldo); err != nil {
		t.Fatal(err)
	}
	if saldo != 16*20 {
		t.Fatalf("saldo final %d; quero %d (todas as escritas concorrentes devem contar)", saldo, 16*20)
	}
}
