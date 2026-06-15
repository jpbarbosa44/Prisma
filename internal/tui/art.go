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

// cabecalho monta o prisma 3D (com feixe de luz entrando e o espectro saindo)
// ao lado da palavra PRISMA em letras grandes; em telas estreitas, versões
// mais compactas.
func cabecalho(largura int) string {
	if largura < 60 {
		return corTitulo.Render(" ◆ P R I S M A ") + corApagada.Render("— finanças pessoais")
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
		return prisma + "\n\n" + strings.Repeat(" ", margem) +
			corTitulo.Render("P R I S M A ") + corApagada.Render("— finanças pessoais")
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

	return lipgloss.JoinHorizontal(lipgloss.Center, prisma, "   ", strings.Join(letras, "\n"))
}
