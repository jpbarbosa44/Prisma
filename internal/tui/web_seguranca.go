package tui

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"

	"prisma/internal/seguranca"
	"prisma/packaging"
)

// O PIN e o acesso pelo celular. Com PIN definido (prisma pin definir, ou pela
// tela Como usar), toda rota exige a sessão aberta em /login; sem ele, nada
// muda para quem usa só no computador. O acesso pela rede de casa só liga com
// PIN: um 2º ouvinte em 0.0.0.0:8765 serve as mesmas rotas para o celular.

//go:embed login.html
var paginaLogin []byte

//go:embed manifest.webmanifest
var manifestoWeb []byte

const cookieSessao = "prisma_s"

// estadoSeguranca é a parte do servidorWeb que cuida do PIN e da rede.
type estadoSeguranca struct {
	sessoes    *seguranca.Sessoes
	tentativas *seguranca.Tentativas

	muCfg   sync.Mutex
	cfg     seguranca.Config
	cfgHora time.Time // mtime do seguranca.json lido por último

	muRede    sync.Mutex
	redeSrv   *http.Server
	redeErro  string
	portaRede int
}

func novoEstadoSeguranca() *estadoSeguranca {
	return &estadoSeguranca{
		sessoes:    seguranca.NovasSessoes(),
		tentativas: seguranca.NovasTentativas(),
		portaRede:  seguranca.PortaRede,
	}
}

// config devolve a configuração atual, relendo o arquivo se ele mudou (o PIN
// pode ser trocado pelo terminal com o servidor no ar).
func (e *estadoSeguranca) config() seguranca.Config {
	e.muCfg.Lock()
	defer e.muCfg.Unlock()
	p, err := seguranca.Caminho()
	if err != nil {
		return e.cfg
	}
	var hora time.Time
	if st, err := os.Stat(p); err == nil {
		hora = st.ModTime()
	}
	if hora.Equal(e.cfgHora) && !hora.IsZero() {
		return e.cfg
	}
	if c, err := seguranca.Carrega(); err == nil {
		if c.Hash != e.cfg.Hash && e.cfg.Hash != "" {
			e.sessoes.EncerraTodas() // PIN trocado: todo mundo entra de novo
		}
		e.cfg, e.cfgHora = c, hora
	}
	return e.cfg
}

// ipLocal diz se o pedido veio do próprio computador.
func ipLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// tailscale é a faixa CGNAT que o Tailscale usa (100.64.0.0/10), sugerido no
// manual para abrir o Prisma fora de casa.
var tailscale = netip.MustParsePrefix("100.64.0.0/10")

// hostDaRede diz se o Host é o endereço IP de uma rede privada (192.168.x,
// 10.x, 172.16-31.x, fd00::/8, link-local ou Tailscale). Só IP literal: um
// nome de domínio seria a porta para o DNS rebinding.
func hostDaRede(h string) bool {
	ip, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || tailscale.Contains(ip)
}

// rotaLivre são as rotas que abrem sem sessão: a tela do PIN e o que o
// navegador do celular busca para instalar o atalho.
func rotaLivre(p string) bool {
	switch p {
	case "/login", "/api/login", "/manifest.webmanifest", "/icone-192.png", "/icone-512.png", "/icone-180.png", "/favicon.ico":
		return true
	}
	return false
}

// exigeSessao tranca as rotas quando há PIN, e barra quem vem de fora do
// computador quando não há (o ouvinte da rede nem sobe sem PIN; isto é a
// segunda trava).
func (s *servidorWeb) exigeSessao(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.seg.config()
		if !cfg.TemPIN() {
			if !ipLocal(r) {
				http.Error(w, "defina um PIN para acessar pela rede", http.StatusForbidden)
				return
			}
			h.ServeHTTP(w, r)
			return
		}
		// a 2ª cópia do app só pede para a janela vir para a frente: não lê nada
		if rotaLivre(r.URL.Path) || (r.URL.Path == "/api/focar" && ipLocal(r)) {
			h.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(cookieSessao); err == nil && s.seg.sessoes.Valida(c.Value, cfg.Ociosidade()) {
			h.ServeHTTP(w, r)
			return
		}
		// as páginas abertas direto (a interface, o relatório para imprimir no
		// navegador do sistema) vão para o PIN e voltam; as chamadas da API
		// recebem 401 e a própria página leva ao PIN
		if strings.HasPrefix(r.URL.Path, "/api/") && !navegacao(r) {
			respondeErro(w, http.StatusUnauthorized, errors.New("trancado: digite o PIN"))
			return
		}
		http.Redirect(w, r, "/login?volta="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	})
}

// navegacao diz se o pedido é o navegador abrindo uma página (e não um fetch).
func navegacao(r *http.Request) bool {
	if m := r.Header.Get("Sec-Fetch-Mode"); m != "" {
		return m == "navigate"
	}
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

func (s *servidorWeb) paginaDeLogin(w http.ResponseWriter, r *http.Request) {
	if !s.seg.config().TemPIN() {
		http.Redirect(w, r, "./", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(paginaLogin)
}

func ipDe(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// apiLogin confere o PIN e abre a sessão.
func (s *servidorWeb) apiLogin(w http.ResponseWriter, r *http.Request) {
	var p struct {
		PIN string `json:"pin"`
	}
	if err := lerJSON(w, r, &p); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.seg.config()
	ip := ipDe(r)
	if d := s.seg.tentativas.Espera(ip); d > 0 {
		respondeErro(w, http.StatusTooManyRequests, fmt.Errorf("muitas tentativas: espere %s", arredonda(d)))
		return
	}
	if !cfg.Confere(p.PIN) {
		msg := "PIN errado"
		if d := s.seg.tentativas.Falhou(ip); d > 0 {
			msg += "; espere " + arredonda(d) + " para tentar de novo"
		}
		respondeErro(w, http.StatusUnauthorized, errors.New(msg))
		return
	}
	s.seg.tentativas.Acertou(ip)
	http.SetCookie(w, &http.Cookie{
		Name: cookieSessao, Value: s.seg.sessoes.Nova(), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	responde(w, map[string]bool{"ok": true})
}

func arredonda(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Seconds()+0.999))
	}
	return fmt.Sprintf("%d min", int(d.Minutes()+0.999))
}

// apiTrancar fecha a sessão (botão 🔒 e o tempo parado na página).
func (s *servidorWeb) apiTrancar(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieSessao); err == nil {
		s.seg.sessoes.Encerra(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieSessao, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	responde(w, map[string]bool{"ok": true})
}

// apiPIN define, troca ou remove o PIN (novo vazio = remover). Com PIN, exige
// o atual — e conta como tentativa, para não virar atalho de força bruta.
func (s *servidorWeb) apiPIN(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Atual string `json:"atual"`
		Novo  string `json:"novo"`
	}
	if err := lerJSON(w, r, &p); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.seg.config()
	ip := ipDe(r)
	if cfg.TemPIN() {
		if d := s.seg.tentativas.Espera(ip); d > 0 {
			respondeErro(w, http.StatusTooManyRequests, fmt.Errorf("muitas tentativas: espere %s", arredonda(d)))
			return
		}
		if !cfg.Confere(p.Atual) {
			s.seg.tentativas.Falhou(ip)
			respondeErro(w, http.StatusForbidden, errors.New("o PIN atual não confere"))
			return
		}
		s.seg.tentativas.Acertou(ip)
	} else if !ipLocal(r) {
		respondeErro(w, http.StatusForbidden, errors.New("defina o PIN no computador"))
		return
	}
	var err error
	msg := "PIN definido. A partir de agora o Prisma pede o PIN ao abrir."
	if p.Novo == "" {
		err = seguranca.RemoverPIN()
		msg = "PIN removido. O acesso pelo celular foi desligado."
	} else {
		err = seguranca.DefinirPIN(p.Novo)
	}
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	cfg = s.seg.config() // relê e encerra as sessões antigas
	if p.Novo == "" {
		s.desligaRede()
	} else {
		// quem trocou o PIN continua dentro
		http.SetCookie(w, &http.Cookie{
			Name: cookieSessao, Value: s.seg.sessoes.Nova(), Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	}
	responde(w, map[string]any{"ok": true, "mensagem": msg, "pin": cfg.TemPIN()})
}

// enderecosRede são os endereços pelos quais o celular acha este computador.
func enderecosRede(porta int) []string {
	type achado struct {
		url  string
		peso int // menor = mais provável de ser a rede de casa
	}
	var achados []achado
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagRunning == 0 || i.Flags&net.FlagLoopback != 0 || interfaceVirtual(i.Name) {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			pfx, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := pfx.Addr()
			if !ip.Is4() || !(ip.IsPrivate() || tailscale.Contains(ip)) {
				continue
			}
			b := ip.As4()
			peso := 3 // VPNs (Tailscale, ZeroTier) e o resto
			switch {
			case b[0] == 192 && b[1] == 168:
				peso = 0
			case b[0] == 10 && !strings.HasPrefix(i.Name, "zt") && !strings.HasPrefix(i.Name, "tailscale"):
				peso = 1
			case b[0] == 172:
				peso = 2
			}
			achados = append(achados, achado{fmt.Sprintf("http://%s:%d/", ip, porta), peso})
		}
	}
	sort.SliceStable(achados, func(a, b int) bool { return achados[a].peso < achados[b].peso })
	out := []string{}
	for _, a := range achados {
		out = append(out, a.url)
	}
	return out
}

// interfaceVirtual reconhece as redes internas de contêineres e máquinas
// virtuais, que o celular nunca alcança.
func interfaceVirtual(nome string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "lxc", "lxd", "cni", "flannel", "podman", "cali", "vEthernet"} {
		if strings.HasPrefix(nome, p) {
			return true
		}
	}
	return false
}

// estadoRede é o que a página mostra no painel "acessar pelo celular".
func (s *servidorWeb) estadoRede() map[string]any {
	s.seg.muRede.Lock()
	defer s.seg.muRede.Unlock()
	v := map[string]any{"ligado": s.seg.redeSrv != nil, "pin": s.seg.config().TemPIN(), "erro": s.seg.redeErro}
	if s.seg.redeSrv != nil {
		v["enderecos"] = enderecosRede(s.seg.portaRede)
	}
	return v
}

// ligaRede sobe o ouvinte da rede (0.0.0.0:8765) com as mesmas rotas.
func (s *servidorWeb) ligaRede() error {
	if !s.seg.config().TemPIN() {
		return errors.New("defina um PIN antes de liberar o acesso pela rede")
	}
	s.seg.muRede.Lock()
	defer s.seg.muRede.Unlock()
	if s.seg.redeSrv != nil {
		return nil
	}
	end := fmt.Sprintf(":%d", s.seg.portaRede)
	ouvinte, err := net.Listen("tcp", end)
	if err != nil {
		s.seg.redeErro = fmt.Sprintf("não deu para escutar na porta %d: %v", s.seg.portaRede, err)
		return errors.New(s.seg.redeErro)
	}
	s.seg.redeErro = ""
	srv := &http.Server{
		Handler:           s.rotas(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	s.seg.redeSrv = srv
	go srv.Serve(ouvinte)
	return nil
}

// desligaRede derruba o ouvinte da rede, se estiver no ar.
func (s *servidorWeb) desligaRede() {
	s.seg.muRede.Lock()
	srv := s.seg.redeSrv
	s.seg.redeSrv = nil
	s.seg.muRede.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}
}

// apiRede liga ou desliga o acesso pelo celular (e lembra a escolha em
// seguranca.json). Só do próprio computador: o celular não abre a porta.
func (s *servidorWeb) apiRede(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		responde(w, s.estadoRede())
		return
	}
	if !ipLocal(r) {
		respondeErro(w, http.StatusForbidden, errors.New("libere o acesso pela rede no próprio computador"))
		return
	}
	var p struct {
		Ligado bool `json:"ligado"`
	}
	if err := lerJSON(w, r, &p); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	if p.Ligado {
		if err := s.ligaRede(); err != nil {
			respondeErro(w, http.StatusBadRequest, err)
			return
		}
	} else {
		s.desligaRede()
	}
	if c, err := seguranca.Carrega(); err == nil && c.Rede != p.Ligado {
		c.Rede = p.Ligado
		seguranca.Salva(c)
	}
	responde(w, s.estadoRede())
}

// apiQR desenha em SVG o QR code de um dos endereços da rede (n = índice),
// para o celular abrir o Prisma apontando a câmera.
func (s *servidorWeb) apiQR(w http.ResponseWriter, r *http.Request) {
	ends, _ := s.estadoRede()["enderecos"].([]string)
	n := 0
	fmt.Sscan(r.URL.Query().Get("n"), &n)
	if n < 0 || n >= len(ends) {
		http.NotFound(w, r)
		return
	}
	code, err := qr.Encode(ends[n], qr.M)
	if err != nil {
		respondeErro(w, http.StatusInternalServerError, err)
		return
	}
	const borda = 4
	lado := code.Size + 2*borda
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="`, lado, lado)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+borda, y+borda)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}

// arquivos da web instalável: o manifesto e os ícones.
func servePNG(png []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(png)
	}
}

func (s *servidorWeb) rotasSeguranca(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", s.paginaDeLogin)
	mux.HandleFunc("POST /api/login", s.apiLogin)
	mux.HandleFunc("POST /api/trancar", s.apiTrancar)
	mux.HandleFunc("POST /api/pin", s.apiPIN)
	mux.HandleFunc("/api/rede", s.apiRede)
	mux.HandleFunc("GET /api/qr", s.apiQR)
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Write(manifestoWeb)
	})
	mux.HandleFunc("GET /icone-192.png", servePNG(packaging.Icone192))
	mux.HandleFunc("GET /icone-512.png", servePNG(packaging.Icone512))
	mux.HandleFunc("GET /icone-180.png", servePNG(packaging.Icone180))
	mux.HandleFunc("GET /favicon.ico", servePNG(packaging.Icone192))
}
