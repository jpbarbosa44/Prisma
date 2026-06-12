// Package money lida com valores monetários em centavos (int64),
// evitando erros de arredondamento de ponto flutuante.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse aceita "1234", "1234,56", "1.234,56" e "1234.56" e retorna centavos.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "R$")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("valor vazio")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}

	var inteiro, decimal string
	switch {
	case strings.Contains(s, ","):
		// vírgula é o separador decimal; pontos são milhares
		partes := strings.SplitN(s, ",", 2)
		inteiro = strings.ReplaceAll(partes[0], ".", "")
		decimal = partes[1]
	case strings.Contains(s, "."):
		// sem vírgula: ponto é separador decimal (estilo 1234.56)
		partes := strings.SplitN(s, ".", 2)
		inteiro = partes[0]
		decimal = partes[1]
	default:
		inteiro = s
		decimal = "00"
	}

	if len(decimal) > 2 {
		return 0, fmt.Errorf("valor %q tem mais de duas casas decimais", s)
	}
	for len(decimal) < 2 {
		decimal += "0"
	}
	if inteiro == "" {
		inteiro = "0"
	}

	i, err := strconv.ParseInt(inteiro, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("valor inválido: %q", s)
	}
	d, err := strconv.ParseInt(decimal, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("valor inválido: %q", s)
	}
	c := i*100 + d
	if neg {
		c = -c
	}
	return c, nil
}

// Format retorna o valor em centavos formatado como "R$ 1.234,56".
func Format(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	inteiro := c / 100
	decimal := c % 100

	digitos := strconv.FormatInt(inteiro, 10)
	var b strings.Builder
	pre := len(digitos) % 3
	if pre > 0 {
		b.WriteString(digitos[:pre])
		if len(digitos) > pre {
			b.WriteString(".")
		}
	}
	for i := pre; i < len(digitos); i += 3 {
		b.WriteString(digitos[i : i+3])
		if i+3 < len(digitos) {
			b.WriteString(".")
		}
	}

	sinal := ""
	if neg {
		sinal = "-"
	}
	return fmt.Sprintf("%sR$ %s,%02d", sinal, b.String(), decimal)
}
