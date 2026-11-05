package app

import (
	"strings"
	"testing"
	"time"
)

// Os códigos abaixo têm dígitos verificadores reais: o do Banco do Brasil e as
// contas de consumo de módulo 10 são exemplos publicados; os demais foram
// montados pelo algoritmo FEBRABAN conferido contra eles. O Pix sem valor é o
// exemplo do manual do BR Code do Banco Central.
const (
	boletoBB       = "00190.50095 40144.816069 06809.350314 3 37370000000100" // R$ 1,00, fator 3737
	boletoItauNovo = "34191751243456701234856789012343216360000123456"        // R$ 1.234,56, fator 1636 (ciclo novo)
	contaLuz       = "836200000005 667800481000 180975657313 001589636081"    // módulo 10, R$ 66,78
	contaAguaMod11 = "828500000000899001234560789012345675890123456785"       // módulo 11, R$ 89,90
	pixSemValor    = "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D"
	pixComValor    = "00020126360014br.gov.bcb.pix0114+5511999999999520400005303986540510.505802BR5913Padaria do Ze6009SAO PAULO62070503***63048525"
	barrasItauNovo = "34192163600001234561751234567012345678901234"
)

func TestLerCodigoPagamento(t *testing.T) {
	antes := relogio
	relogio = func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { relogio = antes })

	casos := []struct {
		codigo           string
		tipo, venc, benf string
		valor            int64
	}{
		// fator 3737: no ciclo novo seria 2032; em 2026, a data mais próxima é a do ciclo novo
		{boletoBB, "boleto", "2032-08-21", "Banco do Brasil", 100},
		{boletoItauNovo, "boleto", "2026-11-20", "Itaú", 123456},
		{barrasItauNovo, "boleto", "2026-11-20", "Itaú", 123456},
		{contaLuz, "arrecadacao", "", "Conta de luz/gás", 6678},
		{contaAguaMod11, "arrecadacao", "", "Conta de água", 8990},
		{pixSemValor, "pix", "", "Fulano de Tal", 0},
		{pixComValor, "pix", "", "Padaria do Ze", 1050},
	}
	for _, c := range casos {
		got, err := LerCodigoPagamento(c.codigo)
		if err != nil {
			t.Errorf("%s: %v", c.codigo, err)
			continue
		}
		if got.Tipo != c.tipo || got.Vencimento != c.venc || got.Beneficiario != c.benf || got.Valor != c.valor {
			t.Errorf("%s:\n got %+v\nquer tipo=%s venc=%s benef=%s valor=%d", c.codigo, got, c.tipo, c.venc, c.benf, c.valor)
		}
	}

	// o mesmo boleto lido em 2008 cai no ciclo antigo (31/12/2007)
	relogio = func() time.Time { return time.Date(2008, 1, 2, 0, 0, 0, 0, time.UTC) }
	if got, _ := LerCodigoPagamento(boletoBB); got.Vencimento != "2007-12-31" {
		t.Errorf("fator 3737 em 2008 = %s, quer 2007-12-31", got.Vencimento)
	}

	ruins := map[string]string{
		strings.Replace(boletoItauNovo, "3456", "3457", 1): "dígito verificador",
		strings.Replace(contaAguaMod11, "8990", "8991", 1): "dígito verificador",
		pixComValor[:len(pixComValor)-1] + "6":             "CRC",
		"12345":                                            "dígitos",
		"boleto da luz":                                    "não reconheci",
	}
	for cod, quer := range ruins {
		if _, err := LerCodigoPagamento(cod); err == nil || !strings.Contains(err.Error(), quer) {
			t.Errorf("%q: erro %v, queria algo com %q", cod, err, quer)
		}
	}
}

func TestPagarCodigo(t *testing.T) {
	conn := abreDB(t)
	silencia(t, func() error { return Regra(conn, []string{"add", "padaria", "padaria"}) })
	saida := capturaSaida(t, func() error { return PagarCodigo(conn, []string{pixComValor}) })
	if !strings.Contains(saida, "Pix Padaria do Ze") || !strings.Contains(saida, "10,50") {
		t.Errorf("saída:\n%s", saida)
	}
	var desc, cat, obs, venc string
	var valor int64
	conn.QueryRow(`SELECT descricao, categoria, observacao, valor, vencimento FROM lancamentos`).Scan(&desc, &cat, &obs, &valor, &venc)
	if desc != "Pix Padaria do Ze" || cat != "padaria" || obs != pixComValor || valor != 1050 || venc != time.Now().Format("2006-01-02") {
		t.Errorf("lançamento: %q %q %q %d %s", desc, cat, obs, valor, venc)
	}
	if err := PagarCodigo(conn, []string{pixComValor}); err == nil || !strings.Contains(err.Error(), "já foi cadastrado") {
		t.Errorf("o mesmo código duas vezes devia ser recusado: %v", err)
	}
	if err := PagarCodigo(conn, []string{pixSemValor}); err == nil || !strings.Contains(err.Error(), "--valor") {
		t.Errorf("Pix sem valor devia pedir --valor: %v", err)
	}
	silencia(t, func() error {
		return PagarCodigo(conn, []string{pixSemValor, "--valor", "25", "--venc", "2026-12-01", "--desc", "Aluguel"})
	})
	conn.QueryRow(`SELECT valor, vencimento FROM lancamentos WHERE descricao = 'Aluguel'`).Scan(&valor, &venc)
	if valor != 2500 || venc != "2026-12-01" {
		t.Errorf("Pix sem valor com --valor/--venc: %d %s", valor, venc)
	}
	silencia(t, func() error { return NovoLancamento(conn, "pagar", []string{"boleto", contaLuz}) })
	conn.QueryRow(`SELECT valor FROM lancamentos WHERE descricao = 'Conta de luz/gás'`).Scan(&valor)
	if valor != 6678 {
		t.Errorf("prisma pagar boleto (conta de luz): valor %d", valor)
	}
}
