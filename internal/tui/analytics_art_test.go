package tui

import (
	"strings"
	"testing"
)

func TestCabecalhoAnalytics(t *testing.T) {
	for _, largura := range []int{40, 80, 130} {
		ban := cabecalhoSelo(largura, "ANALYTICS", "— análise financeira (somente leitura)", corAnalytics)
		if !strings.Contains(ban, "ANALYTICS") && !strings.Contains(ban, "A N A L Y T I C S") {
			t.Errorf("largura %d: cabeçalho analytics deveria conter o selo ANALYTICS:\n%s", largura, ban)
		}
		pessoal := cabecalho(largura, false)
		if strings.Contains(pessoal, "ANALYTICS") || strings.Contains(pessoal, "A N A L Y T I C S") {
			t.Errorf("largura %d: cabeçalho pessoal não deveria conter o selo ANALYTICS:\n%s", largura, pessoal)
		}
	}
}
