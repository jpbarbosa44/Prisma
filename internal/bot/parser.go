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
