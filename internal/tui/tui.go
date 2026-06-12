// Package tui implementa a interface de terminal do Prisma (estilo btop):
// cabeçalho com ASCII art, menu de funcionalidades e telas interativas.
package tui

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type modo int

const (
	modoMenu modo = iota
	modoTela
	modoForm
	modoConfirma
)

// opcao é uma escolha de um campo-seletor: valor vai para o comando,
// rotulo é o que o usuário vê.
type opcao struct {
	valor, rotulo string
}

// campo é um campo de formulário. Com opcoes definidas, vira um seletor
// (←/→ navega nas opções) em vez de texto livre; obrigatorio marca com *
// e impede confirmar vazio.
type campo struct {
	rotulo      string
	dica        string // placeholder (campos de texto)
	obrigatorio bool
	opcoes      func() []opcao // nil = texto livre
}

// acao é um atalho de teclado dentro de uma tela. Pode executar um comando
// (executar) ou trocar os parâmetros de exibição da própria tela (params).
// Ações sem campos são aplicadas imediatamente, sem formulário.
// Com confirma=true, pede "s/n" antes de executar.
type acao struct {
	tecla    string
	rotulo   string
	campos   []campo
	confirma bool
	executar func(vals []string) (string, error)
	params   func(vals []string) []string
}

// tela é uma funcionalidade do menu: conteúdo (gerado pelos comandos da CLI)
// e ações disponíveis.
type tela struct {
	titulo   string
	resumo   string
	padrao   []string // parâmetros iniciais do conteúdo
	conteudo func(params []string) (string, error)
	acoes    []acao
}

// selecionavel é uma linha de tabela cujo primeiro campo é um ID numérico.
type selecionavel struct {
	linha int // índice da linha no conteúdo
	id    string
}

type model struct {
	conn   *sql.DB
	telas  []tela
	modo   modo
	cursor int // item do menu selecionado
	atual  int // tela aberta
	params [][]string

	vp     viewport.Model
	vpOK   bool
	linhas []string // conteúdo da tela, linha a linha (texto puro)
	seleta []selecionavel
	selPos int // posição na seleta (-1 = nenhuma)
	msg    string
	msgErr bool

	formAcao   *acao
	inputs     []textinput.Model
	formOpcoes [][]opcao // opções resolvidas por campo (vazio = texto livre)
	selIdx     []int     // opção escolhida em cada campo-seletor
	foco       int

	pendente     *acao // ação aguardando confirmação s/n
	pendenteVals []string

	largura, altura int
}

// Run abre a interface em tela cheia.
func Run(conn *sql.DB) error {
	telas := novasTelas(conn)
	m := model{
		conn:   conn,
		telas:  telas,
		params: make([][]string, len(telas)),
		selPos: -1,
	}
	for i := range telas {
		m.params[i] = telas[i].padrao
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.largura, m.altura = msg.Width, msg.Height
		m.ajustaViewport()
		return m, nil
	case tea.KeyMsg:
		switch m.modo {
		case modoMenu:
			return m.teclaMenu(msg)
		case modoTela:
			return m.teclaTela(msg)
		case modoForm:
			return m.teclaForm(msg)
		case modoConfirma:
			return m.teclaConfirma(msg)
		}
	}
	return m, nil
}

func (m *model) ajustaViewport() {
	altoCab := lipgloss.Height(cabecalho(m.largura))
	alto := m.altura - altoCab - 5 // título, mensagem e rodapé
	if alto < 3 {
		alto = 3
	}
	if !m.vpOK {
		m.vp = viewport.New(m.largura, alto)
		m.vpOK = true
	} else {
		m.vp.Width, m.vp.Height = m.largura, alto
	}
}

func (m model) teclaMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.telas)-1 {
			m.cursor++
		}
	case "enter", " ", "right", "l":
		m.abreTela(m.cursor)
	default:
		// atalho numérico: 1..9
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			i := int(s[0] - '1')
			if i < len(m.telas) {
				m.cursor = i
				m.abreTela(i)
			}
		}
	}
	return m, nil
}

func (m *model) abreTela(i int) {
	m.atual = i
	m.modo = modoTela
	m.msg = ""
	m.ajustaViewport()
	m.recarrega()
}

func (m *model) recarrega() {
	t := m.telas[m.atual]
	conteudo, err := t.conteudo(m.params[m.atual])
	if err != nil {
		conteudo = strings.TrimSpace(conteudo + "\nerro: " + err.Error())
	}
	m.linhas = strings.Split(strings.TrimRight(conteudo, "\n"), "\n")

	// linhas que começam com um número são selecionáveis (o número é o ID)
	m.seleta = nil
	for i, l := range m.linhas {
		campos := strings.Fields(l)
		if len(campos) == 0 {
			continue
		}
		if _, err := strconv.ParseInt(campos[0], 10, 64); err == nil {
			m.seleta = append(m.seleta, selecionavel{i, campos[0]})
		}
	}
	m.selPos = -1
	if len(m.seleta) > 0 {
		m.selPos = 0
	}
	m.renderConteudo()
	m.vp.GotoTop()
}

// renderConteudo monta o texto do viewport destacando a linha selecionada.
func (m *model) renderConteudo() {
	if m.selPos < 0 {
		m.vp.SetContent(strings.Join(m.linhas, "\n"))
		return
	}
	alvo := m.seleta[m.selPos].linha
	saida := make([]string, len(m.linhas))
	for i, l := range m.linhas {
		if i == alvo {
			saida[i] = corSelec.Render("▸ " + l)
		} else {
			saida[i] = "  " + l
		}
	}
	m.vp.SetContent(strings.Join(saida, "\n"))
}

// idSelecionado devolve o ID da linha sob o cursor, se houver.
func (m model) idSelecionado() string {
	if m.selPos >= 0 && m.selPos < len(m.seleta) {
		return m.seleta[m.selPos].id
	}
	return ""
}

func (m *model) moveSelecao(delta int) {
	if m.selPos < 0 {
		return
	}
	m.selPos += delta
	if m.selPos < 0 {
		m.selPos = 0
	}
	if m.selPos >= len(m.seleta) {
		m.selPos = len(m.seleta) - 1
	}
	m.renderConteudo()
	// mantém a linha selecionada visível
	alvo := m.seleta[m.selPos].linha
	if alvo < m.vp.YOffset {
		m.vp.SetYOffset(alvo)
	}
	if alvo >= m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(alvo - m.vp.Height + 1)
	}
}

func (m model) teclaTela(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tecla := msg.String()
	switch tecla {
	case "esc", "q":
		m.modo = modoMenu
		m.msg = ""
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.selPos >= 0 {
			m.moveSelecao(-1)
			return m, nil
		}
	case "down", "j":
		if m.selPos >= 0 {
			m.moveSelecao(1)
			return m, nil
		}
	}
	switch tecla {
	case "up", "down", "k", "j", "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	for i := range m.telas[m.atual].acoes {
		a := &m.telas[m.atual].acoes[i]
		if a.tecla != tecla {
			continue
		}
		if len(a.campos) == 0 {
			m.aplicaAcao(a, nil)
			return m, nil
		}
		m.abreForm(a)
		return m, textinput.Blink
	}
	return m, nil
}

func (m *model) abreForm(a *acao) {
	m.modo = modoForm
	m.formAcao = a
	m.foco = 0
	m.msg = ""
	m.inputs = make([]textinput.Model, len(a.campos))
	m.formOpcoes = make([][]opcao, len(a.campos))
	m.selIdx = make([]int, len(a.campos))
	for i, c := range a.campos {
		if c.opcoes != nil {
			m.formOpcoes[i] = c.opcoes()
		}
		ti := textinput.New()
		ti.Placeholder = c.dica
		ti.CharLimit = 80
		ti.Width = 42
		if i == 0 {
			ti.Focus()
		}
		m.inputs[i] = ti
	}
	// pré-preenche o campo "id" com a linha selecionada na tabela
	if a.campos[0].rotulo == "id" {
		if id := m.idSelecionado(); id != "" {
			m.inputs[0].SetValue(id)
			m.inputs[0].CursorEnd()
		}
	}
}

// seletor diz se o campo i é um seletor de opções (e não texto livre).
func (m model) seletor(i int) bool { return len(m.formOpcoes[i]) > 0 }

// valoresForm coleta o valor de cada campo: a opção escolhida nos seletores,
// o texto digitado nos demais.
func (m model) valoresForm() []string {
	vals := make([]string, len(m.inputs))
	for i := range m.inputs {
		if m.seletor(i) {
			vals[i] = m.formOpcoes[i][m.selIdx[i]].valor
		} else {
			vals[i] = strings.TrimSpace(m.inputs[i].Value())
		}
	}
	return vals
}

func (m *model) aplicaAcao(a *acao, vals []string) {
	if a.confirma {
		m.pendente, m.pendenteVals = a, vals
		m.modo = modoConfirma
		return
	}
	m.executaAcao(a, vals)
}

func (m *model) executaAcao(a *acao, vals []string) {
	m.modo = modoTela
	if a.params != nil {
		m.params[m.atual] = a.params(vals)
		m.msg = ""
		m.recarrega()
		return
	}
	saida, err := a.executar(vals)
	if err != nil {
		m.msg, m.msgErr = "erro: "+err.Error(), true
		return
	}
	m.msg, m.msgErr = strings.TrimSpace(saida), false
	m.recarrega()
}

func (m model) teclaConfirma(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	a, vals := m.pendente, m.pendenteVals
	m.pendente, m.pendenteVals = nil, nil
	if strings.ToLower(msg.String()) == "s" {
		m.executaAcao(a, vals)
	} else {
		m.modo = modoTela
		m.msg, m.msgErr = "cancelado", false
	}
	return m, nil
}

func (m model) teclaForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.modo = modoTela
		m.msg = ""
		return m, nil
	case "left", "right", " ":
		if m.seletor(m.foco) {
			n := len(m.formOpcoes[m.foco])
			if msg.String() == "left" {
				m.selIdx[m.foco] = (m.selIdx[m.foco] - 1 + n) % n
			} else {
				m.selIdx[m.foco] = (m.selIdx[m.foco] + 1) % n
			}
			return m, nil
		}
	case "tab", "down":
		m.foco = (m.foco + 1) % len(m.inputs)
		return m.focaCampo()
	case "shift+tab", "up":
		m.foco = (m.foco - 1 + len(m.inputs)) % len(m.inputs)
		return m.focaCampo()
	case "enter":
		if m.foco < len(m.inputs)-1 {
			m.foco++
			return m.focaCampo()
		}
		vals := m.valoresForm()
		// valida os obrigatórios antes de confirmar
		for i, c := range m.formAcao.campos {
			if c.obrigatorio && strings.TrimSpace(vals[i]) == "" {
				m.msg = fmt.Sprintf("o campo %q é obrigatório", c.rotulo)
				m.foco = i
				novo, cmd := m.focaCampo()
				return novo, cmd
			}
		}
		m.aplicaAcao(m.formAcao, vals)
		return m, nil
	}
	if m.seletor(m.foco) {
		return m, nil // seletores não recebem texto
	}
	var cmd tea.Cmd
	m.inputs[m.foco], cmd = m.inputs[m.foco].Update(msg)
	return m, cmd
}

func (m model) focaCampo() (tea.Model, tea.Cmd) {
	for i := range m.inputs {
		if i == m.foco {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return m, textinput.Blink
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(cabecalho(m.largura))
	b.WriteString("\n\n")

	switch m.modo {
	case modoMenu:
		b.WriteString(m.viewMenu())
	case modoTela, modoConfirma:
		b.WriteString(m.viewTela())
	case modoForm:
		b.WriteString(m.viewForm())
	}
	return b.String()
}

func (m model) viewMenu() string {
	var b strings.Builder
	for i, t := range m.telas {
		linha := fmt.Sprintf(" %d  %-14s %s", i+1, t.titulo, corApagada.Render(t.resumo))
		if i == m.cursor {
			linha = corSelec.Render(fmt.Sprintf(" %d  %-14s", i+1, t.titulo)) + " " + corApagada.Render(t.resumo)
			linha = " ▸" + linha
		} else {
			linha = "  " + linha
		}
		b.WriteString(linha + "\n")
	}
	b.WriteString("\n" + corApagada.Render("  ↑/↓ navegar · enter abrir · 1-9 atalho · q sair"))
	return b.String()
}

func (m model) viewTela() string {
	t := m.telas[m.atual]
	var b strings.Builder
	b.WriteString(corTitulo.Render(" " + t.titulo))
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n")

	if m.modo == modoConfirma {
		b.WriteString(corErro.Render(fmt.Sprintf(" Confirma %q? (s/n)", m.pendente.rotulo)))
		b.WriteString("\n")
		b.WriteString(corApagada.Render(" s confirmar · qualquer outra tecla cancela"))
		return b.String()
	}

	if m.msg != "" {
		estilo := corOK
		if m.msgErr {
			estilo = corErro
		}
		b.WriteString(estilo.Render(" " + strings.ReplaceAll(m.msg, "\n", " · ")))
	}
	b.WriteString("\n")

	teclas := make([]string, 0, len(t.acoes)+2)
	for _, a := range t.acoes {
		teclas = append(teclas, a.tecla+" "+a.rotulo)
	}
	if m.selPos >= 0 {
		teclas = append(teclas, "↑/↓ selecionar")
	} else {
		teclas = append(teclas, "↑/↓ rolar")
	}
	teclas = append(teclas, "esc voltar")
	b.WriteString(corApagada.Render(" " + strings.Join(teclas, " · ")))
	return b.String()
}

func (m model) viewForm() string {
	a := m.formAcao
	var b strings.Builder
	b.WriteString(corTitulo.Render(" " + m.telas[m.atual].titulo + " › " + a.rotulo))
	b.WriteString("\n\n")
	temSeletor := false
	for i, c := range a.campos {
		marca := "  "
		if i == m.foco {
			marca = corPrisma.Render("▸ ")
		}
		rotulo := fmt.Sprintf("%-13s", c.rotulo)
		if c.obrigatorio {
			rotulo = fmt.Sprintf("%-13s", c.rotulo+"*")
		}
		var valor string
		if m.seletor(i) {
			temSeletor = true
			op := m.formOpcoes[i][m.selIdx[i]]
			if i == m.foco {
				valor = corPrisma.Render("◂ ") + corSelec.Render(" "+op.rotulo+" ") + corPrisma.Render(" ▸")
			} else {
				valor = "  " + op.rotulo
			}
		} else {
			valor = m.inputs[i].View()
		}
		b.WriteString(fmt.Sprintf(" %s%s %s\n", marca, rotulo, valor))
	}
	if m.msg != "" {
		b.WriteString("\n " + corErro.Render(m.msg))
	}
	dicas := " enter confirmar · tab próximo campo"
	if temSeletor {
		dicas += " · ←/→ escolher opção"
	}
	dicas += " · esc cancelar · * obrigatório"
	b.WriteString("\n" + corApagada.Render(dicas))
	return b.String()
}

// captura redireciona o stdout durante a execução de um comando da CLI e
// devolve o texto impresso — assim a TUI reaproveita toda a lógica existente.
func captura(f func() error) (string, error) {
	antigo := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	errF := f()
	w.Close()
	os.Stdout = antigo
	var b strings.Builder
	io.Copy(&b, r)
	r.Close()
	return b.String(), errF
}

// par devolve ["--flag", valor] ou nada, se o valor estiver vazio.
func par(flag, val string) []string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	return []string{flag, strings.TrimSpace(val)}
}

func sim(val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	return v == "s" || v == "sim" || v == "y"
}
