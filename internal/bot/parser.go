package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"prisma/internal/app"
	"prisma/internal/money"
)

// Gramática das mensagens de lançamento:
//
//	[+] valor [#categoria] [descrição...] [marcadores]
//
//	+ antes do valor   receita (a receber); sem +, gasto (a pagar)
//	#categoria         default "geral"
//	@data              @hoje @ontem @amanha @DD @DD/MM @DD/MM/AAAA (default hoje)
//	!                  já quitado (token isolado)
//	3x                 divide o total em 3 parcelas mensais
//	rep:6              repete o lançamento por 6 meses
//	conta:2 cart:1     vincula a conta ou carteira pelo id
//
// O que sobra vira descrição; sem descrição, usa o nome da categoria.

var (
	reParcelas = regexp.MustCompile(`^(\d{1,3})x$`)
	reRepetir  = regexp.MustCompile(`^rep:(\d{1,3})$`)
	reConta    = regexp.MustCompile(`^conta:(\d+)$`)
	reCarteira = regexp.MustCompile(`^(?:cart|carteira):(\d+)$`)
	reDia      = regexp.MustCompile(`^\d{1,2}$`)
	reDiaMes   = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})$`)
)

// parseMensagem interpreta uma mensagem de lançamento relativa à data `agora`.
func parseMensagem(msg string, agora time.Time) (app.LancamentoParams, error) {
	var p app.LancamentoParams
	p.Tipo = "pagar"
	p.Venc = agora.Format("2006-01-02")

	var desc []string
	temValor := false
	for _, tok := range strings.Fields(msg) {
		switch {
		case tok == "!":
			p.Quitado = true
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			p.Cat = strings.ToLower(strings.TrimPrefix(tok, "#"))
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			data, err := parseDataRelativa(strings.TrimPrefix(tok, "@"), agora)
			if err != nil {
				return p, err
			}
			p.Venc = data
		case reParcelas.MatchString(tok):
			n, _ := strconv.Atoi(reParcelas.FindStringSubmatch(tok)[1])
			p.Parcelas = n
		case reRepetir.MatchString(tok):
			n, _ := strconv.Atoi(reRepetir.FindStringSubmatch(tok)[1])
			p.Repetir = n
		case reConta.MatchString(tok):
			p.ContaID, _ = strconv.ParseInt(reConta.FindStringSubmatch(tok)[1], 10, 64)
		case reCarteira.MatchString(tok):
			p.CartID, _ = strconv.ParseInt(reCarteira.FindStringSubmatch(tok)[1], 10, 64)
		case !temValor && pareceValor(tok):
			bruto := tok
			if strings.HasPrefix(bruto, "+") {
				p.Tipo = "receber"
				bruto = bruto[1:]
			}
			centavos, err := money.Parse(bruto)
			if err != nil {
				return p, err
			}
			p.Valor = centavos
			temValor = true
		default:
			desc = append(desc, tok)
		}
	}

	if !temValor {
		return p, fmt.Errorf("não achei o valor na mensagem (ex.: 25,50 #mercado pão e leite)")
	}
	p.Desc = strings.Join(desc, " ")
	if p.Desc == "" {
		p.Desc = p.Cat
		if p.Desc == "" {
			p.Desc = "geral"
		}
	}
	return p, nil
}

var (
	reJuros   = regexp.MustCompile(`^(\d{1,3}(?:[.,]\d+)?)%$`)
	reEntrada = regexp.MustCompile(`^entrada:(.+)$`)
)

// parseSimulacao interpreta `/simular [descrição] <valor> [Nx] [J%] [entrada:V]`
// nos argumentos do comando `simular`. Ex.: "videogame 4000 12x 2%".
func parseSimulacao(resto string) ([]string, error) {
	var args, desc []string
	temValor := false
	for _, tok := range strings.Fields(resto) {
		switch {
		case reParcelas.MatchString(tok):
			args = append(args, "--parcelas", reParcelas.FindStringSubmatch(tok)[1])
		case reJuros.MatchString(tok):
			juros := strings.ReplaceAll(reJuros.FindStringSubmatch(tok)[1], ",", ".")
			args = append(args, "--juros", juros)
		case reEntrada.MatchString(tok):
			args = append(args, "--entrada", reEntrada.FindStringSubmatch(tok)[1])
		case !temValor && pareceValor(tok):
			args = append(args, "--valor", strings.TrimPrefix(tok, "+"))
			temValor = true
		default:
			desc = append(desc, tok)
		}
	}
	if !temValor {
		return nil, fmt.Errorf("não achei o valor da compra (ex.: /simular videogame 4000 12x)")
	}
	if d := strings.Join(desc, " "); d != "" {
		args = append(args, "--desc", d)
	}
	return args, nil
}

// correcao são os campos que uma mensagem `corrigir ...` quer mudar no
// último lançamento; ponteiros nil significam "não mexer".
type correcao struct {
	Valor   *int64
	Cat     *string
	Venc    *string
	Desc    *string
	Quitado bool
}

// parseCorrecao interpreta os tokens após "corrigir" com a mesma gramática
// dos lançamentos: valor, #categoria, @data, ! e texto livre (nova descrição).
func parseCorrecao(resto string, agora time.Time) (correcao, error) {
	var c correcao
	var desc []string
	for _, tok := range strings.Fields(resto) {
		switch {
		case tok == "!":
			c.Quitado = true
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			cat := strings.ToLower(strings.TrimPrefix(tok, "#"))
			c.Cat = &cat
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			data, err := parseDataRelativa(strings.TrimPrefix(tok, "@"), agora)
			if err != nil {
				return c, err
			}
			c.Venc = &data
		case c.Valor == nil && pareceValor(tok):
			centavos, err := money.Parse(strings.TrimPrefix(tok, "+"))
			if err != nil {
				return c, err
			}
			c.Valor = &centavos
		default:
			desc = append(desc, tok)
		}
	}
	if d := strings.Join(desc, " "); d != "" {
		c.Desc = &d
	}
	if c.Valor == nil && c.Cat == nil && c.Venc == nil && c.Desc == nil && !c.Quitado {
		return c, fmt.Errorf("nada para corrigir (ex.: corrigir 27,90 ou corrigir #mercado)")
	}
	return c, nil
}

// transferencia é o resultado de `transferir <valor> <origem> <destino> [descrição]`.
type transferencia struct {
	Valor    int64
	De, Para string // "conta:N" ou "carteira:N"
	Desc     string
}

var reLocal = regexp.MustCompile(`^(conta|cart|carteira):(\d+)$`)

// parseTransferencia interpreta `transferir 200 conta:1 cart:2 [descrição]`.
func parseTransferencia(resto string) (transferencia, error) {
	var t transferencia
	var desc []string
	temValor := false
	for _, tok := range strings.Fields(resto) {
		switch {
		case reLocal.MatchString(tok):
			m := reLocal.FindStringSubmatch(tok)
			local := m[1] + ":" + m[2]
			if m[1] == "cart" {
				local = "carteira:" + m[2]
			}
			if t.De == "" {
				t.De = local
			} else if t.Para == "" {
				t.Para = local
			}
		case !temValor && pareceValor(tok):
			centavos, err := money.Parse(tok)
			if err != nil {
				return t, err
			}
			t.Valor = centavos
			temValor = true
		default:
			desc = append(desc, tok)
		}
	}
	if !temValor || t.De == "" || t.Para == "" {
		return t, fmt.Errorf("uso: transferir <valor> <origem> <destino> (ex.: transferir 200 conta:1 cart:2)")
	}
	t.Desc = strings.Join(desc, " ")
	return t, nil
}

var meses = map[string]time.Month{
	"janeiro": 1, "fevereiro": 2, "marco": 3, "março": 3, "abril": 4,
	"maio": 5, "junho": 6, "julho": 7, "agosto": 8, "setembro": 9,
	"outubro": 10, "novembro": 11, "dezembro": 12,
}

var reUltimosMeses = regexp.MustCompile(`^(\d{1,2})m$`)

// parsePeriodoConsulta converte o período de uma consulta `#categoria [período]`
// nos filtros do comando lancamentos. Aceita: nome de mês ("maio"), "3m"
// (últimos 3 meses), "AAAA-MM" e "tudo"; vazio é o mês atual.
func parsePeriodoConsulta(s string, agora time.Time) ([]string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return []string{"--mes", agora.Format("2006-01")}, nil
	case s == "tudo":
		return nil, nil
	case reUltimosMeses.MatchString(s):
		n, _ := strconv.Atoi(reUltimosMeses.FindStringSubmatch(s)[1])
		de := agora.AddDate(0, -n, 0).Format("2006-01-02")
		return []string{"--de", de}, nil
	}
	if mes, ok := meses[s]; ok {
		ano := agora.Year()
		if mes > agora.Month() { // "dezembro" em junho = dezembro passado
			ano--
		}
		return []string{"--mes", fmt.Sprintf("%d-%02d", ano, mes)}, nil
	}
	if _, err := time.Parse("2006-01", s); err == nil {
		return []string{"--mes", s}, nil
	}
	return nil, fmt.Errorf("período inválido: %q (use um mês como \"maio\", \"3m\", \"2026-05\" ou \"tudo\")", s)
}

// pareceValor diz se o token é um número monetário (com + opcional), sem
// engolir datas tipo 12/06 nem palavras com dígitos.
func pareceValor(tok string) bool {
	s := strings.TrimPrefix(tok, "+")
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != ',' {
			return false
		}
	}
	_, err := money.Parse(s)
	return err == nil
}

// parseDataRelativa resolve o marcador @ em uma data AAAA-MM-DD.
func parseDataRelativa(s string, agora time.Time) (string, error) {
	s = strings.ToLower(s)
	switch s {
	case "hoje":
		return agora.Format("2006-01-02"), nil
	case "ontem":
		return agora.AddDate(0, 0, -1).Format("2006-01-02"), nil
	case "amanha", "amanhã":
		return agora.AddDate(0, 0, 1).Format("2006-01-02"), nil
	}
	if reDia.MatchString(s) {
		dia, _ := strconv.Atoi(s)
		return montaData(agora.Year(), int(agora.Month()), dia)
	}
	if m := reDiaMes.FindStringSubmatch(s); m != nil {
		dia, _ := strconv.Atoi(m[1])
		mes, _ := strconv.Atoi(m[2])
		return montaData(agora.Year(), mes, dia)
	}
	for _, layout := range []string{"02/01/2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("data inválida: @%s (use @hoje, @15, @15/07 ou @15/07/2026)", s)
}

func montaData(ano, mes, dia int) (string, error) {
	if mes < 1 || mes > 12 || dia < 1 || dia > 31 {
		return "", fmt.Errorf("data inválida: %02d/%02d", dia, mes)
	}
	t := time.Date(ano, time.Month(mes), dia, 0, 0, 0, 0, time.UTC)
	if t.Day() != dia { // dia 31 num mês de 30, 30/02 etc.
		return "", fmt.Errorf("o mês %02d não tem dia %d", mes, dia)
	}
	return t.Format("2006-01-02"), nil
}
