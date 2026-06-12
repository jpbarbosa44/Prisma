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

// cabecalho monta o prisma (com feixe de luz entrando e o espectro saindo)
// ao lado da palavra PRISMA em letras grandes.
func cabecalho(largura int) string {
	if largura < 78 {
		return corTitulo.Render(" ◆ P R I S M A ") + corApagada.Render("— finanças pessoais")
	}

	p := corPrisma.Render
	prisma := []string{
		p("           /\\"),
		p("          /  \\"),
		p("         /    \\"),
		corFeixe.Render(" ───────") + p("/      \\") + " " + corVerm.Render("━━━━━"),
		p("       /        \\") + " " + corAmar.Render("━━━━━"),
		p("      /__________\\") + " " + corMagenta.Render("━━━━━"),
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

	esq := strings.Join(prisma, "\n")
	dir := strings.Join(letras, "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, esq, "   ", dir)
}
