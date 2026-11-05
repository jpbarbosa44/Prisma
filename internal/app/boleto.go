package app

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"prisma/internal/money"
)

// Leitura de códigos de pagamento: a linha digitável de um boleto (bancário,
// 47 dígitos, ou de conta de consumo/tributo, 48 dígitos começando com 8), o
// código de barras (44 dígitos) e o Pix copia-e-cola (BR Code). Os dígitos
// verificadores são conferidos — um número digitado errado não vira conta.

// CodigoPagamento é o que se lê de um boleto ou Pix.
type CodigoPagamento struct {
	Tipo         string `json:"tipo"`  // boleto | arrecadacao | pix
	Valor        int64  `json:"valor"` // centavos; 0 = o código não traz o valor
	Vencimento   string `json:"vencimento"`
	Beneficiario string `json:"beneficiario"` // banco do boleto, recebedor do Pix
	Codigo       string `json:"codigo"`       // linha digitável / copia-e-cola, para pagar depois
}

// Descricao é a descrição padrão da conta a pagar criada a partir do código.
func (c CodigoPagamento) Descricao() string {
	switch c.Tipo {
	case "pix":
		if c.Beneficiario != "" {
			return "Pix " + c.Beneficiario
		}
		return "Pix"
	case "arrecadacao":
		return c.Beneficiario
	}
	if c.Beneficiario != "" {
		return "Boleto " + c.Beneficiario
	}
	return "Boleto"
}

// bancos mais comuns (código FEBRABAN → nome curto) para descrever o boleto.
var bancos = map[string]string{
	"001": "Banco do Brasil", "033": "Santander", "041": "Banrisul", "070": "BRB", "077": "Inter",
	"104": "Caixa", "208": "BTG", "212": "Original", "237": "Bradesco", "260": "Nubank",
	"290": "PagBank", "323": "Mercado Pago", "336": "C6", "341": "Itaú", "380": "PicPay",
	"422": "Safra", "655": "Votorantim", "745": "Citibank", "748": "Sicredi", "756": "Sicoob",
}

// segmentos das contas de consumo/tributos (2º dígito do código).
var segmentos = map[byte]string{
	'1': "Prefeitura", '2': "Conta de água", '3': "Conta de luz/gás", '4': "Conta de telefone/internet",
	'5': "Tributo", '6': "Carnê", '7': "Multa de trânsito", '9': "Boleto de convênio",
}

// LerCodigoPagamento reconhece e confere um código colado pelo usuário.
func LerCodigoPagamento(s string) (CodigoPagamento, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "000201") {
		return lerPix(s)
	}
	dig := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		if unicode.IsSpace(r) || r == '.' || r == '-' {
			return -1
		}
		return 'x'
	}, s)
	if strings.ContainsRune(dig, 'x') || dig == "" {
		return CodigoPagamento{}, errors.New("não reconheci o código: cole a linha digitável do boleto (só números) ou o Pix copia-e-cola")
	}
	switch len(dig) {
	case 47:
		return lerBoletoLinha(dig)
	case 48:
		return lerArrecadacaoLinha(dig)
	case 44:
		if dig[0] == '8' {
			return lerArrecadacaoBarras(dig, dig)
		}
		return lerBoletoBarras(dig, dig)
	}
	return CodigoPagamento{}, fmt.Errorf("o código tem %d dígitos; a linha digitável tem 47 (boleto) ou 48 (contas de consumo)", len(dig))
}

func digitos(s string) []int {
	d := make([]int, len(s))
	for i, c := range s {
		d[i] = int(c - '0')
	}
	return d
}

// modulo10 é o DV FEBRABAN de módulo 10 (pesos 2,1 da direita, somando os
// algarismos de cada produto).
func modulo10(s string) int {
	soma, peso := 0, 2
	d := digitos(s)
	for i := len(d) - 1; i >= 0; i-- {
		p := d[i] * peso
		soma += p/10 + p%10
		peso = 3 - peso
	}
	if r := soma % 10; r != 0 {
		return 10 - r
	}
	return 0
}

// modulo11 soma com pesos 2..9 da direita e devolve o resto.
func modulo11(s string) int {
	soma, peso := 0, 2
	d := digitos(s)
	for i := len(d) - 1; i >= 0; i-- {
		soma += d[i] * peso
		if peso++; peso > 9 {
			peso = 2
		}
	}
	return soma % 11
}

// dvBoleto é o DV geral do código de barras bancário (módulo 11; 0, 10 e 11 viram 1).
func dvBoleto(s string) int {
	dv := 11 - modulo11(s)
	if dv == 0 || dv == 10 || dv == 11 {
		return 1
	}
	return dv
}

// dvArrecadacao é o DV de contas de consumo: módulo 10 (identificador 6/7) ou
// módulo 11 (8/9; restos 0 e 1 dão 0).
func dvArrecadacao(s string, mod11 bool) int {
	if !mod11 {
		return modulo10(s)
	}
	if r := modulo11(s); r > 1 {
		return 11 - r
	}
	return 0
}

var errDV = errors.New("o código não confere (dígito verificador): confira se foi copiado inteiro")

// vencimentoFator converte o fator de vencimento do boleto. O fator voltou a
// 1000 em 22/02/2025 (esgotou o 9999 contado de 07/10/1997); dos dois ciclos,
// vale a data mais próxima de hoje. Fator 0 = boleto sem vencimento.
func vencimentoFator(fator int, hoje time.Time) string {
	if fator == 0 {
		return ""
	}
	d := time.Date(1997, 10, 7, 0, 0, 0, 0, time.UTC).AddDate(0, 0, fator)
	if fator >= 1000 {
		novo := time.Date(2025, 2, 22, 0, 0, 0, 0, time.UTC).AddDate(0, 0, fator-1000)
		if absDur(novo.Sub(hoje)) < absDur(d.Sub(hoje)) {
			d = novo
		}
	}
	return d.Format("2006-01-02")
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func lerBoletoLinha(l string) (CodigoPagamento, error) {
	// três campos com DV próprio (módulo 10), o DV geral e fator+valor
	for _, c := range [][2]int{{0, 9}, {10, 20}, {21, 31}} {
		if modulo10(l[c[0]:c[1]]) != int(l[c[1]]-'0') {
			return CodigoPagamento{}, errDV
		}
	}
	barras := l[0:4] + l[32:33] + l[33:47] + l[4:9] + l[10:20] + l[21:31]
	return lerBoletoBarras(barras, l)
}

func lerBoletoBarras(b, codigo string) (CodigoPagamento, error) {
	if dvBoleto(b[:4]+b[5:]) != int(b[4]-'0') {
		return CodigoPagamento{}, errDV
	}
	fator, _ := strconv.Atoi(b[5:9])
	valor, _ := strconv.ParseInt(b[9:19], 10, 64)
	return CodigoPagamento{
		Tipo: "boleto", Valor: valor, Vencimento: vencimentoFator(fator, relogio()),
		Beneficiario: bancos[b[0:3]], Codigo: codigo,
	}, nil
}

func lerArrecadacaoLinha(l string) (CodigoPagamento, error) {
	if l[0] != '8' {
		return CodigoPagamento{}, errors.New("código de 48 dígitos que não começa com 8: não é uma conta de consumo")
	}
	mod11 := l[2] == '8' || l[2] == '9'
	var barras strings.Builder
	for i := 0; i < 4; i++ {
		bloco := l[i*12 : i*12+11]
		if dvArrecadacao(bloco, mod11) != int(l[i*12+11]-'0') {
			return CodigoPagamento{}, errDV
		}
		barras.WriteString(bloco)
	}
	return lerArrecadacaoBarras(barras.String(), l)
}

func lerArrecadacaoBarras(b, codigo string) (CodigoPagamento, error) {
	id := b[2]
	if id < '6' || id > '9' {
		return CodigoPagamento{}, errors.New("conta de consumo com identificador de valor desconhecido")
	}
	if dvArrecadacao(b[:3]+b[4:], id == '8' || id == '9') != int(b[3]-'0') {
		return CodigoPagamento{}, errDV
	}
	c := CodigoPagamento{Tipo: "arrecadacao", Beneficiario: segmentos[b[1]], Codigo: codigo}
	if c.Beneficiario == "" {
		c.Beneficiario = "Conta de consumo"
	}
	if id == '6' || id == '8' { // 7 e 9: o campo é uma referência, não o valor em reais
		c.Valor, _ = strconv.ParseInt(b[4:15], 10, 64)
	}
	return c, nil
}

// crc16 é o CRC-16/CCITT-FALSE do BR Code (polinômio 0x1021, início 0xFFFF).
func crc16(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for b := 0; b < 8; b++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// camposTLV quebra um trecho EMV (ID de 2 dígitos, tamanho de 2, valor).
func camposTLV(s string) (map[string]string, error) {
	out := map[string]string{}
	for len(s) > 0 {
		if len(s) < 4 {
			return nil, errors.New("código do Pix incompleto")
		}
		n, err := strconv.Atoi(s[2:4])
		if err != nil || len(s) < 4+n {
			return nil, errors.New("código do Pix incompleto")
		}
		out[s[:2]] = s[4 : 4+n]
		s = s[4+n:]
	}
	return out, nil
}

func lerPix(s string) (CodigoPagamento, error) {
	if len(s) < 8 || s[len(s)-8:len(s)-4] != "6304" {
		return CodigoPagamento{}, errors.New("código do Pix incompleto: falta o final (CRC)")
	}
	if fmt.Sprintf("%04X", crc16(s[:len(s)-4])) != strings.ToUpper(s[len(s)-4:]) {
		return CodigoPagamento{}, errors.New("o Pix copia-e-cola não confere (CRC): copie o código inteiro de novo")
	}
	campos, err := camposTLV(s)
	if err != nil {
		return CodigoPagamento{}, err
	}
	conta, err := camposTLV(campos["26"])
	if err != nil || !strings.EqualFold(conta["00"], "br.gov.bcb.pix") {
		return CodigoPagamento{}, errors.New("não é um Pix copia-e-cola (BR Code)")
	}
	c := CodigoPagamento{Tipo: "pix", Beneficiario: strings.TrimSpace(campos["59"]), Codigo: s}
	if v := campos["54"]; v != "" { // valor em reais com ponto: "123.45"
		reais, cent, _ := strings.Cut(v, ".")
		cent = (cent + "00")[:2]
		r, err1 := strconv.ParseInt(reais, 10, 64)
		ct, err2 := strconv.ParseInt(cent, 10, 64)
		if err1 != nil || err2 != nil {
			return CodigoPagamento{}, fmt.Errorf("valor inválido no Pix: %q", v)
		}
		c.Valor = r*100 + ct
	}
	return c, nil
}

// PagarCodigo trata `prisma pagar boleto "<código>" [--desc] [--cat] [--venc] [--valor] [--conta N]`:
// lê o boleto ou Pix e cria a conta a pagar, com o código guardado na
// observação (para copiar na hora de pagar). Valor e vencimento vêm do código;
// --valor/--venc só são exigidos quando ele não os traz (Pix sem valor, conta
// de consumo sem vencimento: aí vale hoje).
func PagarCodigo(conn *sql.DB, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(`uso: prisma pagar boleto "<linha digitável ou Pix copia-e-cola>" [--desc] [--cat] [--venc] [--valor] [--conta N]`)
	}
	codigo := args[0]
	fs := flag.NewFlagSet("pagar boleto", flag.ContinueOnError)
	desc := fs.String("desc", "", "descrição (padrão: banco do boleto ou recebedor do Pix)")
	cat := fs.String("cat", "", "categoria (padrão: a regra de categoria, ou geral)")
	venc := fs.String("venc", "", "vencimento, quando o código não traz")
	valor := fs.String("valor", "", "valor, quando o código não traz")
	contaID := fs.Int64("conta", 0, "conta que vai pagar")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	c, err := LerCodigoPagamento(codigo)
	if err != nil {
		return err
	}
	var id int64
	if err := conn.QueryRow(`SELECT id FROM lancamentos WHERE observacao = ? LIMIT 1`, c.Codigo).Scan(&id); err == nil {
		return fmt.Errorf("esse código já foi cadastrado (lançamento #%d)", id)
	}
	if c.Valor == 0 {
		if *valor == "" {
			return errors.New("o código não traz o valor: informe --valor")
		}
		if c.Valor, err = money.Parse(*valor); err != nil {
			return err
		}
	}
	if c.Vencimento == "" || *venc != "" {
		v := *venc
		if v == "" {
			v = "hoje"
		}
		if c.Vencimento, err = parseData(v); err != nil {
			return err
		}
	}
	p := LancamentoParams{Tipo: "pagar", Desc: strings.TrimSpace(*desc), Valor: c.Valor, Cat: *cat,
		Venc: c.Vencimento, ContaID: *contaID, Obs: c.Codigo}
	if p.Desc == "" {
		p.Desc = c.Descricao()
	}
	criados, _, _, err := CriarLancamentos(conn, p)
	if err != nil {
		return err
	}
	fmt.Printf("Conta a pagar #%d criada: %s — %s, vence %s.\n", criados[0].ID, p.Desc, money.Format(c.Valor), dataBR(c.Vencimento))
	fmt.Println("O código fica na observação, para copiar na hora de pagar.")
	return nil
}
