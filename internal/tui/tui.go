// Package tui implementa a interface de terminal do Prisma (estilo btop):
// cabeçalho com ASCII art, menu de funcionalidades e telas interativas.
// As mesmas telas alimentam a interface web (prisma --web), em web.go.
package tui

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"prisma/internal/update"

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
	opcoes      func() []opcao  // nil = texto livre
	sugestoes   func() []string // texto livre com sugestões navegáveis por ←/→ (ex.: categorias)
	// auto, quando devolve um texto não-vazio (em função dos valores atuais do
	// formulário), trava o campo como somente-leitura exibindo esse texto — útil
	// para sugerir um valor calculado (ex.: o vencimento da fatura ao escolher um
	// cartão). Campo travado não recebe foco nem vira argumento do comando.
	auto func(vals []string) string
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
	// carregar devolve os valores atuais dos campos editáveis (campos[1:], na
	// mesma ordem) de um registro, para o formulário de edição abrir preenchido.
	carregar func(id string) ([]string, error)
}

// tela é uma funcionalidade do menu: conteúdo (gerado pelos comandos da CLI)
// e ações disponíveis.
type tela struct {
	titulo   string
	resumo   string
	padrao   []string // parâmetros iniciais do conteúdo
	conteudo func(params []string) (string, error)
	acoes    []acao
	// listaMensal liga a navegação rápida por mês (←/→) e o atalho de tipo
	// (t alterna pagar/receber/todos) sobre uma listagem com filtro --mes/--tipo.
	listaMensal bool
}

// selecionavel é uma linha de tabela cujo primeiro campo é um ID numérico.
type selecionavel struct {
	linha int // índice da linha no conteúdo
	id    string
}

type model struct {
	conn  *sql.DB
	telas []tela
	// banner: selo ("CORP"/"ANALYTICS"/""), subtítulo e cor do selo no cabeçalho
	selo    string
	seloSub string
	seloCor lipgloss.Style
	modo    modo
	cursor  int // item do menu selecionado
	atual   int // tela aberta
	params  [][]string

	prefixo    string // primeiro dígito de um atalho de dois dígitos (ex.: "1" de "12")
	prefixoGen int    // invalida ticks de prefixo antigos

	vp     viewport.Model
	vpOK   bool
	linhas []string // conteúdo da tela, linha a linha (texto puro)
	seleta []selecionavel
	selPos int // posição na seleta (-1 = nenhuma)
	msg    string
	msgErr bool
	msgGen int // invalida ticks de auto-dismiss antigos

	formAcao   *acao
	inputs     []textinput.Model
	formOpcoes [][]opcao  // opções resolvidas por campo (vazio = texto livre)
	selIdx     []int      // opção escolhida em cada campo-seletor
	sugest     [][]string // sugestões de campos-combo (nil = não é combo)
	sugestIdx  []int      // posição atual na navegação de sugestões (-1 = nenhuma)
	foco       int

	pendente     *acao // ação aguardando confirmação s/n
	pendenteVals []string

	aviso string // aviso de versão nova (vazio = nenhum)

	largura, altura int
}

// Run abre a interface em tela cheia. modoEmpresa (true em `prisma --empresa`)
// troca o banner pro selo CORP e acrescenta as telas de sócios/capital/
// imposto/investimento/lucro.
func Run(conn *sql.DB, modoEmpresa bool) error {
	telas := novasTelas(conn, modoEmpresa)
	selo, sub := "", "— finanças pessoais"
	if modoEmpresa {
		selo, sub = "CORP", "— finanças empresariais"
	}
	return roda(conn, telas, selo, sub, corCorp)
}

// RunAnalytics abre o módulo Prisma Analytics (`prisma --analytics`): banner com
// o selo ANALYTICS e telas exclusivas de análise (somente leitura, sem CRUD).
func RunAnalytics(conn *sql.DB) error {
	return roda(conn, novasTelasAnalytics(conn), "ANALYTICS", "— análise financeira (somente leitura)", corAnalytics)
}

// roda monta o modelo com o conjunto de telas e o banner informados e sobe a TUI.
func roda(conn *sql.DB, telas []tela, selo, sub string, corSelo lipgloss.Style) error {
	aviso, _ := update.Aviso()
	m := model{
		conn:    conn,
		telas:   telas,
		selo:    selo,
		seloSub: sub,
		seloCor: corSelo,
		params:  make([][]string, len(telas)),
		selPos:  -1,
		aviso:   aviso,
	}
	for i := range telas {
		m.params[i] = telas[i].padrao
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return nil }

// flushPrefixoMsg fecha a janela de espera do segundo dígito de um atalho.
type flushPrefixoMsg int

// limpaMsgMsg apaga a mensagem verde/vermelha depois de alguns segundos.
type limpaMsgMsg int

// duracaoMsg é quanto tempo a mensagem de status fica visível.
const duracaoMsg = 10 * time.Second

// defineMsg registra a mensagem de status e devolve o comando que a apaga
// após duracaoMsg (msgGen invalida ticks de mensagens já substituídas).
func (m *model) defineMsg(texto string, erro bool) tea.Cmd {
	m.msg, m.msgErr = strings.TrimSpace(texto), erro
	m.msgGen++
	if m.msg == "" {
		return nil
	}
	gen := m.msgGen
	return tea.Tick(duracaoMsg, func(time.Time) tea.Msg { return limpaMsgMsg(gen) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.largura, m.altura = msg.Width, msg.Height
		m.ajustaViewport()
		return m, nil
	case limpaMsgMsg:
		if int(msg) == m.msgGen {
			m.msg = ""
		}
		return m, nil
	case flushPrefixoMsg:
		// passou o tempo sem segundo dígito: abre a tela do dígito sozinho
		if int(msg) == m.prefixoGen && m.prefixo != "" && m.modo == modoMenu {
			n := int(m.prefixo[0] - '0')
			m.prefixo = ""
			if n >= 1 && n <= len(m.telas) {
				m.cursor = n - 1
				m.abreTela(n - 1)
			}
		}
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
	altoCab := lipgloss.Height(cabecalhoSelo(m.largura, m.selo, m.seloSub, m.seloCor))
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
	s := msg.String()
	ehDigito := len(s) == 1 && s[0] >= '0' && s[0] <= '9'
	if !ehDigito {
		m.prefixo = "" // qualquer tecla não-numérica cancela um prefixo pendente
	}
	switch s {
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
		if ehDigito {
			return m.teclaDigito(s[0])
		}
	}
	return m, nil
}

// teclaDigito trata o atalho numérico do menu, com suporte a dois dígitos
// (10..14): se o dígito pode iniciar um número maior, espera o segundo por um
// instante; senão, abre a tela na hora.
func (m model) teclaDigito(d byte) (tea.Model, tea.Cmd) {
	abre := func(n int) {
		if n >= 1 && n <= len(m.telas) {
			m.cursor = n - 1
			m.abreTela(n - 1)
		}
	}
	if m.prefixo != "" {
		// segundo dígito: forma o número de dois dígitos (ex.: "1" e "2" = 12)
		primeiro := int(m.prefixo[0] - '0')
		n := primeiro*10 + int(d-'0')
		m.prefixo = ""
		if n >= 1 && n <= len(m.telas) {
			abre(n)
		} else {
			abre(primeiro) // combinação inválida: abre a tela do primeiro dígito
		}
		return m, nil
	}
	n := int(d - '0')
	if n >= 1 && n*10 <= len(m.telas) {
		// pode ser o começo de um número de dois dígitos: aguarda o segundo
		m.prefixo = string(d)
		m.prefixoGen++
		gen := m.prefixoGen
		return m, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return flushPrefixoMsg(gen) })
	}
	abre(n)
	return m, nil
}

func (m *model) abreTela(i int) {
	m.atual = i
	m.modo = modoTela
	m.msg = ""
	// abre sempre na visão padrão da tela (ex.: a lista de cartões, não uma
	// fatura aberta numa visita anterior, cujos params teriam ficado guardados)
	m.params[i] = m.telas[i].padrao
	m.ajustaViewport()
	m.recarrega()
}

// mesmosParams diz se dois conjuntos de parâmetros de tela são iguais.
func mesmosParams(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

// renderConteudo monta o texto do viewport destacando a linha selecionada e
// aplicando o esquema de cores nas demais (a selecionada fica só com o fundo
// de seleção, sem cores internas brigando com ele).
func (m *model) renderConteudo() {
	if m.selPos < 0 {
		saida := make([]string, len(m.linhas))
		for i, l := range m.linhas {
			saida[i] = colorir(l)
		}
		m.vp.SetContent(strings.Join(saida, "\n"))
		return
	}
	alvo := m.seleta[m.selPos].linha
	saida := make([]string, len(m.linhas))
	for i, l := range m.linhas {
		if i == alvo {
			saida[i] = corSelec.Render("▸ " + l)
		} else {
			saida[i] = "  " + colorir(l)
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
	// na primeira linha selecionável, volta ao topo para o cabeçalho da tabela
	// (e qualquer preâmbulo acima dela) reaparecer
	if m.selPos == 0 {
		m.vp.GotoTop()
		return
	}
	// na última, vai até o fim para revelar o que vem depois da tabela
	// (totais, "pendente a pagar/receber" etc.), que de outro modo ficaria oculto
	if m.selPos == len(m.seleta)-1 {
		m.vp.GotoBottom()
		return
	}
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
		// numa visão "aprofundada" (ex.: a fatura de um cartão), o primeiro esc
		// volta para a visão padrão da tela (a lista); só então sai para o menu
		if !mesmosParams(m.params[m.atual], m.telas[m.atual].padrao) {
			m.params[m.atual] = m.telas[m.atual].padrao
			m.msg = ""
			m.recarrega()
			return m, nil
		}
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
	// listagens mensais: ←/→ muda o mês, t alterna pagar/receber/todos
	if m.telas[m.atual].listaMensal {
		switch tecla {
		case "left":
			m.ajustaMes(-1)
			return m, nil
		case "right":
			m.ajustaMes(1)
			return m, nil
		case "t":
			m.cicleTipo()
			return m, nil
		}
	}
	for i := range m.telas[m.atual].acoes {
		a := &m.telas[m.atual].acoes[i]
		if a.tecla != tecla {
			continue
		}
		if len(a.campos) == 0 {
			cmd := m.aplicaAcao(a, nil)
			return m, cmd
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
	m.sugest = make([][]string, len(a.campos))
	m.sugestIdx = make([]int, len(a.campos))
	for i, c := range a.campos {
		if c.opcoes != nil {
			m.formOpcoes[i] = c.opcoes()
		}
		if c.sugestoes != nil {
			m.sugest[i] = c.sugestoes()
		}
		m.sugestIdx[i] = -1
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
			// e, na edição, traz os valores atuais do registro do banco
			m.preencheEdicao(a, id)
		}
	}
}

// preencheEdicao carrega os valores atuais do registro e os coloca nos campos
// editáveis (campos[1:]), para o formulário de edição abrir já preenchido.
func (m *model) preencheEdicao(a *acao, id string) {
	if a.carregar == nil {
		return
	}
	vals, err := a.carregar(id)
	if err != nil {
		m.msg, m.msgErr = "erro ao carregar: "+err.Error(), true
		return
	}
	for i, v := range vals {
		campo := i + 1 // campos[0] é o id
		if campo >= len(m.inputs) {
			break
		}
		if m.seletor(campo) {
			for j, op := range m.formOpcoes[campo] {
				if op.valor == v {
					m.selIdx[campo] = j
					break
				}
			}
		} else {
			m.inputs[campo].SetValue(v)
			m.inputs[campo].CursorEnd()
		}
	}
}

// combo diz se o campo i é texto livre com sugestões navegáveis.
func (m model) combo(i int) bool { return len(m.sugest[i]) > 0 }

// navegaSugestao troca o valor do campo-combo pela sugestão seguinte/anterior.
func (m *model) navegaSugestao(i, delta int) {
	n := len(m.sugest[i])
	if n == 0 {
		return
	}
	m.sugestIdx[i] = (m.sugestIdx[i] + delta + n) % n
	m.inputs[i].SetValue(m.sugest[i][m.sugestIdx[i]])
	m.inputs[i].CursorEnd()
}

// seletor diz se o campo i é um seletor de opções (e não texto livre).
func (m model) seletor(i int) bool { return len(m.formOpcoes[i]) > 0 }

// rawValores coleta o valor cru de cada campo (sem aplicar visibilidade): a
// opção escolhida nos seletores, o texto digitado nos demais.
func (m model) rawValores() []string {
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

// autoTexto devolve o texto do campo travado i (vazio = campo normal/editável).
func (m model) autoTexto(i int) string {
	c := m.formAcao.campos[i]
	if c.auto == nil {
		return ""
	}
	return c.auto(m.rawValores())
}

// travado diz se o campo i está em modo somente-leitura (tem texto automático).
func (m model) travado(i int) bool { return m.autoTexto(i) != "" }

// proxVisivel devolve o índice do próximo campo editável a partir de `de` na
// direção `dir` (+1/-1), pulando os travados; volta `de` se não houver outro.
func (m model) proxVisivel(de, dir int) int {
	n := len(m.inputs)
	i := de
	for k := 0; k < n; k++ {
		i = (i + dir + n) % n
		if !m.travado(i) {
			return i
		}
	}
	return de
}

// valoresForm coleta os valores do formulário, zerando os campos travados (o
// valor exibido é só sugestão e não deve virar argumento do comando).
func (m model) valoresForm() []string {
	vals := m.rawValores()
	for i := range vals {
		if m.travado(i) {
			vals[i] = ""
		}
	}
	return vals
}

func (m *model) aplicaAcao(a *acao, vals []string) tea.Cmd {
	if a.confirma {
		m.pendente, m.pendenteVals = a, vals
		m.modo = modoConfirma
		return nil
	}
	return m.executaAcao(a, vals)
}

func (m *model) executaAcao(a *acao, vals []string) tea.Cmd {
	m.modo = modoTela
	if a.params != nil {
		m.params[m.atual] = a.params(vals)
		m.msg = ""
		m.recarrega()
		return nil
	}
	saida, err := a.executar(vals)
	if err != nil {
		return m.defineMsg("erro: "+err.Error(), true)
	}
	cmd := m.defineMsg(saida, false)
	m.recarrega()
	return cmd
}

func (m model) teclaConfirma(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	a, vals := m.pendente, m.pendenteVals
	m.pendente, m.pendenteVals = nil, nil
	var cmd tea.Cmd
	if strings.ToLower(msg.String()) == "s" {
		cmd = m.executaAcao(a, vals)
	} else {
		m.modo = modoTela
		cmd = m.defineMsg("cancelado", false)
	}
	return m, cmd
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
		// campo-combo (categoria): ←/→ navega nas existentes; espaço é digitado
		if m.combo(m.foco) && msg.String() != " " {
			if msg.String() == "left" {
				m.navegaSugestao(m.foco, -1)
			} else {
				m.navegaSugestao(m.foco, 1)
			}
			return m, nil
		}
	case "tab", "down":
		m.foco = m.proxVisivel(m.foco, 1)
		return m.focaCampo()
	case "shift+tab", "up":
		m.foco = m.proxVisivel(m.foco, -1)
		return m.focaCampo()
	case "enter":
		if prox := m.proxVisivel(m.foco, 1); prox > m.foco {
			m.foco = prox
			return m.focaCampo()
		}
		vals := m.valoresForm()
		// valida os obrigatórios antes de confirmar (campos travados não contam)
		for i, c := range m.formAcao.campos {
			if m.travado(i) {
				continue
			}
			if c.obrigatorio && strings.TrimSpace(vals[i]) == "" {
				m.foco = i
				msgCmd := m.defineMsg(fmt.Sprintf("o campo %q é obrigatório", c.rotulo), true)
				novo, blink := m.focaCampo()
				return novo, tea.Batch(msgCmd, blink)
			}
		}
		// repetir e parcelar são mutuamente exclusivos
		if aviso := validaRepetirParcelas(m.formAcao.campos, vals); aviso != "" {
			return m, m.defineMsg(aviso, true)
		}
		if aviso := validaRecebePagamento(m.formAcao.campos, vals); aviso != "" {
			return m, m.defineMsg(aviso, true)
		}
		cmd := m.aplicaAcao(m.formAcao, vals)
		return m, cmd
	}
	if m.seletor(m.foco) {
		return m, nil // seletores não recebem texto
	}
	if m.combo(m.foco) {
		m.sugestIdx[m.foco] = -1 // digitar reinicia a navegação de sugestões
	}
	var cmd tea.Cmd
	m.inputs[m.foco], cmd = m.inputs[m.foco].Update(msg)
	return m, cmd
}

// valorCampo devolve o valor coletado do campo de rótulo `rotulo`, se existir.
func valorCampo(campos []campo, vals []string, rotulo string) (string, bool) {
	for i, c := range campos {
		if c.rotulo == rotulo && i < len(vals) {
			return vals[i], true
		}
	}
	return "", false
}

// validaRepetirParcelas barra o uso simultâneo de repetir e parcelas no
// formulário (a CLI também recusa, mas aqui evitamos a viagem).
func validaRepetirParcelas(campos []campo, vals []string) string {
	rep, ok1 := valorCampo(campos, vals, "repetir")
	par, ok2 := valorCampo(campos, vals, "parcelas")
	if ok1 && ok2 && maiorQue1(rep) && maiorQue1(par) {
		return "use repetir OU parcelas, não os dois (repetir repete o valor; parcelas divide o total)"
	}
	return ""
}

// validaRecebePagamento barra "outros do grupo te pagam?" sem um grupo
// selecionado: a opção só faz sentido dividindo uma despesa com um grupo.
func validaRecebePagamento(campos []campo, vals []string) string {
	recebe, ok1 := valorCampo(campos, vals, "outros do grupo te pagam?")
	grupo, ok2 := valorCampo(campos, vals, "grupo")
	if ok1 && sim(recebe) && (!ok2 || grupo == "") {
		return "selecione um grupo para usar \"outros do grupo te pagam?\""
	}
	return ""
}

func maiorQue1(s string) bool {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && n > 1
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
	b.WriteString(cabecalhoSelo(m.largura, m.selo, m.seloSub, m.seloCor))
	b.WriteString("\n")
	if m.aviso != "" {
		b.WriteString(corAmar.Render(" ↑ "+m.aviso) + "\n")
	}
	b.WriteString("\n")

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
	b.WriteString("\n" + corApagada.Render(fmt.Sprintf("  ↑/↓ navegar · enter abrir · 1-%d atalho · q sair", len(m.telas))))
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

	teclas := make([]string, 0, len(t.acoes)+3)
	for _, a := range t.acoes {
		teclas = append(teclas, a.tecla+" "+a.rotulo)
	}
	if t.listaMensal {
		teclas = append(teclas, "←/→ mês", "t pagar/receber")
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
		// campo travado: mostra o rótulo e o valor sugerido (somente leitura)
		if texto := m.autoTexto(i); texto != "" {
			rotulo := fmt.Sprintf("%-13s", c.rotulo)
			b.WriteString(fmt.Sprintf("   %s %s\n", rotulo, corApagada.Render("→ "+texto)))
			continue
		}
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
			if m.combo(i) && i == m.foco {
				valor += corApagada.Render("  ←/→ existentes")
			}
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

// getArg devolve o valor da flag `nome` em params (ex.: "--mes"), ou "" se ausente.
func getArg(params []string, nome string) string {
	for i := 0; i+1 < len(params); i++ {
		if params[i] == nome {
			return params[i+1]
		}
	}
	return ""
}

// setArg devolve params com a flag `nome` valendo `valor`; valor vazio a remove.
func setArg(params []string, nome, valor string) []string {
	out := make([]string, 0, len(params)+2)
	for i := 0; i < len(params); i++ {
		if params[i] == nome && i+1 < len(params) {
			i++ // pula o valor antigo
			continue
		}
		out = append(out, params[i])
	}
	if valor != "" {
		out = append(out, nome, valor)
	}
	return out
}

// ajustaMes avança/volta o filtro --mes da tela atual em `delta` meses e recarrega.
func (m *model) ajustaMes(delta int) {
	base, err := time.Parse("2006-01", getArg(m.params[m.atual], "--mes"))
	if err != nil {
		base = time.Now() // sem mês no filtro: parte do atual
	}
	novo := base.AddDate(0, delta, 0).Format("2006-01")
	m.params[m.atual] = setArg(m.params[m.atual], "--mes", novo)
	m.msg = ""
	m.recarrega()
}

// cicleTipo alterna o filtro --tipo entre todos → pagar → receber e recarrega.
func (m *model) cicleTipo() {
	ordem := []string{"", "pagar", "receber"}
	atual := getArg(m.params[m.atual], "--tipo")
	prox := ""
	for i, t := range ordem {
		if t == atual {
			prox = ordem[(i+1)%len(ordem)]
			break
		}
	}
	m.params[m.atual] = setArg(m.params[m.atual], "--tipo", prox)
	m.msg = ""
	m.recarrega()
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
