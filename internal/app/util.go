// Package app implementa os comandos da CLI do Prisma.
package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// parseData aceita "AAAA-MM-DD", "DD/MM/AAAA" e "DD/MM" (ano vigente) e retorna
// a data normalizada AAAA-MM-DD.
func parseData(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "hoje" {
		return time.Now().Format("2006-01-02"), nil
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	// DD/MM sem ano: assume o ano vigente.
	if t, err := time.Parse("02/01", s); err == nil {
		t = t.AddDate(time.Now().Year(), 0, 0)
		return t.Format("2006-01-02"), nil
	}
	return "", fmt.Errorf("data inválida: %q (use AAAA-MM-DD, DD/MM/AAAA ou DD/MM)", s)
}

// dataBR converte AAAA-MM-DD para DD/MM/AAAA para exibição.
func dataBR(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02/01/2006")
}

// periodo é um intervalo [Inicio, Fim) em datas AAAA-MM-DD.
type periodo struct {
	Inicio, Fim string
	Rotulo      string
}

// periodoMes converte "2026-06" no intervalo do mês.
func periodoMes(ref string) (periodo, error) {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return periodo{}, fmt.Errorf("referência de mês inválida: %q (use AAAA-MM)", ref)
	}
	ini := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	fim := ini.AddDate(0, 1, 0)
	return periodo{ini.Format("2006-01-02"), fim.Format("2006-01-02"), ref}, nil
}

// periodoSemana converte "2026-W24" (semana ISO) no intervalo segunda→domingo.
func periodoSemana(ref string) (periodo, error) {
	partes := strings.SplitN(strings.ToUpper(ref), "-W", 2)
	if len(partes) != 2 {
		return periodo{}, fmt.Errorf("referência de semana inválida: %q (use AAAA-Wnn)", ref)
	}
	ano, err1 := strconv.Atoi(partes[0])
	sem, err2 := strconv.Atoi(partes[1])
	if err1 != nil || err2 != nil || sem < 1 || sem > 53 {
		return periodo{}, fmt.Errorf("referência de semana inválida: %q (use AAAA-Wnn)", ref)
	}
	// 4 de janeiro sempre cai na semana ISO 1
	jan4 := time.Date(ano, 1, 4, 0, 0, 0, 0, time.UTC)
	diaSemana := int(jan4.Weekday())
	if diaSemana == 0 {
		diaSemana = 7 // domingo = 7 no padrão ISO
	}
	segundaSem1 := jan4.AddDate(0, 0, -(diaSemana - 1))
	ini := segundaSem1.AddDate(0, 0, (sem-1)*7)
	fim := ini.AddDate(0, 0, 7)
	return periodo{ini.Format("2006-01-02"), fim.Format("2006-01-02"), ref}, nil
}

// refAtual retorna a referência corrente para o período informado.
func refAtual(per string) string {
	agora := time.Now()
	if per == "semana" {
		ano, sem := agora.ISOWeek()
		return fmt.Sprintf("%d-W%02d", ano, sem)
	}
	return agora.Format("2006-01")
}

// resolvePeriodo interpreta uma referência ("2026-06" ou "2026-W24") no intervalo correspondente.
func resolvePeriodo(per, ref string) (periodo, error) {
	if ref == "" {
		ref = refAtual(per)
	}
	if per == "semana" {
		return periodoSemana(ref)
	}
	return periodoMes(ref)
}

// parseDataT converte AAAA-MM-DD em time.Time.
func parseDataT(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// somaDias soma n dias a uma data AAAA-MM-DD.
func somaDias(data string, n int) string {
	t, err := time.Parse("2006-01-02", data)
	if err != nil {
		return data
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// somaMeses soma n meses a uma data AAAA-MM-DD, travando o dia no fim do mês
// quando necessário (ex.: 31/01 + 1 mês = 28/02, não 03/03).
func somaMeses(data string, n int) string {
	t, err := time.Parse("2006-01-02", data)
	if err != nil {
		return data
	}
	primeiroDia := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	ultimoDia := primeiroDia.AddDate(0, 1, -1).Day()
	dia := t.Day()
	if dia > ultimoDia {
		dia = ultimoDia
	}
	return time.Date(primeiroDia.Year(), primeiroDia.Month(), dia, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// valEf devolve a expressão SQL do "valor efetivo" de um lançamento: quando ele
// está vinculado a um grupo, o valor é dividido pelo número de pessoas do grupo
// (a minha parte); sem grupo, é o valor cheio. Se recebe_pagamento estiver
// marcado, o valor já É a minha parte (o resto foi lançado como receita de
// reembolso por CriarLancamentos), então não divide de novo. t é o nome ou
// alias da tabela lancamentos na consulta (ex.: "lancamentos" ou "l") — sempre
// qualificado para não colidir com a coluna grupo_id da subconsulta.
func valEf(t string) string {
	return "(CASE WHEN " + t + ".grupo_id IS NULL OR " + t + ".recebe_pagamento = 1 THEN " + t + ".valor" +
		" ELSE " + t + ".valor / max(1, (SELECT COUNT(*) FROM grupo_pessoas gp WHERE gp.grupo_id = " + t + ".grupo_id)) END)"
}

// novaTabela cria um tabwriter para saída alinhada em colunas.
func novaTabela() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
}

// ouTraco devolve "-" quando o texto está vazio, para nenhuma célula das
// tabelas ficar em branco.
func ouTraco(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// truncar encurta um texto a no máximo n runas, terminando em "…".
func truncar(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// barra desenha uma barra de progresso textual de 20 colunas.
func barra(usado, total int64) string {
	if total <= 0 {
		return ""
	}
	pct := float64(usado) / float64(total)
	cheias := int(pct * 20)
	if cheias > 20 {
		cheias = 20
	}
	if cheias < 0 {
		cheias = 0
	}
	return "[" + strings.Repeat("█", cheias) + strings.Repeat("░", 20-cheias) + "]"
}
