package app

import (
	"database/sql"
	"flag"
	"fmt"
	"sort"
	"time"

	"prisma/internal/money"
)

// Estatisticas faz uma análise estatística do histórico quitado:
// resumo por categoria (média/mediana/extremos/%), tendência e variação,
// top gastos e despesas recorrentes, e projeção/saúde financeira.
// `prisma estatisticas [--meses N]`.
func Estatisticas(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("estatisticas", flag.ContinueOnError)
	meses := fs.Int("meses", 6, "período analisado em meses (1 a 36)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *meses < 1 || *meses > 36 {
		return fmt.Errorf("--meses deve estar entre 1 e 36")
	}

	agora := time.Now()
	primeiro := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(*meses - 1), 0)
	inicio := primeiro.Format("2006-01-02")
	hoje := agora.Format("2006-01-02")

	// referências AAAA-MM da janela (para preencher meses sem gasto com zero)
	refs := make([]string, *meses)
	for i := 0; i < *meses; i++ {
		refs[i] = primeiro.AddDate(0, i, 0).Format("2006-01")
	}

	fmt.Printf("ESTATÍSTICAS — %s a %s (%d meses)\n", dataBR(inicio), dataBR(hoje), *meses)

	cats, err := estatPorCategoria(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	receitas, despesas, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	if len(cats) == 0 && somaInt(despesas) == 0 && somaInt(receitas) == 0 {
		fmt.Println("\nNenhum lançamento quitado no período.")
		return nil
	}

	imprimeResumoCategoria(cats)
	imprimeTendencia(cats, despesas, refs)
	if err := imprimeTopERecorrentes(conn, inicio, hoje); err != nil {
		return err
	}
	return imprimeSaude(conn, receitas, despesas, *meses)
}

// As funções EstatX abaixo expõem cada seção isoladamente, para a TUI alternar
// entre elas com ←/→ (abas). Recalculam a janela e os dados que precisam — barato
// e mantém cada aba independente. O dump completo continua em Estatisticas.

func estatCabecalho(meses int) (inicio, hoje string, refs []string) {
	inicio, hoje, refs = janelaMeses(meses)
	fmt.Printf("ESTATÍSTICAS — %s a %s (%d meses)\n", dataBR(inicio), dataBR(hoje), meses)
	return
}

// EstatResumo: resumo por categoria (total, média, mediana, extremos, %).
func EstatResumo(conn *sql.DB, meses int) error {
	inicio, hoje, refs := estatCabecalho(meses)
	cats, err := estatPorCategoria(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		fmt.Println("\nNenhuma despesa quitada no período.")
		return nil
	}
	imprimeResumoCategoria(cats)
	return nil
}

// EstatTendencia: variação do mês, média móvel e categorias acima da média.
func EstatTendencia(conn *sql.DB, meses int) error {
	inicio, hoje, refs := estatCabecalho(meses)
	cats, err := estatPorCategoria(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	_, despesas, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	imprimeTendencia(cats, despesas, refs)
	return nil
}

// EstatTopGastos: maiores lançamentos e gastos recorrentes do período.
func EstatTopGastos(conn *sql.DB, meses int) error {
	inicio, hoje, _ := estatCabecalho(meses)
	return imprimeTopERecorrentes(conn, inicio, hoje)
}

// EstatSaude: projeção do saldo e indicadores de saúde financeira.
func EstatSaude(conn *sql.DB, meses int) error {
	inicio, hoje, refs := estatCabecalho(meses)
	receitas, despesas, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	return imprimeSaude(conn, receitas, despesas, meses)
}

// estatCategoria guarda a série mensal de uma categoria e suas estatísticas.
type estatCategoria struct {
	cat                   string
	serie                 []int64 // um valor por mês da janela (0 onde não houve gasto)
	total, media, mediana int64
	maior, menor          int64
	maiorMes, menorMes    string
}

// estatPorCategoria monta a série mensal de cada categoria de despesa e calcula
// média, mediana e extremos.
func estatPorCategoria(conn *sql.DB, inicio, hoje string, refs []string) ([]estatCategoria, error) {
	rows, err := conn.Query(`
		SELECT categoria, substr(COALESCE(data_compra, quitado_em), 1, 7) AS mes, SUM(`+valEf("lancamentos")+`)
		FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado'
		      AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY categoria, mes`, inicio, hoje)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	idxRef := map[string]int{}
	for i, r := range refs {
		idxRef[r] = i
	}
	porCat := map[string][]int64{}
	for rows.Next() {
		var cat, mes string
		var total int64
		if err := rows.Scan(&cat, &mes, &total); err != nil {
			return nil, err
		}
		if _, ok := porCat[cat]; !ok {
			porCat[cat] = make([]int64, len(refs))
		}
		if i, ok := idxRef[mes]; ok {
			porCat[cat][i] = total
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var cats []estatCategoria
	for cat, serie := range porCat {
		e := estatCategoria{cat: cat, serie: serie}
		for i, v := range serie {
			e.total += v
			if v > e.maior {
				e.maior, e.maiorMes = v, refs[i]
			}
		}
		e.menor, e.menorMes = serie[0], refs[0]
		for i, v := range serie {
			if v < e.menor {
				e.menor, e.menorMes = v, refs[i]
			}
		}
		e.media = e.total / int64(len(serie))
		e.mediana = mediana(serie)
		cats = append(cats, e)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].total > cats[j].total })
	return cats, nil
}

// estatMensal devolve as séries de receita e despesa por mês da janela.
func estatMensal(conn *sql.DB, inicio, hoje string, refs []string) (receitas, despesas []int64, err error) {
	rows, err := conn.Query(`
		SELECT substr(COALESCE(data_compra, quitado_em), 1, 7) AS mes,
		       SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END),
		       SUM(CASE tipo WHEN 'pagar'   THEN `+valEf("lancamentos")+` ELSE 0 END)
		FROM lancamentos WHERE status = 'quitado'
		      AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY mes`, inicio, hoje)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	receitas = make([]int64, len(refs))
	despesas = make([]int64, len(refs))
	idxRef := map[string]int{}
	for i, r := range refs {
		idxRef[r] = i
	}
	for rows.Next() {
		var mes string
		var rec, desp int64
		if err := rows.Scan(&mes, &rec, &desp); err != nil {
			return nil, nil, err
		}
		if i, ok := idxRef[mes]; ok {
			receitas[i], despesas[i] = rec, desp
		}
	}
	return receitas, despesas, rows.Err()
}

func imprimeResumoCategoria(cats []estatCategoria) {
	if len(cats) == 0 {
		return
	}
	var totalGeral int64
	for _, c := range cats {
		totalGeral += c.total
	}
	fmt.Println("\n1) RESUMO POR CATEGORIA")
	w := novaTabela()
	fmt.Fprintln(w, "  CATEGORIA\tTOTAL\tMÉDIA/MÊS\tMEDIANA\tMAIOR MÊS\tMENOR MÊS\t% DO TOTAL")
	for _, c := range cats {
		pct := float64(0)
		if totalGeral > 0 {
			pct = float64(c.total) / float64(totalGeral) * 100
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s (%s)\t%s (%s)\t%.0f%%\n",
			c.cat, money.Format(c.total), money.Format(c.media), money.Format(c.mediana),
			money.Format(c.maior), mesBR(c.maiorMes), money.Format(c.menor), mesBR(c.menorMes), pct)
	}
	w.Flush()
}

func imprimeTendencia(cats []estatCategoria, despesas []int64, refs []string) {
	fmt.Println("\n2) TENDÊNCIA E VARIAÇÃO")
	n := len(despesas)
	if n >= 2 {
		ult, ant := despesas[n-1], despesas[n-2]
		fmt.Printf("  Despesa do mês (%s): %s", mesBR(refs[n-1]), money.Format(ult))
		if ant > 0 {
			fmt.Printf("  (%+.0f%% vs. %s)", float64(ult-ant)/float64(ant)*100, mesBR(refs[n-2]))
		}
		fmt.Println()
		// média móvel dos até 3 últimos meses
		jan := 3
		if n < jan {
			jan = n
		}
		var soma int64
		for _, v := range despesas[n-jan:] {
			soma += v
		}
		fmt.Printf("  Média móvel (%d meses): %s\n", jan, money.Format(soma/int64(jan)))
	}
	// categorias que dispararam: último mês acima de 1,5× a própria média
	var alertas []string
	for _, c := range cats {
		ult := c.serie[len(c.serie)-1]
		if c.media > 0 && ult > c.media*3/2 && ult > 0 {
			alertas = append(alertas, fmt.Sprintf("%s (%s, média %s)", c.cat, money.Format(ult), money.Format(c.media)))
		}
	}
	if len(alertas) > 0 {
		fmt.Println("  ⚠ Acima da média histórica neste mês:")
		for _, a := range alertas {
			fmt.Printf("    • %s\n", a)
		}
	} else {
		fmt.Println("  Nenhuma categoria muito acima da própria média neste mês.")
	}
}

func imprimeTopERecorrentes(conn *sql.DB, inicio, hoje string) error {
	fmt.Println("\n3) TOP GASTOS E RECORRENTES")
	rows, err := conn.Query(`
		SELECT descricao, `+valEf("lancamentos")+`, COALESCE(data_compra, quitado_em), categoria
		FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado'
		      AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		ORDER BY `+valEf("lancamentos")+` DESC LIMIT 5`, inicio, hoje)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Println("  Maiores lançamentos:")
	w := novaTabela()
	achou := false
	for rows.Next() {
		achou = true
		var desc, data, cat string
		var valor int64
		if err := rows.Scan(&desc, &valor, &data, &cat); err != nil {
			return err
		}
		fmt.Fprintf(w, "    %s\t%s\t%s\t[%s]\n", dataBR(data), desc, money.Format(valor), cat)
	}
	if achou {
		w.Flush()
	} else {
		fmt.Println("    (nenhum)")
	}

	// despesas repetidas (mesma descrição/valor em 3+ meses) que ainda não são
	// recorrência nem parcela — candidatas a virar recorrência.
	rec, err := conn.Query(`
		SELECT descricao, valor, COUNT(DISTINCT substr(COALESCE(data_compra, quitado_em), 1, 7)) AS m
		FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado' AND recorrencia_id IS NULL AND parcela_grupo IS NULL
		      AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY lower(descricao), valor HAVING m >= 3
		ORDER BY m DESC, valor DESC LIMIT 8`, inicio, hoje)
	if err != nil {
		return err
	}
	defer rec.Close()
	fmt.Println("  Gastos recorrentes (candidatos a recorrência):")
	achou = false
	w2 := novaTabela()
	for rec.Next() {
		achou = true
		var desc string
		var valor int64
		var m int
		if err := rec.Scan(&desc, &valor, &m); err != nil {
			return err
		}
		fmt.Fprintf(w2, "    %s\t%s\t%d meses\n", desc, money.Format(valor), m)
	}
	if achou {
		w2.Flush()
	} else {
		fmt.Println("    (nenhum padrão claro)")
	}
	return nil
}

func imprimeSaude(conn *sql.DB, receitas, despesas []int64, meses int) error {
	totRec, totDesp := somaInt(receitas), somaInt(despesas)
	sobra := totRec - totDesp
	mediaSobra := sobra / int64(meses)
	mediaDesp := totDesp / int64(meses)

	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}

	fmt.Println("\n4) PROJEÇÃO E SAÚDE FINANCEIRA")
	fmt.Printf("  Receitas: %s | Despesas: %s | Sobra: %s\n",
		money.Format(totRec), money.Format(totDesp), money.Format(sobra))
	if totRec > 0 {
		fmt.Printf("  Taxa de poupança: %.0f%%\n", float64(sobra)/float64(totRec)*100)
	}
	fmt.Printf("  Sobra média por mês: %s\n", money.Format(mediaSobra))
	fmt.Printf("  Saldo atual: %s\n", money.Format(saldo))
	if mediaSobra != 0 {
		fmt.Printf("  Projeção do saldo em 6 meses: %s\n", money.Format(saldo+mediaSobra*6))
	}
	if mediaDesp > 0 {
		folego := float64(saldo) / float64(mediaDesp)
		fmt.Printf("  Meses de fôlego (saldo ÷ despesa média): %.1f meses\n", folego)
	}
	return nil
}

// mediana devolve o valor central de uma série (média dos dois centrais se par).
func mediana(serie []int64) int64 {
	c := append([]int64(nil), serie...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	n := len(c)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func somaInt(v []int64) int64 {
	var s int64
	for _, x := range v {
		s += x
	}
	return s
}
