// prisma-app: o Prisma numa janela própria. Sobe a interface web numa porta
// livre de 127.0.0.1 e a mostra num WebView do sistema (WebKitGTK no Linux,
// WebView2 no Windows, WKWebView no macOS). Fecha junto com a janela.
//
// O WebView vem do go-webview, que carrega a biblioteca nativa sem CGO: o
// prisma-app compila de qualquer sistema para qualquer sistema, como o prisma.
// No Windows, compile com -ldflags "-H=windowsgui" (sem console por trás).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	webview "github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded" // embute a biblioteca nativa do WebView

	"prisma/internal/app"
	"prisma/internal/db"
	"prisma/internal/remote"
	"prisma/internal/tui"
	"prisma/internal/update"
)

// janela guarda o WebView aberto, para o /api/focar (que chega pela thread do
// servidor HTTP) poder trazê-lo para a frente.
var janela struct {
	sync.Mutex
	w webview.WebView
}

func main() {
	url, fechar, err := preparar(func() {
		janela.Lock()
		defer janela.Unlock()
		if w := janela.w; w != nil {
			w.Dispatch(func() { focar(w.Window()) })
		}
	})
	if err == nil && url == "" {
		return // já havia uma janela aberta, e ela veio para a frente
	}

	antesDaJanela()
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Prisma")
	w.SetSize(1200, 800, webview.HintNone)
	depoisDaJanela(w.Window())
	if err != nil {
		w.SetHtml(paginaDeErro(err))
	} else {
		// links de fora (release no GitHub) vão para o navegador do sistema
		w.Bind("abrirExterno", func(u string) {
			if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
				tui.AbreNavegador(u)
			}
		})
		w.Navigate(url)
	}
	janela.Lock()
	janela.w = w
	janela.Unlock()

	w.Run()

	janela.Lock()
	janela.w = nil
	janela.Unlock()
	if fechar != nil {
		fechar()
	}
}

// trava guarda a porta da janela aberta, ao lado do banco, para a próxima
// cópia achá-la.
func trava() (string, error) {
	p, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "app.lock"), nil
}

// preparar abre o banco, sobe o servidor e devolve a URL da página e a função
// que desliga tudo ao fechar a janela. URL vazia sem erro: outra janela já
// estava aberta (e recebeu o pedido para aparecer).
func preparar(aoFocar func()) (url string, fechar func(), err error) {
	lock, err := trava()
	if err != nil {
		return "", nil, err
	}
	if porta, err := os.ReadFile(lock); err == nil && focarOutra(strings.TrimSpace(string(porta))) {
		return "", nil, nil
	}

	conn, err := abrirBanco()
	if err != nil {
		return "", nil, err
	}
	// checa por uma versão nova em segundo plano (no máximo 1x/dia), para o
	// aviso no topo da página
	go update.AtualizaCache()

	srv, porta, err := tui.IniciarApp(conn, aoFocar)
	if err != nil {
		conn.Close()
		return "", nil, err
	}
	os.WriteFile(lock, []byte(strconv.Itoa(porta)), 0o600)
	fechar = func() {
		os.Remove(lock)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		conn.Close()
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", porta), fechar, nil
}

// abrirBanco abre o banco pessoal como o prisma faz ao abrir: local, ou um
// servidor Prisma na rede no modo cliente. Dono do banco, materializa as
// recorrências e quita os pendentes vencidos.
func abrirBanco() (*sql.DB, error) {
	cfg, err := remote.Carrega()
	if err != nil {
		return nil, fmt.Errorf("configuração: %w", err)
	}
	conn, err := db.Abrir(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Modo != remote.ModoCliente {
		if _, err := app.GerarRecorrencias(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("recorrências: %w", err)
		}
		if _, err := app.QuitarVencidos(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("quitar vencidos: %w", err)
		}
	}
	return conn, nil
}

// focarOutra pede à janela que já está aberta que venha para a frente.
func focarOutra(porta string) bool {
	if _, err := strconv.Atoi(porta); err != nil {
		return false
	}
	cli := &http.Client{Timeout: 2 * time.Second}
	resp, err := cli.Post("http://127.0.0.1:"+porta+"/api/focar", "application/json", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func paginaDeErro(err error) string {
	return `<!doctype html><meta charset=utf-8><body style="background:#0e1014;color:#d8dee4;font:15px monospace;padding:32px">` +
		`<h2 style="color:#ee6a6a">O Prisma não conseguiu abrir</h2><pre style="white-space:pre-wrap">` +
		html.EscapeString(err.Error()) +
		`</pre><p style="color:#6c7380">O app de terminal (prisma) mostra o mesmo erro com mais detalhes.</p></body>`
}
