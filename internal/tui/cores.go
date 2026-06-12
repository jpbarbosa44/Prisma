package tui

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// Esquema de cores aplicado sobre a saída capturada da CLI, na hora de
// exibir: valores negativos e despesas em vermelho, receitas em verde,
// pendências em amarelo. As linhas cruas ficam intactas — a seleção de
// tabela e o parse de IDs acontecem antes, sobre o texto puro.
var (
	reValor    = regexp.MustCompile(`-?R\$ [0-9.,]+`)
	rePagar    = regexp.MustCompile(`\bpagar\b`)
	reReceber  = regexp.MustCompile(`\breceber\b`)
	rePendente = regexp.MustCompile(`\bpendentes?\b`)
	reQuitado  = regexp.MustCompile(`\bquitad[oa]s?\b`)
)

// colorir aplica o esquema de cores a uma linha da saída da CLI.
func colorir(l string) string {
	t := strings.TrimSpace(l)
	switch {
	case t == "":
		return l
	case ehCabecalhoTabela(t):
		return corApagada.Render(l)
	case strings.HasPrefix(t, "⚠") || strings.HasPrefix(t, "Aviso"):
		return corAmar.Render(l)
	case strings.HasPrefix(t, "erro"):
		return corErro.Render(l)
	}

	pagar, receber := rePagar.MatchString(l), reReceber.MatchString(l)
	if pagar && receber && strings.Contains(l, "|") {
		// rodapés tipo "Pendente a pagar: R$ X | Pendente a receber: R$ Y":
		// cada lado tem um contexto, então cada lado é colorido sozinho
		partes := strings.Split(l, "|")
		for i, p := range partes {
			partes[i] = colorir(p)
		}
		return strings.Join(partes, "|")
	}

	// valores: negativo é sempre vermelho; positivo segue o contexto da linha
	l = reValor.ReplaceAllStringFunc(l, func(v string) string {
		switch {
		case strings.HasPrefix(v, "-"):
			return corErro.Render(v)
		case pagar && !receber:
			return corErro.Render(v)
		case receber && !pagar:
			return corOK.Render(v)
		}
		return v
	})

	// palavras de tipo e status
	l = rePagar.ReplaceAllStringFunc(l, pinta(corErro))
	l = reReceber.ReplaceAllStringFunc(l, pinta(corOK))
	l = rePendente.ReplaceAllStringFunc(l, pinta(corAmar))
	l = reQuitado.ReplaceAllStringFunc(l, pinta(corOK))
	return l
}

// pinta adapta um estilo ao formato de função do ReplaceAllStringFunc.
func pinta(st lipgloss.Style) func(string) string {
	return func(s string) string { return st.Render(s) }
}

// ehCabecalhoTabela detecta linhas de cabeçalho ("ID  TIPO  DESCRIÇÃO ...");
// elas são todas maiúsculas, sem valores e não começam com número.
func ehCabecalhoTabela(t string) bool {
	if strings.Contains(t, "R$") || t != strings.ToUpper(t) {
		return false
	}
	if t[0] >= '0' && t[0] <= '9' {
		return false
	}
	return strings.IndexFunc(t, unicode.IsLetter) >= 0
}
