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

// corCorp é o selo "CORP" que marca o modo empresa (`prisma --empresa`),
// numa cor diferente do resto da arte pra ficar óbvio que é outro banco.
var corCorp = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // âmbar

// badgeCorp é um selo emoldurado de 3 linhas — bem maior que um texto inline
// solto, sem precisar desenhar "CORP" em letras de bloco caractere a
// caractere (risco de desalinhar sem ver renderizado).
var badgeCorp = []string{
	"┌─────────────┐",
	"│   C O R P   │",
	"└─────────────┘",
}

// badgeCorpAoLado devolve o badgeCorp colorido, alinhado verticalmente para
// ficar ao lado de um bloco de altura linhas (ex.: as letras grandes do
// PRISMA), com o badge centrado nessa altura.
func badgeCorpAoLado(altura int) string {
	topo := (altura - len(badgeCorp)) / 2
	linhas := make([]string, altura)
	for i := range linhas {
		linhas[i] = ""
	}
	for i, l := range badgeCorp {
		linhas[topo+i] = corCorp.Render(l)
	}
	return strings.Join(linhas, "\n")
}

// cabecalho monta o prisma 3D (com feixe de luz entrando e o espectro saindo)
// ao lado da palavra PRISMA em letras grandes; em telas estreitas, versões
// mais compactas. Em modoEmpresa, troca o subtítulo e acrescenta o
// badgeCorp — não redesenha PRISMA em letras de bloco com sufixo CORP porque
// dá pra errar o alinhamento sem ver renderizado; se quiser isso depois, ajusta.
func cabecalho(largura int, modoEmpresa bool) string {
	subtitulo := "— finanças pessoais"
	if modoEmpresa {
		subtitulo = "— finanças empresariais"
	}

	if largura < 60 {
		s := corTitulo.Render(" ◆ P R I S M A ") + corApagada.Render(subtitulo)
		if modoEmpresa {
			s += " " + corCorp.Render("[CORP]")
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
		if modoEmpresa {
			linha += "  " + corCorp.Render("◆ C O R P ◆")
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
	if modoEmpresa {
		return lipgloss.JoinHorizontal(lipgloss.Center, prisma, "   ", strings.Join(letras, "\n"), "  ", badgeCorpAoLado(len(letras)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, prisma, "   ", strings.Join(letras, "\n"))
}
