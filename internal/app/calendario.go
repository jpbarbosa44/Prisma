package app

import (
	"database/sql"
	"flag"
	"fmt"
	"time"

	"prisma/internal/money"
)

// O calendário de contas: cada dia do mês com o que vence nele (compras no
// cartão viram uma linha por fatura, como nos lembretes) e o saldo previsto ao
// fim do dia, de hoje em diante. As recorrências já aparecem como lançamentos
// pendentes: o Prisma as materializa com 3 meses de antecedência.

// ItemCal é uma conta num dia do calendário.
type ItemCal struct {
	ID        int64  `json:"id"`   // 0 nas faturas
	Tipo      string `json:"tipo"` // pagar | receber | fatura
	Descricao string `json:"descricao"`
	Valor     int64  `json:"valor"`
	Quitado   bool   `json:"quitado"`
	CartaoID  int64  `json:"cartaoId,omitempty"`
}

// DiaCal é um dia do mês.
type DiaCal struct {
	Data    string    `json:"data"` // AAAA-MM-DD
	Itens   []ItemCal `json:"itens"`
	Pagar   int64     `json:"pagar"`   // pendente a pagar no dia
	Receber int64     `json:"receber"` // pendente a receber no dia
	// Saldo previsto ao fim do dia (só de hoje em diante; TemSaldo diz se há)
	Saldo    int64 `json:"saldo"`
	TemSaldo bool  `json:"temSaldo"`
}

// MesCalendario é o calendário de um mês.
type MesCalendario struct {
	Mes        string   `json:"mes"` // AAAA-MM
	Hoje       string   `json:"hoje"`
	Dias       []DiaCal `json:"dias"`
	Pagar      int64    `json:"pagar"`   // pendente no mês
	Receber    int64    `json:"receber"` // pendente no mês
	MenorSaldo int64    `json:"menorSaldo"`
	DiaMenor   string   `json:"diaMenor"` // "" quando não há dia com saldo previsto
}

// Calendario monta o mês (AAAA-MM; "" = o atual).
func Calendario(conn *sql.DB, mes string) (MesCalendario, error) {
	agora := relogio()
	if mes == "" {
		mes = agora.Format("2006-01")
	}
	ini, err := time.Parse("2006-01", mes)
	if err != nil {
		return MesCalendario{}, fmt.Errorf("mês inválido %q (use AAAA-MM)", mes)
	}
	hoje := agora.Format("2006-01-02")
	c := MesCalendario{Mes: mes, Hoje: hoje}
	fim := ini.AddDate(0, 1, -1)
	idx := map[string]int{}
	for d := ini; !d.After(fim); d = d.AddDate(0, 0, 1) {
		idx[d.Format("2006-01-02")] = len(c.Dias)
		c.Dias = append(c.Dias, DiaCal{Data: d.Format("2006-01-02"), Itens: []ItemCal{}})
	}

	rows, err := conn.Query(`
		SELECT l.id, l.tipo, l.descricao, `+valEf("l")+`, l.vencimento, l.status = 'quitado',
		       COALESCE(l.cartao_id, 0), COALESCE(k.nome, '')
		FROM lancamentos l LEFT JOIN cartoes k ON k.id = l.cartao_id
		WHERE l.vencimento >= ? AND l.vencimento <= ?
		ORDER BY l.vencimento, l.tipo DESC, l.id`, ini.Format("2006-01-02"), fim.Format("2006-01-02"))
	if err != nil {
		return c, err
	}
	faturas := map[string][2]int{} // cartão|dia → (dia, item)
	for rows.Next() {
		var it ItemCal
		var venc, cartao string
		if err := rows.Scan(&it.ID, &it.Tipo, &it.Descricao, &it.Valor, &venc, &it.Quitado, &it.CartaoID, &cartao); err != nil {
			rows.Close()
			return c, err
		}
		d := &c.Dias[idx[venc]]
		if !it.Quitado {
			if it.Tipo == "pagar" {
				d.Pagar += it.Valor
			} else {
				d.Receber += it.Valor
			}
		}
		if it.CartaoID != 0 && it.Tipo == "pagar" {
			chave := fmt.Sprint(it.CartaoID, "|", venc)
			if p, ok := faturas[chave]; ok {
				f := &c.Dias[p[0]].Itens[p[1]]
				f.Valor += it.Valor
				f.Quitado = f.Quitado && it.Quitado // a fatura só está paga com tudo pago
				continue
			}
			it.ID, it.Tipo, it.Descricao = 0, "fatura", "Fatura "+cartao
			faturas[chave] = [2]int{idx[venc], len(d.Itens)}
		}
		d.Itens = append(d.Itens, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}

	// saldo previsto: o de agora, mais o pendente que vence até o dia (o
	// atrasado conta como se fosse pago hoje)
	saldo, err := saldoTotal(conn)
	if err != nil {
		return c, err
	}
	var pendAte int64 // pendente com vencimento antes do 1º dia exibido com saldo
	primeiro := hoje
	if ini.Format("2006-01-02") > primeiro {
		primeiro = ini.Format("2006-01-02")
	}
	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE -`+valEf("lancamentos")+` END), 0)
		FROM lancamentos WHERE status = 'pendente' AND vencimento < ?`, primeiro).Scan(&pendAte); err != nil {
		return c, err
	}
	corrente := saldo + pendAte
	for i := range c.Dias {
		d := &c.Dias[i]
		if d.Data < hoje {
			continue
		}
		corrente += d.Receber - d.Pagar
		d.Saldo, d.TemSaldo = corrente, true
		if c.DiaMenor == "" || corrente < c.MenorSaldo {
			c.MenorSaldo, c.DiaMenor = corrente, d.Data
		}
	}
	for _, d := range c.Dias {
		c.Pagar += d.Pagar
		c.Receber += d.Receber
	}
	return c, nil
}

// CalendarioCmd trata `prisma calendario [--mes AAAA-MM]`: os dias com contas,
// semana a semana, e o saldo previsto.
func CalendarioCmd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("calendario", flag.ContinueOnError)
	mes := fs.String("mes", "", "mês AAAA-MM (padrão: o atual)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := Calendario(conn, *mes)
	if err != nil {
		return err
	}
	t, _ := time.Parse("2006-01", c.Mes)
	fmt.Printf("Calendário de %s/%d\n", mesPorExtenso(c.Mes+"-01"), t.Year())
	fmt.Printf("Pendente no mês: a pagar %s · a receber %s\n", money.Format(c.Pagar), money.Format(c.Receber))
	if c.DiaMenor != "" {
		alerta := ""
		if c.MenorSaldo < 0 {
			alerta = "  ⚠ fica negativo"
		}
		fmt.Printf("Menor saldo previsto: %s em %s%s\n", money.Format(c.MenorSaldo), dataBR(c.DiaMenor), alerta)
	}
	semana := -1
	algum := false
	for _, d := range c.Dias {
		if len(d.Itens) == 0 && d.Data != c.Hoje {
			continue
		}
		dt, _ := time.Parse("2006-01-02", d.Data)
		_, s := dt.ISOWeek()
		if s != semana {
			semana = s
			fmt.Println()
		}
		algum = true
		marca := "  "
		if d.Data == c.Hoje {
			marca = "▸ "
		}
		saldo := ""
		if d.TemSaldo && (len(d.Itens) > 0 || d.Data == c.Hoje) {
			saldo = "saldo previsto " + money.Format(d.Saldo)
			if d.Saldo < 0 {
				saldo += " ⚠"
			}
		}
		fmt.Printf("%s%s %s  %s\n", marca, diasSemana[dt.Weekday()], dt.Format("02/01"), saldo)
		if len(d.Itens) == 0 {
			fmt.Println("      (hoje: nada vence)")
		}
		for _, it := range d.Itens {
			id := "     "
			if it.ID != 0 {
				id = fmt.Sprintf("%-5d", it.ID)
			}
			tipo := it.Tipo
			if tipo == "fatura" {
				tipo = "pagar"
			}
			st := ""
			if it.Quitado {
				st = "  quitado"
			}
			fmt.Printf("      %s %-7s %-32s %14s%s\n", id, tipo, truncar(it.Descricao, 32), money.Format(it.Valor), st)
		}
	}
	if !algum {
		fmt.Println("\nNada vence neste mês.")
	}
	return nil
}
