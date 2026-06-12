package tui

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// A interface web (prisma --web) reaproveita as mesmas telas da TUI: o
// navegador é só outro renderizador do conteúdo capturado da CLI. Uma página
// estática embutida no binário consome a API JSON abaixo — sem frameworks.

//go:embed web.html
var paginaWeb []byte

// servidorWeb expõe as telas numa API JSON. O mutex serializa as execuções:
// captura() troca o os.Stdout do processo inteiro, então dois comandos não
// podem rodar ao mesmo tempo.
type servidorWeb struct {
	telas []tela
	mu    sync.Mutex
}

// RunWeb sobe um servidor local e abre a interface no navegador. Escuta só
// em 127.0.0.1 — as finanças não ficam expostas na rede. Opções: --porta N
// e --sem-abrir (não chama o navegador).
func RunWeb(conn *sql.DB, args []string) error {
	porta := 7747 // P-R-I-S num teclado de telefone
	abrir := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--porta":
			i++
			if i == len(args) {
				return fmt.Errorf("--porta exige um número")
			}
			p, err := strconv.Atoi(args[i])
			if err != nil || p < 1 || p > 65535 {
				return fmt.Errorf("porta inválida: %q", args[i])
			}
			porta = p
		case "--sem-abrir":
			abrir = false
		default:
			return fmt.Errorf("opção desconhecida: %q (use --porta N ou --sem-abrir)", args[i])
		}
	}

	s := &servidorWeb{telas: novasTelas(conn)}
	endereco := fmt.Sprintf("127.0.0.1:%d", porta)
	ouvinte, err := net.Listen("tcp", endereco)
	if err != nil {
		return fmt.Errorf("não deu para escutar em %s: %w", endereco, err)
	}
	url := "http://" + endereco
	fmt.Printf("prisma web em %s — ctrl+c encerra\n", url)
	if abrir {
		abreNavegador(url)
	}
	return http.Serve(ouvinte, s.rotas())
}

func (s *servidorWeb) rotas() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.pagina)
	mux.HandleFunc("/api/telas", s.apiTelas)
	mux.HandleFunc("/api/conteudo", s.apiConteudo)
	mux.HandleFunc("/api/form", s.apiForm)
	mux.HandleFunc("/api/executar", s.apiExecutar)
	return mux
}

func (s *servidorWeb) pagina(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(paginaWeb)
}

func responde(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func respondeErro(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"erro": err.Error()})
}

// apiTelas devolve o menu: telas, parâmetros iniciais e ações (sem os
// campos, que são resolvidos na hora de abrir o formulário).
func (s *servidorWeb) apiTelas(w http.ResponseWriter, r *http.Request) {
	type acaoJSON struct {
		Tecla    string `json:"tecla"`
		Rotulo   string `json:"rotulo"`
		Confirma bool   `json:"confirma"`
		TemForm  bool   `json:"temForm"`
	}
	type telaJSON struct {
		Titulo string     `json:"titulo"`
		Resumo string     `json:"resumo"`
		Padrao []string   `json:"padrao"`
		Acoes  []acaoJSON `json:"acoes"`
	}
	saida := make([]telaJSON, len(s.telas))
	for i, t := range s.telas {
		tj := telaJSON{Titulo: t.titulo, Resumo: t.resumo, Padrao: t.padrao}
		for _, a := range t.acoes {
			tj.Acoes = append(tj.Acoes, acaoJSON{a.tecla, a.rotulo, a.confirma, len(a.campos) > 0})
		}
		saida[i] = tj
	}
	responde(w, saida)
}

// apiConteudo gera o conteúdo de uma tela. A página guarda os parâmetros de
// exibição de cada tela e os manda repetindo a query "p" — o servidor não
// tem estado de navegação.
func (s *servidorWeb) apiConteudo(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(r.URL.Query().Get("tela"))
	if err != nil || i < 0 || i >= len(s.telas) {
		respondeErro(w, http.StatusBadRequest, fmt.Errorf("tela inválida"))
		return
	}
	s.mu.Lock()
	texto, err := s.telas[i].conteudo(r.URL.Query()["p"])
	s.mu.Unlock()
	if err != nil {
		// como na TUI: o erro vira parte do conteúdo exibido
		texto = strings.TrimSpace(texto + "\nerro: " + err.Error())
	}
	responde(w, map[string]string{"texto": texto})
}

// apiForm devolve os campos de uma ação, com os seletores resolvidos na
// hora — uma conta recém-criada já aparece nas opções.
func (s *servidorWeb) apiForm(w http.ResponseWriter, r *http.Request) {
	a, err := s.acaoDe(r.URL.Query().Get("tela"), r.URL.Query().Get("acao"))
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	type opcaoJSON struct {
		Valor  string `json:"valor"`
		Rotulo string `json:"rotulo"`
	}
	type campoJSON struct {
		Rotulo      string      `json:"rotulo"`
		Dica        string      `json:"dica"`
		Obrigatorio bool        `json:"obrigatorio"`
		Opcoes      []opcaoJSON `json:"opcoes,omitempty"`
	}
	campos := make([]campoJSON, len(a.campos))
	for i, c := range a.campos {
		cj := campoJSON{Rotulo: c.rotulo, Dica: c.dica, Obrigatorio: c.obrigatorio}
		if c.opcoes != nil {
			for _, o := range c.opcoes() {
				cj.Opcoes = append(cj.Opcoes, opcaoJSON{o.valor, o.rotulo})
			}
		}
		campos[i] = cj
	}
	responde(w, map[string]any{"campos": campos})
}

// apiExecutar dispara uma ação com os valores do formulário. Ações de
// exibição devolvem os novos parâmetros da tela ("params"); as demais
// executam o comando e devolvem a mensagem de resultado ("msg").
func (s *servidorWeb) apiExecutar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondeErro(w, http.StatusMethodNotAllowed, fmt.Errorf("use POST"))
		return
	}
	var req struct {
		Tela int      `json:"tela"`
		Acao int      `json:"acao"`
		Vals []string `json:"vals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondeErro(w, http.StatusBadRequest, fmt.Errorf("corpo inválido: %v", err))
		return
	}
	a, err := s.acaoDe(strconv.Itoa(req.Tela), strconv.Itoa(req.Acao))
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Vals) != len(a.campos) {
		respondeErro(w, http.StatusBadRequest, fmt.Errorf("a ação %q espera %d campos", a.rotulo, len(a.campos)))
		return
	}
	for i, c := range a.campos {
		if c.obrigatorio && strings.TrimSpace(req.Vals[i]) == "" {
			respondeErro(w, http.StatusBadRequest, fmt.Errorf("o campo %q é obrigatório", c.rotulo))
			return
		}
	}
	if a.params != nil {
		responde(w, map[string]any{"params": a.params(req.Vals)})
		return
	}
	s.mu.Lock()
	saida, err := a.executar(req.Vals)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusUnprocessableEntity, err)
		return
	}
	responde(w, map[string]any{"msg": strings.TrimSpace(saida)})
}

// acaoDe localiza uma ação a partir dos índices (em texto) de tela e ação.
func (s *servidorWeb) acaoDe(telaStr, acaoStr string) (*acao, error) {
	i, err1 := strconv.Atoi(telaStr)
	j, err2 := strconv.Atoi(acaoStr)
	if err1 != nil || err2 != nil || i < 0 || i >= len(s.telas) || j < 0 || j >= len(s.telas[i].acoes) {
		return nil, fmt.Errorf("tela ou ação inválida")
	}
	return &s.telas[i].acoes[j], nil
}

// abreNavegador tenta abrir a URL no navegador padrão do sistema.
func abreNavegador(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("abra %s no navegador (não consegui abrir sozinho: %v)\n", url, err)
	}
}
