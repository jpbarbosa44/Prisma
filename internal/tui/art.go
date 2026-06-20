package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	corPrisma  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // ciano
	corFeixe   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")) // luz que entra
	corVerm    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	corAmar    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	corMagenta = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	corTitulo  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	corSelec   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Bold(true)
	corApagada = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	corErro    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	corOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// arte é o prisma em 3D; o feixe entra na face esquerda e o espectro sai da
// face frontal, à direita.
var arte = []string{
	`         /=\\`,
	`        /===\ \`,
	`       /=====\ ' \`,
	`      /=======\ ' ' \`,
	`     /=========\ ' ' '\`,
	`    /===========\ ' ' ' /`,
	`   /=============\ ' ' /`,
	`  /===============\ ' /`,
	` /=================\ /`,
	`/===================\/`,
}

const (
	margem     = 8 // folga à esquerda, atravessada pelo feixe que entra
	linhaFeixe = 4 // linha em que o feixe branco atinge a face esquerda
)

// raios é o espectro que sai da face frontal: linha → cor.
var raios = map[int]lipgloss.Style{4: corVerm, 5: corAmar, 6: corMagenta}

// corCorp é o selo "CORP" que marca o modo empresa (`prisma --empresa`); corAnalytics
// é o selo "ANALYTICS" (`prisma --analytics`), cada um numa cor distinta da arte
// pra ficar óbvio em qual modo se está.
var (
	corCorp      = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // âmbar
	corAnalytics = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true) // magenta
)

// espacar separa as letras com espaços ("CORP" → "C O R P"), no estilo do selo.
func espacar(s string) string {
	return strings.Join(strings.Split(s, ""), " ")
}

// badge monta um selo emoldurado de 3 linhas com o texto espaçado e centrado —
// bem mais visível que um texto inline e sem desenhar letras de bloco à mão
// (risco de desalinhar sem ver renderizado). Dimensiona a moldura ao texto.
func badge(texto string) []string {
	inner := "   " + espacar(texto) + "   "
	largura := len([]rune(inner))
	return []string{
		"┌" + strings.Repeat("─", largura) + "┐",
		"│" + inner + "│",
		"└" + strings.Repeat("─", largura) + "┘",
	}
}

// badgeAoLado devolve o badge colorido, alinhado verticalmente para ficar ao
// lado de um bloco de `altura` linhas (ex.: as letras grandes do PRISMA),
// centrado nessa altura.
func badgeAoLado(texto string, altura int, cor lipgloss.Style) string {
	b := badge(texto)
	topo := (altura - len(b)) / 2
	if topo < 0 {
		topo = 0
	}
	linhas := make([]string, altura)
	for i := range linhas {
		linhas[i] = ""
	}
	for i, l := range b {
		if topo+i < altura {
			linhas[topo+i] = cor.Render(l)
		}
	}
	return strings.Join(linhas, "\n")
}

// cabecalho é o atalho para os dois modos com banco próprio: pessoal e empresa.
func cabecalho(largura int, modoEmpresa bool) string {
	if modoEmpresa {
		return cabecalhoSelo(largura, "CORP", "— finanças empresariais", corCorp)
	}
	return cabecalhoSelo(largura, "", "— finanças pessoais", corCorp)
}

// cabecalhoSelo monta o prisma 3D (com feixe de luz entrando e o espectro
// saindo) ao lado da palavra PRISMA em letras grandes; em telas estreitas,
// versões mais compactas. Com `selo` não-vazio (ex.: "CORP", "ANALYTICS"),
// troca o subtítulo e acrescenta o badge correspondente na cor `corSelo` — não
// redesenha PRISMA em letras de bloco com sufixo porque dá pra errar o
// alinhamento sem ver renderizado.
func cabecalhoSelo(largura int, selo, subtitulo string, corSelo lipgloss.Style) string {
	if largura < 60 {
		s := corTitulo.Render(" ◆ P R I S M A ") + corApagada.Render(subtitulo)
		if selo != "" {
			s += " " + corSelo.Render("["+selo+"]")
		}
		return s
	}

	linhas := make([]string, len(arte))
	for i, l := range arte {
		var b strings.Builder
		if i == linhaFeixe {
			brancos := len(l) - len(strings.TrimLeft(l, " "))
			b.WriteString(corFeixe.Render(strings.Repeat("─", margem+brancos)))
			b.WriteString(corPrisma.Render(strings.TrimLeft(l, " ")))
		} else {
			b.WriteString(strings.Repeat(" ", margem))
			b.WriteString(corPrisma.Render(l))
		}
		// o raio sai colado na borda, acompanhando a inclinação da face
		if cor, ok := raios[i]; ok {
			b.WriteString(" " + cor.Render("━━━━━━━━━━━━"))
		}
		linhas[i] = b.String()
	}
	prisma := strings.Join(linhas, "\n")

	// sem espaço para as letras grandes: título sob o prisma
	if largura < 110 {
		linha := strings.Repeat(" ", margem) + corTitulo.Render("P R I S M A ") + corApagada.Render(subtitulo)
		if selo != "" {
			linha += "  " + corSelo.Render("◆ "+espacar(selo)+" ◆")
		}
		return prisma + "\n\n" + linha
	}

	letras := []string{
		"██████╗ ██████╗ ██╗███████╗███╗   ███╗ █████╗ ",
		"██╔══██╗██╔══██╗██║██╔════╝████╗ ████║██╔══██╗",
		"██████╔╝██████╔╝██║███████╗██╔████╔██║███████║",
		"██╔═══╝ ██╔══██╗██║╚════██║██║╚██╔╝██║██╔══██║",
		"██║     ██║  ██║██║███████║██║ ╚═╝ ██║██║  ██║",
		"╚═╝     ╚═╝  ╚═╝╚═╝╚══════╝╚═╝     ╚═╝╚═╝  ╚═╝",
	}
	for i := range letras {
		letras[i] = corTitulo.Render(letras[i])
	}
	if selo != "" {
		return lipgloss.JoinHorizontal(lipgloss.Center, prisma, "   ", strings.Join(letras, "\n"), "  ", badgeAoLado(selo, len(letras), corSelo))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, prisma, "   ", strings.Join(letras, "\n"))
}
