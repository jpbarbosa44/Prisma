package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"

	"prisma/internal/money"
)

// Este arquivo concentra os dados dos gráficos. As funções exportadas devolvem
// séries já calculadas (em centavos) e são reaproveitadas tanto pela saída
// ASCII (terminal e web-como-texto) quanto pelo endpoint JSON da interface web,
// que desenha os gráficos em SVG. Todos os valores já refletem a divisão por
// grupo (valEf), como no resto do sistema.

// ParRotulo é um ponto rotulado de um gráfico (categoria, grupo, mês...).
type ParRotulo struct {
	Rotulo string `json:"rotulo"`
	Valor  int64  `json:"valor"`
}

// TrioMes traz receitas e despesas de um mês (AAAA-MM).
type TrioMes struct {
	Mes  string `json:"mes"`
	Rec  int64  `json:"rec"`
	Desp int64  `json:"desp"`
}

// GrupoGasto é o quanto um grupo movimentou: o total cheio e a sua parte.
type GrupoGasto struct {
	Nome  string `json:"nome"`
	Minha int64  `json:"minha"`
	Total int64  `json:"total"`
}

// janelaMeses devolve [início do primeiro mês, hoje] cobrindo `meses` meses.
func janelaMeses(meses int) (inicio, hoje string, refs []string) {
	agora := time.Now()
	primeiro := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -(meses - 1), 0)
	for i := 0; i < meses; i++ {
		refs = append(refs, primeiro.AddDate(0, i, 0).Format("2006-01"))
	}
	return primeiro.Format("2006-01-02"), agora.Format("2006-01-02"), refs
}

// DadosGraficos reúne as quatro séries — usado pelo endpoint JSON da web.
type DadosGraficos struct {
	Categorias []ParRotulo  `json:"categorias"`
	Saldo      []ParRotulo  `json:"saldo"`
	Mensal     []TrioMes    `json:"mensal"`
	Grupos     []GrupoGasto `json:"grupos"`
}

// GraficosDados calcula todas as séries de uma vez para os `meses` informados.
func GraficosDados(conn *sql.DB, meses int) (DadosGraficos, error) {
	if meses < 1 {
		meses = 1
	}
	if meses > 36 {
		meses = 36
	}
	inicio, hoje, _ := janelaMeses(meses)
	var d DadosGraficos
	var err error
	if d.Categorias, err = GastosPorCategoria(conn, inicio, hoje); err != nil {
		return d, err
	}
	if d.Saldo, err = SaldoMensal(conn, meses); err != nil {
		return d, err
	}
	if d.Mensal, err = ReceitaDespesaMensal(conn, meses); err != nil {
		return d, err
	}
	if d.Grupos, err = DespesaPorGrupo(conn); err != nil {
		return d, err
	}
	return d, nil
}

// GastosPorCategoria soma as despesas quitadas por categoria no período.
func GastosPorCategoria(conn *sql.DB, inicio, hoje string) ([]ParRotulo, error) {
	rows, err := conn.Query(`
		SELECT categoria, SUM(`+valEf("lancamentos")+`) AS t FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado'
		  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY categoria ORDER BY t DESC`, inicio, hoje)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParRotulo
	for rows.Next() {
		var p ParRotulo
		if err := rows.Scan(&p.Rotulo, &p.Valor); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReceitaDespesaMensal devolve receitas e despesas quitadas de cada mês da
// janela (meses sem movimento entram zerados, para o gráfico não ter buracos).
func ReceitaDespesaMensal(conn *sql.DB, meses int) ([]TrioMes, error) {
	inicio, hoje, refs := janelaMeses(meses)
	rows, err := conn.Query(`
		SELECT substr(COALESCE(data_compra, quitado_em),1,7) AS mes,
		       SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END),
		       SUM(CASE tipo WHEN 'pagar'   THEN `+valEf("lancamentos")+` ELSE 0 END)
		FROM lancamentos WHERE status = 'quitado'
		  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY mes`, inicio, hoje)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	porMes := map[string]TrioMes{}
	for rows.Next() {
		var t TrioMes
		if err := rows.Scan(&t.Mes, &t.Rec, &t.Desp); err != nil {
			return nil, err
		}
		porMes[t.Mes] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TrioMes, len(refs))
	for i, ref := range refs {
		out[i] = TrioMes{Mes: ref, Rec: porMes[ref].Rec, Desp: porMes[ref].Desp}
	}
	return out, nil
}

// SaldoMensal devolve o saldo total acumulado ao fim de cada mês da janela.
func SaldoMensal(conn *sql.DB, meses int) ([]ParRotulo, error) {
	inicio, hoje, refs := janelaMeses(meses)
	var base int64
	err := conn.QueryRow(`
		SELECT COALESCE((SELECT SUM(saldo_inicial) FROM contas), 0)
		     + COALESCE((SELECT SUM(saldo_inicial) FROM carteiras), 0)
		     + COALESCE((SELECT SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE -`+valEf("lancamentos")+` END)
		                 FROM lancamentos WHERE status = 'quitado' AND quitado_em < ?), 0)`,
		inicio).Scan(&base)
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(`
		SELECT substr(quitado_em,1,7) AS mes,
		       SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE -`+valEf("lancamentos")+` END)
		FROM lancamentos WHERE status = 'quitado' AND quitado_em >= ? AND quitado_em <= ?
		GROUP BY mes`, inicio, hoje)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	delta := map[string]int64{}
	for rows.Next() {
		var mes string
		var d int64
		if err := rows.Scan(&mes, &d); err != nil {
			return nil, err
		}
		delta[mes] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ParRotulo, len(refs))
	acc := base
	for i, ref := range refs {
		acc += delta[ref]
		out[i] = ParRotulo{Rotulo: ref, Valor: acc}
	}
	return out, nil
}

// DespesaPorGrupo lista, por grupo, a parte que coube a você e o total cheio
// das despesas vinculadas (quitadas ou não).
func DespesaPorGrupo(conn *sql.DB) ([]GrupoGasto, error) {
	rows, err := conn.Query(`
		SELECT g.nome, COALESCE(SUM(` + valEf("l") + `), 0), COALESCE(SUM(l.valor), 0)
		FROM grupos g JOIN lancamentos l ON l.grupo_id = g.id AND l.tipo = 'pagar'
		GROUP BY g.id, g.nome
		HAVING SUM(l.valor) > 0
		ORDER BY 2 DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GrupoGasto
	for rows.Next() {
		var g GrupoGasto
		if err := rows.Scan(&g.Nome, &g.Minha, &g.Total); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Graficos imprime os gráficos em ASCII: `prisma graficos [--meses N]`.
func Graficos(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("graficos", flag.ContinueOnError)
	meses := fs.Int("meses", 6, "período em meses (1 a 36)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *meses < 1 || *meses > 36 {
		return fmt.Errorf("--meses deve estar entre 1 e 36")
	}
	inicio, hoje, _ := janelaMeses(*meses)

	fmt.Printf("GRÁFICOS — %s a %s\n", dataBR(inicio), dataBR(hoje))
	for _, f := range []func(*sql.DB, int) error{
		GraficoCategorias, GraficoRecDesp, GraficoSaldo, GraficoGrupos,
	} {
		if err := f(conn, *meses); err != nil {
			return err
		}
	}
	return nil
}

// GraficoCategorias desenha a composição e o ranking de gastos por categoria.
// É uma das "abas" navegáveis por ←/→ na TUI (e parte do dump de `Graficos`).
func GraficoCategorias(conn *sql.DB, meses int) error {
	inicio, hoje, _ := janelaMeses(meses)
	cats, err := GastosPorCategoria(conn, inicio, hoje)
	if err != nil {
		return err
	}
	fmt.Println("\nGASTOS POR CATEGORIA")
	if len(cats) == 0 {
		fmt.Println("  (sem despesas quitadas no período)")
		return nil
	}
	var maior, total int64
	for _, c := range cats {
		total += c.Valor
		if c.Valor > maior {
			maior = c.Valor
		}
	}
	// composição: barra 100% empilhada + legenda com percentuais
	if barra, leg := barra100(cats, 40); barra != "" {
		fmt.Printf("  Composição  %s\n", barra)
		for _, l := range leg {
			fmt.Printf("              %s\n", l)
		}
		fmt.Println()
	}
	// barra colorida na última coluna: o ANSI tem largura zero na tela e,
	// por vir por último, não desalinha as colunas medidas pelo tabwriter.
	w := novaTabela()
	for _, c := range cats {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
			c.Rotulo, money.Format(c.Valor), pctStr(c.Valor, total), barraFinaCor(c.Valor, maior, 24, cCiano))
	}
	return w.Flush()
}

// GraficoRecDesp desenha receitas × despesas por mês como gráfico de linha, com
// a sparkline do líquido mês a mês.
func GraficoRecDesp(conn *sql.DB, meses int) error {
	rd, err := ReceitaDespesaMensal(conn, meses)
	if err != nil {
		return err
	}
	fmt.Println("\nRECEITAS vs DESPESAS POR MÊS")
	recs := make([]int64, len(rd))
	desps := make([]int64, len(rd))
	var totRec, totDesp int64
	for i, m := range rd {
		recs[i], desps[i] = m.Rec, m.Desp
		totRec += m.Rec
		totDesp += m.Desp
	}
	cap2 := fmt.Sprintf("%s  →  receitas %s · despesas %s · saldo do período %s",
		periodoRot(rd), money.Format(totRec), money.Format(totDesp), money.Format(totRec-totDesp))
	fmt.Println(graficoLinha(
		[][]float64{reaisSerie(recs), reaisSerie(desps)}, 9,
		[]asciigraph.AnsiColor{asciigraph.Green, asciigraph.Red},
		[]string{"Receitas", "Despesas"}, cap2))
	liq := make([]int64, len(rd))
	for i := range rd {
		liq[i] = rd[i].Rec - rd[i].Desp
	}
	fmt.Printf("  Líquido mês a mês: %s\n", pintar(cCiano, sparkline(liq)))
	return nil
}

// GraficoSaldo desenha a evolução do saldo acumulado como gráfico de linha.
func GraficoSaldo(conn *sql.DB, meses int) error {
	saldos, err := SaldoMensal(conn, meses)
	if err != nil {
		return err
	}
	fmt.Println("\nEVOLUÇÃO DO SALDO")
	serieSaldo := make([]int64, len(saldos))
	for i, s := range saldos {
		serieSaldo[i] = s.Valor
	}
	var cap3 string
	if n := len(saldos); n > 0 {
		delta := saldos[n-1].Valor - saldos[0].Valor
		cap3 = fmt.Sprintf("%s → %s  (variação %s)",
			money.Format(saldos[0].Valor), money.Format(saldos[n-1].Valor), money.Format(delta))
	}
	fmt.Println(graficoLinha(
		[][]float64{reaisSerie(serieSaldo)}, 9,
		[]asciigraph.AnsiColor{asciigraph.Blue}, nil, cap3))
	return nil
}

// GraficoGrupos desenha a despesa por grupo (sua parte sobre o total cheio).
func GraficoGrupos(conn *sql.DB, meses int) error {
	grupos, err := DespesaPorGrupo(conn)
	if err != nil {
		return err
	}
	fmt.Println("\nDESPESA POR GRUPO (sua parte █ do total cheio ░)")
	if len(grupos) == 0 {
		fmt.Println("  (nenhum grupo com despesas vinculadas)")
		return nil
	}
	var maiorG int64
	for _, g := range grupos {
		if g.Total > maiorG {
			maiorG = g.Total
		}
	}
	w := novaTabela()
	for _, g := range grupos {
		fmt.Fprintf(w, "  %s\t%s de %s\t%s\n",
			g.Nome, money.Format(g.Minha), money.Format(g.Total), barraParcialCor(g.Minha, g.Total, maiorG, 28))
	}
	return w.Flush()
}

// barraParcialCor é barraParcial com a sua parte (█) em ciano e o restante (░)
// em cinza; serve na última coluna do tabwriter (ANSI de largura zero na tela).
func barraParcialCor(parte, total, maior int64, largura int) string {
	runes := []rune(barraParcial(parte, total, maior, largura))
	i := 0
	for i < len(runes) && runes[i] != '░' {
		i++
	}
	return pintar(cCiano, string(runes[:i])) + pintar(cCinza, string(runes[i:]))
}

// periodoRot resume o intervalo de uma série mensal como "06/2025–06/2026".
func periodoRot(rd []TrioMes) string {
	if len(rd) == 0 {
		return ""
	}
	return mesBR(rd[0].Mes) + "–" + mesBR(rd[len(rd)-1].Mes)
}

// mesBR converte "2026-06" em "06/2026".
func mesBR(ref string) string {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return ref
	}
	return t.Format("01/2006")
}

// barraParcial mostra a parte cheia (█) sobre o restante até o total (░),
// com o comprimento total proporcional ao maior valor da série.
func barraParcial(parte, total, maior int64, largura int) string {
	comp := escala(total, maior, largura)
	cheia := 0
	if total > 0 {
		cheia = int(float64(parte) / float64(total) * float64(comp))
	}
	if cheia > comp {
		cheia = comp
	}
	return strings.Repeat("█", cheia) + strings.Repeat("░", comp-cheia)
}
