package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	"prisma/internal/money"
)

// Previsao projeta o saldo total para os próximos meses.
//
// Modelo: para cada mês futuro, as receitas e despesas previstas são os
// lançamentos pendentes com vencimento no mês; quando um mês não tem nenhum
// lançamento agendado daquele tipo, usa-se a média dos últimos 3 meses de
// lançamentos quitados (marcado com "~" na tabela). Os aportes das
// emergências ativas entram como saída na coluna DÍVIDAS.
func Previsao(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("previsao", flag.ContinueOnError)
	meses := fs.Int("meses", 6, "quantos meses projetar (1 a 36)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *meses < 1 || *meses > 36 {
		return fmt.Errorf("--meses deve estar entre 1 e 36")
	}

	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return err
	}
	aportes, err := aportesEmergencias(conn)
	if err != nil {
		return err
	}

	fmt.Printf("PREVISÃO — saldo atual: %s\n", money.Format(saldo))
	fmt.Printf("Média (últimos 3 meses): receitas %s/mês, despesas %s/mês\n\n",
		money.Format(mediaRec), money.Format(mediaDesp))

	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tRECEITAS\tDESPESAS\tDÍVIDAS\tFLUXO\tSALDO PROJETADO")
	agora := time.Now()
	projetado := saldo
	alertaMes := ""
	var rotulos []string
	var saldos []int64
	for i := 1; i <= *meses; i++ {
		ref := agora.AddDate(0, i, 0).Format("2006-01")
		p, err := periodoMes(ref)
		if err != nil {
			return err
		}
		rec, recFonte, err := previstoMes(conn, "receber", p, mediaRec)
		if err != nil {
			return err
		}
		desp, despFonte, err := previstoMes(conn, "pagar", p, mediaDesp)
		if err != nil {
			return err
		}
		divida := aportes[i]
		fluxo := rec - desp - divida
		projetado += fluxo
		rotulo := dataBR(p.Inicio)[3:]
		fmt.Fprintf(w, "%s\t%s%s\t%s%s\t%s\t%s\t%s\n",
			rotulo, recFonte, money.Format(rec), despFonte, money.Format(desp),
			money.Format(divida), money.Format(fluxo), money.Format(projetado))
		rotulos = append(rotulos, rotulo)
		saldos = append(saldos, projetado)
		if projetado < 0 && alertaMes == "" {
			alertaMes = ref
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("\n(≈ = recorrência prevista, ainda não lançada; ~ = média histórica; DÍVIDAS = aportes das emergências ativas)")
	if alertaMes != "" {
		fmt.Printf("⚠ Atenção: o saldo projetado fica NEGATIVO a partir de %s.\n", alertaMes)
	}

	// gráfico de barras do saldo projetado
	fmt.Println("\nSALDO PROJETADO")
	maior := saldo
	if maior < 0 {
		maior = -maior
	}
	for _, s := range saldos {
		if s > maior {
			maior = s
		}
		if -s > maior {
			maior = -s
		}
	}
	g := novaTabela()
	fmt.Fprintf(g, "hoje\t%s\t%s\n", barraSaldo(saldo, maior), money.Format(saldo))
	for i, s := range saldos {
		fmt.Fprintf(g, "%s\t%s\t%s\n", rotulos[i], barraSaldo(s, maior), money.Format(s))
	}
	return g.Flush()
}

// barraSaldo desenha o saldo como barra: cheia (█) para positivo,
// hachurada (▒) para negativo.
func barraSaldo(valor, maior int64) string {
	v := valor
	ch := "█"
	if v < 0 {
		v, ch = -v, "▒"
	}
	return strings.Repeat(ch, escala(v, maior, 28))
}

func escala(valor, maior int64, largura int) int {
	if maior <= 0 || valor <= 0 {
		return 0
	}
	n := int(float64(valor) / float64(maior) * float64(largura))
	if n < 1 {
		n = 1
	}
	return n
}

// aportesEmergencias devolve, por mês futuro (1 = mês que vem), a soma dos
// pagamentos planejados das emergências ativas, simulando cada plano.
func aportesEmergencias(conn *sql.DB) (map[int]int64, error) {
	rows, err := conn.Query(
		`SELECT valor_total, juros_mes, aporte_mensal FROM emergencias WHERE status = 'ativa'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aportes := map[int]int64{}
	for rows.Next() {
		var valor, aporte int64
		var juros float64
		if err := rows.Scan(&valor, &juros, &aporte); err != nil {
			return nil, err
		}
		for _, p := range simulaPlano(valor, juros, aporte) {
			aportes[p.mes] += p.pago
		}
	}
	return aportes, nil
}

// previstoMes retorna o total previsto de um tipo no mês, em três camadas de
// confiança (refletidas no "fonte" para a tabela):
//
//	""  lançamentos já agendados — avulsos mais as recorrências materializadas
//	    (GerarRecorrencias só vai até 3 meses à frente);
//	"≈" além desse horizonte, deriva das recorrências cadastradas (vigentes no
//	    mês), somadas a eventuais avulsos lançados longe;
//	"~" sem nada conhecido, cai para a média histórica.
//
// Avulsos e recorrências materializadas são separados de propósito: dentro do
// horizonte as recorrências já estão nos lançamentos, então NÃO somamos as
// regras de novo (evita contar em dobro); fora dele, sim.
func previstoMes(conn *sql.DB, tipo string, p periodo, media int64) (int64, string, error) {
	val := valEf("lancamentos")
	var avulsos, materializadas int64
	var qtdMat int
	err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN recorrencia_id IS NULL     THEN `+val+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN recorrencia_id IS NOT NULL THEN `+val+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN recorrencia_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM lancamentos
		WHERE tipo = ? AND status = 'pendente' AND vencimento >= ? AND vencimento < ?`,
		tipo, p.Inicio, p.Fim,
	).Scan(&avulsos, &materializadas, &qtdMat)
	if err != nil {
		return 0, "", err
	}

	parteRec, fonte := materializadas, ""
	if qtdMat == 0 {
		// nenhuma recorrência materializada neste mês: ou não há regra, ou está
		// além do horizonte de materialização — projeta pelas regras vigentes
		regras, qtdReg, err := recorrenciasNoMes(conn, tipo, p.Rotulo)
		if err != nil {
			return 0, "", err
		}
		if qtdReg > 0 {
			parteRec, fonte = regras, "≈"
		}
	}

	total := avulsos + parteRec
	if total == 0 {
		return media, "~", nil
	}
	return total, fonte, nil
}

// saldoTotal = saldos iniciais de contas e carteiras + todos os lançamentos quitados.
func saldoTotal(conn *sql.DB) (int64, error) {
	var s int64
	err := conn.QueryRow(`
		SELECT COALESCE((SELECT SUM(saldo_inicial) FROM contas), 0)
		     + COALESCE((SELECT SUM(saldo_inicial) FROM carteiras), 0)
		     + COALESCE((SELECT SUM(CASE tipo WHEN 'receber' THEN ` + valEf("lancamentos") + ` ELSE -` + valEf("lancamentos") + ` END)
		                 FROM lancamentos WHERE status = 'quitado'), 0)`).Scan(&s)
	return s, err
}

// mediasHistoricas calcula a média mensal de receitas e despesas quitadas
// nos últimos 3 meses completos.
func mediasHistoricas(conn *sql.DB) (rec, desp int64, err error) {
	inicio := time.Now().AddDate(0, -3, 0).Format("2006-01-02")
	hoje := time.Now().Format("2006-01-02")
	err = conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END) / 3, 0),
		       COALESCE(SUM(CASE tipo WHEN 'pagar'   THEN `+valEf("lancamentos")+` ELSE 0 END) / 3, 0)
		FROM lancamentos
		WHERE status = 'quitado' AND quitado_em >= ? AND quitado_em <= ?`,
		inicio, hoje,
	).Scan(&rec, &desp)
	return rec, desp, err
}

// fluxoMensalEsperado devolve a receita e a despesa médias por mês previstas
// para os próximos 12 meses, usando previstoMes (lançamentos agendados →
// recorrências cadastradas → média histórica). Suaviza as recorrências anuais
// (que aparecem num mês só) ao distribuí-las pela janela. As médias históricas
// servem de fallback dentro de previstoMes quando um mês não tem nada conhecido.
func fluxoMensalEsperado(conn *sql.DB, mediaRec, mediaDesp int64) (rec, desp int64, err error) {
	const janela = 12
	agora := time.Now()
	var somaRec, somaDesp int64
	for i := 1; i <= janela; i++ {
		ref := agora.AddDate(0, i, 0).Format("2006-01")
		p, err := periodoMes(ref)
		if err != nil {
			return 0, 0, err
		}
		r, _, err := previstoMes(conn, "receber", p, mediaRec)
		if err != nil {
			return 0, 0, err
		}
		d, _, err := previstoMes(conn, "pagar", p, mediaDesp)
		if err != nil {
			return 0, 0, err
		}
		somaRec += r
		somaDesp += d
	}
	return somaRec / janela, somaDesp / janela, nil
}

// Saldo mostra a posição consolidada: contas, carteiras, pendências e total.
func Saldo(conn *sql.DB, args []string) error {
	fmt.Println("POSIÇÃO GERAL")
	fmt.Println()
	if err := contaListar(conn); err != nil {
		return err
	}
	fmt.Println()
	if err := carteiraListar(conn); err != nil {
		return err
	}

	total, err := saldoTotal(conn)
	if err != nil {
		return err
	}
	// pendências do mês atual — não de todo o futuro: as recorrências são
	// materializadas com meses de antecedência, então somar todos os pendentes
	// inflaria o total (pareceria a soma de vários meses). O recorte casa com a
	// tela Pagar/Receber, que também abre no mês corrente.
	mesAtual := time.Now().Format("2006-01")
	p, err := periodoMes(mesAtual)
	if err != nil {
		return err
	}
	var pendPagar, pendReceber int64
	err = conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
		FROM lancamentos WHERE status = 'pendente' AND vencimento >= ? AND vencimento < ?`,
		p.Inicio, p.Fim).Scan(&pendPagar, &pendReceber)
	if err != nil {
		return err
	}
	var dividas int64
	if err := conn.QueryRow(
		`SELECT COALESCE(SUM(valor_total), 0) FROM emergencias WHERE status = 'ativa'`).Scan(&dividas); err != nil {
		return err
	}

	mesBR := time.Now().Format("01/2006")
	fmt.Println()
	fmt.Printf("Saldo total:          %s\n", money.Format(total))
	fmt.Printf("Pendente a pagar:     %s   (mês %s)\n", money.Format(pendPagar), mesBR)
	fmt.Printf("Pendente a receber:   %s   (mês %s)\n", money.Format(pendReceber), mesBR)
	if dividas > 0 {
		fmt.Printf("Dívidas em emergência: %s\n", money.Format(dividas))
	}
	fmt.Printf("Posição líquida:      %s\n", money.Format(total-pendPagar+pendReceber-dividas))
	return nil
}
