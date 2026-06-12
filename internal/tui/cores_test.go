package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestColorir(t *testing.T) {
	// sem TTY o lipgloss desliga as cores; força para o teste enxergá-las
	antigo := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(antigo) })

	t.Run("despesa fica vermelha, status pendente amarelo", func(t *testing.T) {
		s := colorir("12  pagar  Aluguel  moradia  05/07/2026  R$ 1.200,00  pendente")
		if !strings.Contains(s, corErro.Render("R$ 1.200,00")) {
			t.Errorf("valor de despesa devia estar vermelho: %q", s)
		}
		if !strings.Contains(s, corAmar.Render("pendente")) {
			t.Errorf("status pendente devia estar amarelo: %q", s)
		}
	})

	t.Run("receita fica verde", func(t *testing.T) {
		s := colorir("3  receber  Salário  salario  05/07/2026  R$ 5.000,00  quitado")
		if !strings.Contains(s, corOK.Render("R$ 5.000,00")) {
			t.Errorf("valor de receita devia estar verde: %q", s)
		}
		if !strings.Contains(s, corOK.Render("quitado")) {
			t.Errorf("status quitado devia estar verde: %q", s)
		}
	})

	t.Run("saldo negativo sempre vermelho", func(t *testing.T) {
		s := colorir("Banco  corrente  -R$ 320,00")
		if !strings.Contains(s, corErro.Render("-R$ 320,00")) {
			t.Errorf("saldo negativo devia estar vermelho: %q", s)
		}
	})

	t.Run("saldo positivo neutro", func(t *testing.T) {
		s := colorir("Banco  corrente  R$ 1.500,00")
		if strings.Contains(s, corErro.Render("R$ 1.500,00")) || strings.Contains(s, corOK.Render("R$ 1.500,00")) {
			t.Errorf("saldo sem contexto devia ficar neutro: %q", s)
		}
	})

	t.Run("rodapé com os dois contextos colore cada lado", func(t *testing.T) {
		s := colorir("Pendente a pagar: R$ 3.600,00 | Pendente a receber: R$ 500,00")
		if !strings.Contains(s, corErro.Render("R$ 3.600,00")) || !strings.Contains(s, corOK.Render("R$ 500,00")) {
			t.Errorf("cada lado do | devia ter sua cor: %q", s)
		}
	})

	t.Run("cabeçalho de tabela apagado", func(t *testing.T) {
		l := "ID  TIPO  DESCRIÇÃO  CATEGORIA  VENCIMENTO  VALOR  STATUS"
		if s := colorir(l); s != corApagada.Render(l) {
			t.Errorf("cabeçalho devia estar apagado: %q", s)
		}
	})

	t.Run("linha de dados em caixa alta não vira cabeçalho", func(t *testing.T) {
		l := "7  MERCADO LTDA  importado  12/06/2026"
		if s := colorir(l); s == corApagada.Render(l) {
			t.Errorf("linha de dados não devia ser tratada como cabeçalho: %q", s)
		}
	})

	t.Run("avisos amarelos", func(t *testing.T) {
		l := "⚠ Limite estourado em: mercado"
		if s := colorir(l); s != corAmar.Render(l) {
			t.Errorf("aviso devia estar amarelo: %q", s)
		}
	})
}
