package tui

import (
	"strings"
	"testing"
)

func TestCabecalhoModoEmpresa(t *testing.T) {
	// largura 40 usa a tag inline "[CORP]"; 80 e 130 usam o badge emoldurado
	// "C O R P" (letras espaçadas, no estilo do resto do cabeçalho).
	// 130: largo o bastante pras letras de bloco — aí não tem linha de
	// subtítulo nenhuma (nem "pessoais" no modo pessoal), só o badge.
	for _, largura := range []int{40, 80, 130} {
		pessoal := cabecalho(largura, false)
		empresa := cabecalho(largura, true)
		if !strings.Contains(empresa, "CORP") && !strings.Contains(empresa, "C O R P") {
			t.Errorf("largura %d: cabeçalho de empresa deveria conter o selo CORP:\n%s", largura, empresa)
		}
		if strings.Contains(pessoal, "CORP") || strings.Contains(pessoal, "C O R P") {
			t.Errorf("largura %d: cabeçalho pessoal não deveria conter o selo CORP:\n%s", largura, pessoal)
		}
	}
	for _, largura := range []int{40, 80} {
		pessoal := cabecalho(largura, false)
		empresa := cabecalho(largura, true)
		if !strings.Contains(pessoal, "pessoais") {
			t.Errorf("largura %d: cabeçalho pessoal deveria mencionar finanças pessoais:\n%s", largura, pessoal)
		}
		if !strings.Contains(empresa, "empresariais") {
			t.Errorf("largura %d: cabeçalho de empresa deveria mencionar finanças empresariais:\n%s", largura, empresa)
		}
	}
}
