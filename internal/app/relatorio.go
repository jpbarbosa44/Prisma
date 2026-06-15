package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	"prisma/internal/money"
)

// Relatorio analisa o passado: gastos por categoria (com barras) e o
// balanço mês a mês: `prisma relatorio [--meses N]`.
func Relatorio(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("relatorio", flag.ContinueOnError)
	meses := fs.Int("meses", 6, "período analisado em meses (1 a 36)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *meses < 1 || *meses > 36 {
		return fmt.Errorf("--meses deve estar entre 1 e 36")
	}

	agora := time.Now()
	inicio := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -(*meses - 1), 0).Format("2006-01-02")
	hoje := agora.Format("2006-01-02")

	var totRec, totDesp int64
	err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
		FROM lancamentos WHERE status = 'quitado' AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?`,
		inicio, hoje).Scan(&totRec, &totDesp)
	if err != nil {
		return err
	}

	fmt.Printf("RELATÓRIO — %s a %s\n\n", dataBR(inicio), dataBR(hoje))
	fmt.Printf("Receitas: %s | Despesas: %s | Resultado: %s",
		money.Format(totRec), money.Format(totDesp), money.Format(totRec-totDesp))
	if totRec > 0 {
		fmt.Printf(" | Poupança: %.0f%%", float64(totRec-totDesp)/float64(totRec)*100)
	}
	fmt.Println()

	if totRec == 0 && totDesp == 0 {
		fmt.Println("\nNenhum lançamento quitado no período.")
		return nil
	}

	// gastos por categoria, com barra proporcional ao maior gasto
	rows, err := conn.Query(`
		SELECT categoria, SUM(`+valEf("lancamentos")+`) FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado' AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY categoria ORDER BY SUM(`+valEf("lancamentos")+`) DESC`, inicio, hoje)
	if err != nil {
		return err
	}
	defer rows.Close()

	type linhaCat struct {
		cat   string
		total int64
	}
	var cats []linhaCat
	var maior int64
	for rows.Next() {
		var l linhaCat
		if err := rows.Scan(&l.cat, &l.total); err != nil {
			return err
		}
		if l.total > maior {
			maior = l.total
		}
		cats = append(cats, l)
	}
	if len(cats) > 0 {
		fmt.Println("\nGASTOS POR CATEGORIA")
		w := novaTabela()
		for _, l := range cats {
			pct := float64(0)
			if totDesp > 0 {
				pct = float64(l.total) / float64(totDesp) * 100
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%.0f%%\n", l.cat, money.Format(l.total), barraEscala(l.total, maior, 24), pct)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}

	// balanço mês a mês
	rows2, err := conn.Query(`
		SELECT substr(COALESCE(data_compra, quitado_em), 1, 7) AS mes,
		       SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END),
		       SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END)
		FROM lancamentos WHERE status = 'quitado' AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY mes ORDER BY mes`, inicio, hoje)
	if err != nil {
		return err
	}
	defer rows2.Close()

	fmt.Println("\nMÊS A MÊS")
	w := novaTabela()
	fmt.Fprintln(w, "  MÊS\tRECEITAS\tDESPESAS\tRESULTADO")
	for rows2.Next() {
		var mes string
		var rec, desp int64
		if err := rows2.Scan(&mes, &rec, &desp); err != nil {
			return err
		}
		t, _ := time.Parse("2006-01", mes)
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
			t.Format("01/2006"), money.Format(rec), money.Format(desp), money.Format(rec-desp))
	}
	return w.Flush()
}

// barraEscala desenha uma barra proporcional a valor/maior com a largura dada.
func barraEscala(valor, maior int64, largura int) string {
	if maior <= 0 {
		return ""
	}
	n := int(float64(valor) / float64(maior) * float64(largura))
	if n < 1 && valor > 0 {
		n = 1
	}
	return strings.Repeat("█", n)
}

// Extrato mostra a movimentação de uma conta ou carteira com saldo corrente:
// `prisma extrato --conta 1 [--meses 3]` (ou --carteira).
func Extrato(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("extrato", flag.ContinueOnError)
	contaID := fs.Int64("conta", 0, "id da conta")
	cartID := fs.Int64("carteira", 0, "id da carteira")
	meses := fs.Int("meses", 3, "período em meses (1 a 36)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*contaID == 0) == (*cartID == 0) {
		return fmt.Errorf("informe --conta OU --carteira")
	}
	if *meses < 1 || *meses > 36 {
		return fmt.Errorf("--meses deve estar entre 1 e 36")
	}
	tipo, id := "conta", *contaID
	if *cartID != 0 {
		tipo, id = "carteira", *cartID
	}
	tabela := tipo + "s"
	var nome string
	var saldoInicial int64
	err := conn.QueryRow(`SELECT nome, saldo_inicial FROM `+tabela+` WHERE id = ?`, id).
		Scan(&nome, &saldoInicial)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s #%d não encontrada", tipo, id)
	}
	if err != nil {
		return err
	}

	agora := time.Now()
	inicio := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -(*meses - 1), 0).Format("2006-01-02")

	// todos os movimentos em ordem; o saldo acumula desde o início da conta
	rows, err := conn.Query(`
		SELECT data, delta, descr FROM (
			SELECT l.quitado_em AS data,
			       CASE l.tipo WHEN 'receber' THEN `+valEf("l")+` ELSE -`+valEf("l")+` END AS delta,
			       l.descricao || ' [' || l.categoria || ']' AS descr
			FROM lancamentos l WHERE l.`+tipo+`_id = ? AND l.status = 'quitado'
			UNION ALL
			SELECT t.data, t.valor, '⇄ ' || t.descricao
			FROM transferencias t WHERE t.destino_tipo = ? AND t.destino_id = ?
			UNION ALL
			SELECT t.data, -t.valor, '⇄ ' || t.descricao
			FROM transferencias t WHERE t.origem_tipo = ? AND t.origem_id = ?
		) ORDER BY data, descr`,
		id, tipo, id, tipo, id)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Printf("EXTRATO — %s %s (desde %s)\n\n", tipo, nome, dataBR(inicio))
	w := novaTabela()
	fmt.Fprintln(w, "DATA\tDESCRIÇÃO\tVALOR\tSALDO")
	saldo := saldoInicial
	movimentos := 0
	for rows.Next() {
		var data, descr string
		var delta int64
		if err := rows.Scan(&data, &delta, &descr); err != nil {
			return err
		}
		saldo += delta
		if data < inicio {
			continue
		}
		movimentos++
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", dataBR(data), descr, money.Format(delta), money.Format(saldo))
	}
	if movimentos == 0 {
		fmt.Println("Nenhuma movimentação no período.")
	} else if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nSaldo atual: %s\n", money.Format(saldo))
	return nil
}
