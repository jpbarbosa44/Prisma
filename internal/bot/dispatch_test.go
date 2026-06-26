package bot

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"prisma/internal/db"
)

// servidorFake simula a API do Telegram: responde {"ok":true} a tudo e guarda os
// textos enviados (o parâmetro "text" dos sendMessage), para os testes inspecionarem
// o que o bot respondeu sem tocar na rede.
type servidorFake struct {
	srv      *httptest.Server
	mu       sync.Mutex
	enviados []string
}

func novoServidorFake(t *testing.T) *servidorFake {
	t.Helper()
	f := &servidorFake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if txt := r.FormValue("text"); txt != "" {
			f.mu.Lock()
			f.enviados = append(f.enviados, txt)
			f.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *servidorFake) textos() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.enviados...)
}

func sessaoTeste(t *testing.T) (*sessao, *servidorFake) {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	f := novoServidorFake(t)
	cli := &cliente{token: "X", http: f.srv.Client(), base: f.srv.URL}
	return &sessao{conn: conn, cli: cli, cfg: &config{}}, f
}

func contemAlgum(textos []string, sub string) bool {
	for _, t := range textos {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

// TestTrataComandoRoteia exercita os comandos /xxx do bot sobre um banco real,
// conferindo que cada um responde algo coerente. Pega regressões de roteamento e
// quebras nos comandos de consulta.
func TestTrataComandoRoteia(t *testing.T) {
	casos := []struct {
		nome, texto, contem string
	}{
		{"saldo", "/saldo", "POSI"},
		{"previsao", "/previsao", "PREVIS"},
		{"pendentes", "/pendentes", ""}, // só precisa responder sem quebrar
		{"cartoes", "/cartoes", ""},
		{"fatura sem id", "/fatura", "uso: /fatura"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s, fake := sessaoTeste(t)
			s.trataComando(&mensagem{Chat: chat{ID: 1}}, c.texto)
			ts := fake.textos()
			if len(ts) == 0 {
				t.Fatalf("%q não respondeu nada", c.texto)
			}
			if c.contem != "" && !contemAlgum(ts, c.contem) {
				t.Fatalf("%q: nenhuma resposta contém %q; respostas: %v", c.texto, c.contem, ts)
			}
		})
	}
}

// TestTrataCallbackAutorizaPeloChat confere que os botões inline (desfazer,
// quitar) são autorizados pelo chat onde a mensagem está, e não pelo id de quem
// clicou. Isso é o que faz os botões funcionarem em grupo, onde o chat tem id
// próprio e cb.De.ID é a pessoa que tocou no botão.
func TestTrataCallbackAutorizaPeloChat(t *testing.T) {
	const grupo = -100 // id de grupo (negativo); diferente de qualquer usuário

	casos := []struct {
		nome   string
		chatID int64     // chat pareado na config
		cb     *callback // callback recebido
		atende bool      // espera que o botão seja processado
	}{
		{
			nome:   "grupo: clique de membro é atendido",
			chatID: grupo,
			cb:     &callback{De: usuario{ID: 777}, Mensagem: &mensagem{Chat: chat{ID: grupo}}, Dados: "undo:0"},
			atende: true,
		},
		{
			nome:   "privado: dono ainda é atendido",
			chatID: 42,
			cb:     &callback{De: usuario{ID: 42}, Mensagem: &mensagem{Chat: chat{ID: 42}}, Dados: "undo:0"},
			atende: true,
		},
		{
			nome:   "outro chat é ignorado",
			chatID: grupo,
			cb:     &callback{De: usuario{ID: 777}, Mensagem: &mensagem{Chat: chat{ID: 999}}, Dados: "undo:0"},
			atende: false,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s, fake := sessaoTeste(t)
			s.cfg.ChatID = c.chatID
			s.trataCallback(c.cb)
			// "undo:0" não apaga nada, mas um callback atendido edita a mensagem
			// acrescentando a nota com ↩️; um ignorado não manda texto nenhum.
			editou := contemAlgum(fake.textos(), "↩️")
			if editou != c.atende {
				t.Fatalf("atendido=%v, esperado %v; respostas: %v", editou, c.atende, fake.textos())
			}
		})
	}
}

// TestTrataComandoDesconhecidoMandaAjuda confere que um comando não reconhecido
// cai no default e devolve exatamente a ajuda.
func TestTrataComandoDesconhecidoMandaAjuda(t *testing.T) {
	s, fake := sessaoTeste(t)
	s.trataComando(&mensagem{Chat: chat{ID: 1}}, "/naoexiste")
	ts := fake.textos()
	if len(ts) != 1 || ts[0] != ajuda {
		t.Fatalf("comando desconhecido deveria mandar a ajuda; veio %v", ts)
	}
}
