package money

import "testing"

func TestParse(t *testing.T) {
	casos := []struct {
		entrada string
		quer    int64
	}{
		{"1234", 123400},
		{"1234,56", 123456},
		{"1.234,56", 123456},
		{"1234.56", 123456},
		{"1.200", 120000}, // ponto de milhar (sem vírgula)
		{"1.000.000", 100000000},
		{"1.2", 120},          // um dígito após o ponto ainda é decimal
		{"100.000", 10000000}, // cem mil
		{"R$ 10", 1000},
		{"R$ 1.234,56", 123456},
		{"-5,50", -550},
		{"0,5", 50},
		{"0", 0},
		{",99", 99},
	}
	for _, c := range casos {
		got, err := Parse(c.entrada)
		if err != nil {
			t.Errorf("Parse(%q): erro inesperado: %v", c.entrada, err)
			continue
		}
		if got != c.quer {
			t.Errorf("Parse(%q) = %d, quer %d", c.entrada, got, c.quer)
		}
	}
}

func TestParseInvalido(t *testing.T) {
	for _, entrada := range []string{
		"", "abc", "1,234", "12,3456", "1.2.3",
		"-",              // só o sinal não é um valor
		"12,-5", "12,+5", // sinal no meio distorceria o valor em silêncio
		"+5",                // o "+" de receita é responsabilidade de quem chama
		"99999999999999999", // estouraria o int64 e viraria negativo
	} {
		if _, err := Parse(entrada); err == nil {
			t.Errorf("Parse(%q): esperava erro, não veio", entrada)
		}
	}
}

func TestParseTeto(t *testing.T) {
	// o maior valor aceito: 10 bilhões de reais
	if v, err := Parse("10.000.000.000,00"); err != nil || v != 1_000_000_000_000 {
		t.Errorf("Parse(10 bi) = %d, %v; queria 1_000_000_000_000", v, err)
	}
	if _, err := Parse("10.000.000.001,00"); err == nil {
		t.Error("Parse(acima do teto): esperava erro, não veio")
	}
}

func TestFormat(t *testing.T) {
	casos := []struct {
		entrada int64
		quer    string
	}{
		{123456, "R$ 1.234,56"},
		{-50, "-R$ 0,50"},
		{0, "R$ 0,00"},
		{100000000, "R$ 1.000.000,00"},
		{999, "R$ 9,99"},
		{100, "R$ 1,00"},
	}
	for _, c := range casos {
		if got := Format(c.entrada); got != c.quer {
			t.Errorf("Format(%d) = %q, quer %q", c.entrada, got, c.quer)
		}
	}
}

func TestParseFormatIdaEVolta(t *testing.T) {
	for _, v := range []int64{1, 99, 100, 123456, 999999999} {
		got, err := Parse(Format(v))
		if err != nil || got != v {
			t.Errorf("Parse(Format(%d)) = %d, %v", v, got, err)
		}
	}
}
