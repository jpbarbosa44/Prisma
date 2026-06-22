package app

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"

	"prisma/internal/money"
)

// Prisma Analytics é o módulo somente-leitura de análise financeira
// (`prisma --analytics`). As funções abaixo só LEEM o banco (RNF01) e imprimem
// o resultado — a TUI do módulo captura essa saída para exibir. O cálculo pesado
// fica nas consultas SQL (RNF02), e a visualização usa barras ASCII (RNF03).

// analyticsJanela é a janela padrão de análise, em meses completos.
const analyticsJanela = 6

// AnalyticsHealthScore (RF01) calcula e exibe um índice 0–100 de saúde
// financeira a partir de três pilares: a taxa de poupança média, o nível do
// fundo de emergência (saldo ÷ despesa média mensal) e a constância do fluxo de
// caixa livre (quanto menos o líquido mensal oscila, melhor).
func AnalyticsHealthScore(conn *sql.DB) error {
	inicio, hoje, refs := janelaMeses(analyticsJanela)
	recs, desps, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	totRec, totDesp := somaInt(recs), somaInt(desps)
	meses := int64(len(refs))
	if meses == 0 {
		meses = 1
	}
	despMedia := totDesp / meses

	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}

	// 1) taxa de poupança: (receitas - despesas) / receitas; 20%+ = nota cheia
	poup := 0.0
	if totRec > 0 {
		poup = float64(totRec-totDesp) / float64(totRec)
	}
	nPoup := clamp01(poup/0.20) * 100

	// 2) fundo de emergência: meses de despesa cobertos pelo saldo; 6 = cheio
	cobertura := 0.0
	switch {
	case despMedia > 0:
		cobertura = float64(saldo) / float64(despMedia)
	case saldo > 0:
		cobertura = 6 // sem despesas no período: considera coberto
	}
	nFundo := clamp01(cobertura/6) * 100

	// 3) constância do fluxo de caixa livre
	nConst := constanciaScore(recs, desps)

	score := int(math.Round(0.40*nPoup + 0.35*nFundo + 0.25*nConst))
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	fmt.Println("HEALTH SCORE — Saúde Financeira")
	fmt.Printf("(janela: últimos %d meses)\n\n", len(refs))
	barra, regua := medidor(score, 40)
	fmt.Printf("  %s\n", barra)
	fmt.Printf("  %s %d/100 · %s\n", regua, score, rotuloSaude(score))
	fmt.Printf("   %s\n\n", reguaZonas(40))

	fmt.Println("Componentes:")
	linhaComp("Taxa de poupança", nPoup, fmt.Sprintf("%.0f%% da renda sobra", poup*100))
	linhaComp("Fundo de emergência", nFundo, fmt.Sprintf("%.1f meses de despesa no saldo", cobertura))
	linhaComp("Constância do fluxo", nConst, descConstancia(nConst))
	// líquido mês a mês resume a tendência por trás do score
	liq := make([]int64, len(recs))
	for i := range recs {
		liq[i] = recs[i] - desps[i]
	}
	fmt.Printf("\nLíquido mês a mês: %s\n", pintar(cCiano, sparkline(liq)))
	return nil
}

// AnalyticsRunway (RF04) projeta o saldo para 30/90/180 dias, mostra o burn rate
// (despesa média) e, se o fluxo livre é negativo, o runway: quantos meses o
// usuário tem até o saldo zerar.
//
// O fluxo mensal vem da média dos próximos 12 meses de previstoMes (a mesma
// lógica da Previsão): lançamentos agendados, recorrências cadastradas e, só na
// falta de ambos, a média histórica. Assim o salário e demais recorrências
// entram na conta mesmo além do horizonte de materialização, em vez de o runway
// olhar só o passado quitado e projetar negativo sem motivo.
func AnalyticsRunway(conn *sql.DB) error {
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return err
	}
	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}
	recMedia, despMedia, err := fluxoMensalEsperado(conn, mediaRec, mediaDesp)
	if err != nil {
		return err
	}
	liquido := recMedia - despMedia

	fmt.Println("RUNWAY — Projeção de Fluxo de Caixa")
	fmt.Println("(base: recorrências e contas agendadas; média histórica como complemento)")
	fmt.Println()
	fmt.Printf("  Saldo atual:        %s\n", money.Format(saldo))
	fmt.Printf("  Receita média/mês:  %s\n", money.Format(recMedia))
	fmt.Printf("  Despesa média/mês:  %s  (burn rate)\n", money.Format(despMedia))
	fmt.Printf("  Fluxo livre/mês:    %s\n\n", money.Format(liquido))

	fmt.Println("Projeção do saldo:")
	for _, h := range []struct {
		dias  int
		meses float64
	}{{30, 1}, {90, 3}, {180, 6}} {
		proj := saldo + int64(float64(liquido)*h.meses)
		fmt.Printf("  em %3d dias: %s\n", h.dias, money.Format(proj))
	}
	fmt.Println()

	// projeção mês a mês (0..6) como gráfico de linha
	proj := make([]int64, 7)
	for i := range proj {
		proj[i] = saldo + int64(float64(liquido)*float64(i))
	}
	cor := asciigraph.Blue
	if liquido < 0 {
		cor = asciigraph.Red
	}
	fmt.Println(graficoLinha([][]float64{reaisSerie(proj)}, 8,
		[]asciigraph.AnsiColor{cor}, nil, "saldo projetado — mês 0 (hoje) a mês 6"))
	fmt.Println()

	if liquido >= 0 {
		fmt.Println("Fluxo positivo: o saldo tende a crescer; sem risco de runway.")
		return nil
	}
	runway := float64(saldo) / float64(-liquido)
	if saldo <= 0 {
		fmt.Printf("⚠ Saldo já zerado/negativo com fluxo negativo (queima de %s/mês).\n", money.Format(-liquido))
		return nil
	}
	fmt.Printf("⚠ Fluxo negativo. Runway: %.1f meses até o saldo zerar (queima de %s/mês).\n",
		runway, money.Format(-liquido))
	return nil
}

// AnalyticsAssinaturasOcultas (RF06) varre o histórico atrás de despesas de
// mesmo valor que se repetem em vários meses e ainda não vêm de uma recorrência
// cadastrada — prováveis assinaturas. Exibe o impacto anual de cada uma.
func AnalyticsAssinaturasOcultas(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT descricao, valor, COUNT(DISTINCT substr(vencimento,1,7)) AS meses
		FROM lancamentos
		WHERE tipo = 'pagar' AND recorrencia_id IS NULL
		GROUP BY lower(descricao), valor
		HAVING meses >= 3
		ORDER BY valor * meses DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("ASSINATURAS OCULTAS — Recorrências prováveis")
	fmt.Println("(despesas de mesmo valor em 3+ meses, fora das recorrências cadastradas)")
	fmt.Println()

	w := novaTabela()
	fmt.Fprintln(w, "DESCRIÇÃO\tVALOR\tMESES\tIMPACTO/ANO")
	achou := false
	var totalAno int64
	for rows.Next() {
		achou = true
		var desc string
		var valor int64
		var meses int
		if err := rows.Scan(&desc, &valor, &meses); err != nil {
			return err
		}
		impacto := valor * 12
		totalAno += impacto
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", desc, money.Format(valor), meses, money.Format(impacto))
	}
	if !achou {
		fmt.Println("Nenhuma assinatura oculta detectada. 👍")
		return nil
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nImpacto anual somado das prováveis assinaturas: %s\n", money.Format(totalAno))
	return nil
}

// --- helpers de visualização e estatística ---

// clamp01 prende x ao intervalo [0,1].
func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}

// reguaZonas desenha a régua 0–25–50–75–100 alinhada à barra do medidor, para
// situar o score nas faixas Crítica/Atenção/Boa/Excelente.
func reguaZonas(largura int) string {
	r := []byte(strings.Repeat(" ", largura+3))
	for _, m := range []struct {
		pct int
		s   string
	}{{0, "0"}, {25, "25"}, {50, "50"}, {75, "75"}, {100, "100"}} {
		ini := m.pct * (largura - 1) / 100
		if ini+len(m.s) > len(r) { // encosta o último rótulo na borda direita
			ini = len(r) - len(m.s)
		}
		for i := 0; i < len(m.s); i++ {
			r[ini+i] = m.s[i]
		}
	}
	return string(r)
}

// rotuloSaude traduz um score 0–100 num rótulo qualitativo.
func rotuloSaude(score int) string {
	switch {
	case score >= 80:
		return "Excelente"
	case score >= 60:
		return "Boa"
	case score >= 40:
		return "Atenção"
	default:
		return "Crítica"
	}
}

// linhaComp imprime um componente do score: nome, medidor, nota e um detalhe.
func linhaComp(nome string, nota float64, detalhe string) {
	fmt.Printf("  %-22s %s %3.0f/100  %s\n", nome, barraFinaCor(int64(nota), 100, 16, corZona(int(nota))), nota, detalhe)
}

// constanciaScore mede a previsibilidade do fluxo livre mensal (receita -
// despesa) pelo coeficiente de variação (desvio padrão ÷ |média|): CV 0 = nota
// 100, CV ≥ 1 = nota 0.
func constanciaScore(recs, desps []int64) float64 {
	n := len(recs)
	if n == 0 {
		return 0
	}
	liq := make([]float64, n)
	var soma float64
	for i := range recs {
		liq[i] = float64(recs[i] - desps[i])
		soma += liq[i]
	}
	media := soma / float64(n)
	if media == 0 {
		return 50 // sem tendência clara
	}
	var varSum float64
	for _, v := range liq {
		d := v - media
		varSum += d * d
	}
	desvio := math.Sqrt(varSum / float64(n))
	cv := desvio / math.Abs(media)
	return clamp01(1-cv) * 100
}

// descConstancia descreve a nota de constância do fluxo.
func descConstancia(nota float64) string {
	switch {
	case nota >= 70:
		return "fluxo estável mês a mês"
	case nota >= 40:
		return "alguma oscilação no fluxo"
	default:
		return "fluxo irregular"
	}
}

// --- classificação de categorias (heurística por nome) ---

var (
	// gastos essenciais/fixos (pilar "necessidades" da regra 50/30/20)
	catsNecessidade = []string{"aluguel", "moradia", "condominio", "condomínio", "luz", "energia",
		"agua", "água", "gas", "gás", "internet", "telefone", "mercado", "supermercado", "feira",
		"saude", "saúde", "farmacia", "farmácia", "transporte", "combustivel", "combustível",
		"educacao", "educação", "escola", "faculdade", "seguro", "imposto"}
	// contas de utilidade da casa (RF11)
	catsUtilidade = []string{"luz", "energia", "agua", "água", "gas", "gás", "internet", "telefone"}
	// contas básicas para o índice de inflação pessoal (RF08)
	catsBasica = []string{"mercado", "supermercado", "feira", "luz", "energia",
		"agua", "água", "condominio", "condomínio"}
)

func contemAlguma(cat string, chaves []string) bool {
	c := strings.ToLower(cat)
	for _, k := range chaves {
		if strings.Contains(c, k) {
			return true
		}
	}
	return false
}

// ehNecessidade diz se a categoria é um gasto essencial (vs. estilo de vida).
func ehNecessidade(cat string) bool { return contemAlguma(cat, catsNecessidade) }

// likeCategorias monta um filtro SQL "lower(categoria) LIKE '%k%' OR ..." a
// partir de chaves CONSTANTES (sem entrada do usuário, sem risco de injeção).
func likeCategorias(chaves []string) string {
	partes := make([]string, len(chaves))
	for i, k := range chaves {
		partes[i] = "lower(categoria) LIKE '%" + k + "%'"
	}
	return "(" + strings.Join(partes, " OR ") + ")"
}

func pct(parte, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(parte) / float64(total) * 100
}

// AnalyticsRegra502030 (RF09) classifica o orçamento em Necessidades, Desejos e
// Poupança/Investimentos (o que sobra da renda) e compara com o padrão 50/30/20.
func AnalyticsRegra502030(conn *sql.DB) error {
	inicio, hoje, refs := janelaMeses(analyticsJanela)
	recs, _, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}
	renda := somaInt(recs)
	cats, err := GastosPorCategoria(conn, inicio, hoje)
	if err != nil {
		return err
	}
	var necess, desejo, totalDesp int64
	for _, c := range cats {
		totalDesp += c.Valor
		if ehNecessidade(c.Rotulo) {
			necess += c.Valor
		} else {
			desejo += c.Valor
		}
	}
	poup := renda - totalDesp // o que sobra é o que foi guardado

	fmt.Println("REGRA 50/30/20 — Auditoria do Orçamento")
	fmt.Printf("(janela: últimos %d meses · renda %s)\n\n", len(refs), money.Format(renda))
	if renda <= 0 {
		fmt.Println("Sem receitas no período para calcular as proporções.")
		return nil
	}
	linhaPilar("Necessidades", necess, renda, 50, cCiano)
	linhaPilar("Desejos", desejo, renda, 30, cMagen)
	linhaPilar("Poupança/Invest.", poup, renda, 20, cVerde)
	fmt.Println()

	pN, pP := pct(necess, renda), pct(poup, renda)
	switch {
	case pP < 0:
		fmt.Println("⚠ Você está gastando mais do que ganha: não há poupança no período.")
	case pN > 60:
		fmt.Println("⚠ Custos fixos altos (>60% da renda) estão apertando o orçamento.")
	case pP >= 20:
		fmt.Println("👍 Boa taxa de poupança: dentro ou acima do ideal de 20%.")
	default:
		fmt.Println("Dá para se aproximar do ideal aumentando a fatia guardada (meta: 20%).")
	}
	return nil
}

// linhaPilar imprime "Nome [barra] X%  (ideal Y%, R$ ...)", com um marcador ┊ na
// posição do percentual ideal para se ver de relance se a fatia passou da meta.
// A parte cheia sai em `cor`, o trilho em cinza e o marcador em amarelo.
func linhaPilar(nome string, parte, total int64, ideal int, cor string) {
	p := pct(parte, total)
	larg := 24
	v := int64(p)
	if v < 0 {
		v = 0
	}
	pos := ideal * (larg - 1) / 100
	var b strings.Builder
	for i, r := range []rune(barraFina(v, 100, larg)) {
		switch {
		case i == pos && (r == '░' || r == ' '):
			b.WriteString(pintar(cAmar, "┊"))
		case r == '░' || r == ' ':
			b.WriteString(pintar(cCinza, string(r)))
		default:
			b.WriteString(pintar(cor, string(r)))
		}
	}
	fmt.Printf("  %-18s %s %5.1f%%  (ideal %d%%, %s)\n", nome, b.String(), p, ideal, money.Format(parte))
}

// AnalyticsPatrimonio (RF10) mostra o patrimônio líquido (ativos − dívidas) e
// sua evolução mês a mês.
func AnalyticsPatrimonio(conn *sql.DB) error {
	ativos, err := saldoTotal(conn)
	if err != nil {
		return err
	}
	var dividas int64
	if err := conn.QueryRow(
		`SELECT COALESCE(SUM(valor_total),0) FROM emergencias WHERE status='ativa'`).Scan(&dividas); err != nil {
		return err
	}
	liquido := ativos - dividas

	fmt.Println("PATRIMÔNIO LÍQUIDO — Net Worth")
	fmt.Println()
	fmt.Printf("  Ativos (contas + carteiras):  %s\n", money.Format(ativos))
	fmt.Printf("  Dívidas (emergências ativas): %s\n", money.Format(dividas))
	fmt.Printf("  Patrimônio líquido:           %s\n\n", money.Format(liquido))

	saldos, err := SaldoMensal(conn, 12)
	if err != nil {
		return err
	}
	fmt.Println("Evolução (patrimônio líquido ao fim de cada mês):")
	serie := make([]int64, len(saldos))
	for i, s := range saldos {
		serie[i] = s.Valor - dividas
	}
	var capP string
	if n := len(serie); n > 0 {
		capP = fmt.Sprintf("%s → %s  (variação %s)",
			money.Format(serie[0]), money.Format(serie[n-1]), money.Format(serie[n-1]-serie[0]))
	}
	fmt.Println(graficoLinha([][]float64{reaisSerie(serie)}, 9,
		[]asciigraph.AnsiColor{asciigraph.Green}, nil, capP))
	fmt.Println("\n(o histórico de dívidas não é rastreado; aplica-se a dívida atual como referência)")
	return nil
}

// AnalyticsInflacao (RF08) calcula a inflação pessoal: compara o gasto com contas
// básicas no trimestre atual com o mesmo trimestre do ano anterior.
func AnalyticsInflacao(conn *sql.DB) error {
	soma := func(ini, fim string) (int64, error) {
		var s int64
		err := conn.QueryRow(`
			SELECT COALESCE(SUM(`+valEf("lancamentos")+`),0) FROM lancamentos
			WHERE tipo='pagar' AND status='quitado'
			  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
			  AND `+likeCategorias(catsBasica), ini, fim).Scan(&s)
		return s, err
	}
	agora := time.Now()
	aIni, aFim := agora.AddDate(0, -3, 0).Format("2006-01-02"), agora.Format("2006-01-02")
	bIni, bFim := agora.AddDate(-1, -3, 0).Format("2006-01-02"), agora.AddDate(-1, 0, 0).Format("2006-01-02")
	atual, err := soma(aIni, aFim)
	if err != nil {
		return err
	}
	anterior, err := soma(bIni, bFim)
	if err != nil {
		return err
	}

	fmt.Println("INFLAÇÃO PESSOAL — Custo de Vida Doméstico")
	fmt.Println("(contas básicas: mercado, luz, água, condomínio — trimestre atual x ano anterior)")
	fmt.Println()
	fmt.Printf("  Trimestre atual:        %s\n", money.Format(atual))
	fmt.Printf("  Mesmo trimestre (-1 ano): %s\n\n", money.Format(anterior))
	if anterior <= 0 {
		fmt.Println("Sem dados do ano anterior para comparar (precisa de ~15 meses de histórico).")
		return nil
	}
	inf := pct(atual-anterior, anterior)
	switch {
	case inf > 0:
		fmt.Printf("Seu custo de vida básico subiu %.1f%% em um ano. 📈\n", inf)
	case inf < 0:
		fmt.Printf("Seu custo de vida básico caiu %.1f%% em um ano. 📉\n", -inf)
	default:
		fmt.Println("Seu custo de vida básico ficou estável em um ano.")
	}
	return nil
}

// AnalyticsUtilidades (RF11) acompanha o consumo das contas de utilidade (luz,
// água, gás, internet): gasto mensal, proporção da renda e detecção de picos.
func AnalyticsUtilidades(conn *sql.DB) error {
	inicio, hoje, refs := janelaMeses(analyticsJanela)
	porMes := map[string]int64{}
	rows, err := conn.Query(`
		SELECT substr(COALESCE(data_compra, quitado_em),1,7) AS mes, SUM(`+valEf("lancamentos")+`)
		FROM lancamentos WHERE tipo='pagar' AND status='quitado'
		  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		  AND `+likeCategorias(catsUtilidade)+`
		GROUP BY mes`, inicio, hoje)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m string
		var v int64
		if err := rows.Scan(&m, &v); err != nil {
			rows.Close()
			return err
		}
		porMes[m] = v
	}
	rows.Close()
	recs, _, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return err
	}

	fmt.Println("EFICIÊNCIA DE UTILIDADES — Contas da Casa")
	fmt.Println("(luz, água, gás, internet)")
	fmt.Println()
	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tUTILIDADES\t% DA RENDA")
	serie := make([]int64, len(refs))
	for i, ref := range refs {
		serie[i] = porMes[ref]
		prop := "-"
		if recs[i] > 0 {
			prop = fmt.Sprintf("%.1f%%", pct(porMes[ref], recs[i]))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", mesBR(ref), money.Format(porMes[ref]), prop)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(serie) >= 2 {
		fmt.Printf("\nTendência (%s a %s): %s\n", mesBR(refs[0]), mesBR(refs[len(refs)-1]), pintar(cCiano, sparkline(serie)))
	}
	// detecção de pico: mês corrente x média dos anteriores
	if len(serie) >= 2 {
		atual := serie[len(serie)-1]
		var soma int64
		for _, v := range serie[:len(serie)-1] {
			soma += v
		}
		media := soma / int64(len(serie)-1)
		fmt.Println()
		if media > 0 && atual > media*125/100 {
			fmt.Printf("⚠ Pico: as utilidades deste mês estão %.0f%% acima da média dos meses anteriores.\n",
				pct(atual-media, media))
		} else if media > 0 {
			fmt.Println("👍 Consumo de utilidades dentro do padrão dos meses anteriores.")
		}
	}
	return nil
}

// AnalyticsAnomalias (RF02) compara os gastos do mês corrente, por categoria, com
// o histórico (média e desvio padrão dos meses anteriores) e alerta as categorias
// com comportamento atípico (acima do padrão).
func AnalyticsAnomalias(conn *sql.DB) error {
	_, _, refs := janelaMeses(7) // 6 meses anteriores + o corrente
	atual := refs[len(refs)-1]
	anteriores := refs[:len(refs)-1]

	// gasto por categoria e mês no período
	rows, err := conn.Query(`
		SELECT categoria, substr(COALESCE(data_compra, quitado_em),1,7) AS mes, SUM(`+valEf("lancamentos")+`)
		FROM lancamentos WHERE tipo='pagar' AND status='quitado'
		  AND substr(COALESCE(data_compra, quitado_em),1,7) >= ?
		GROUP BY categoria, mes`, refs[0])
	if err != nil {
		return err
	}
	porCat := map[string]map[string]int64{}
	for rows.Next() {
		var cat, mes string
		var v int64
		if err := rows.Scan(&cat, &mes, &v); err != nil {
			rows.Close()
			return err
		}
		if porCat[cat] == nil {
			porCat[cat] = map[string]int64{}
		}
		porCat[cat][mes] = v
	}
	rows.Close()

	fmt.Println("MODO ECONOMIA — Detecção de Anomalias")
	fmt.Printf("(mês corrente %s x média dos %d meses anteriores)\n\n", mesBR(atual), len(anteriores))

	var alertas []anomaliaAlerta
	for cat, serie := range porCat {
		vAtual := serie[atual]
		if vAtual == 0 {
			continue
		}
		// média e desvio dos meses anteriores (faltantes contam como 0)
		var soma float64
		for _, m := range anteriores {
			soma += float64(serie[m])
		}
		media := soma / float64(len(anteriores))
		if media <= 0 {
			continue // categoria nova: sem base de comparação
		}
		var varSum float64
		for _, m := range anteriores {
			d := float64(serie[m]) - media
			varSum += d * d
		}
		desvio := math.Sqrt(varSum / float64(len(anteriores)))
		// atípico: acima de 30% da média E acima de média + 1 desvio
		if float64(vAtual) > media*1.3 && float64(vAtual) > media+desvio {
			alertas = append(alertas, anomaliaAlerta{cat, vAtual, int64(media), pct(vAtual-int64(media), int64(media))})
		}
	}
	if len(alertas) == 0 {
		fmt.Println("Nenhuma anomalia: os gastos do mês seguem o padrão histórico. 👍")
		return nil
	}
	sortAlertasDesc(alertas)
	for _, a := range alertas {
		fmt.Printf("⚠ %s: %s neste mês — %.0f%% acima do padrão (média %s).\n",
			a.cat, money.Format(a.atual), a.acima, money.Format(a.media))
	}
	return nil
}

// AnalyticsSazonalidade (RF03) identifica padrões anuais: meses do calendário
// historicamente mais caros, comparando a média de cada mês com a média geral.
func AnalyticsSazonalidade(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT CAST(substr(COALESCE(data_compra, quitado_em),6,2) AS INTEGER) AS m,
		       SUM(` + valEf("lancamentos") + `),
		       COUNT(DISTINCT substr(COALESCE(data_compra, quitado_em),1,7))
		FROM lancamentos WHERE tipo='pagar' AND status='quitado'
		GROUP BY m`)
	if err != nil {
		return err
	}
	medias := map[int]int64{} // mês (1-12) -> média de gasto
	var somaMedias, n int64
	var maior int64
	for rows.Next() {
		var m int
		var total, ocorr int64
		if err := rows.Scan(&m, &total, &ocorr); err != nil {
			rows.Close()
			return err
		}
		if ocorr == 0 || m < 1 || m > 12 {
			continue
		}
		med := total / ocorr
		medias[m] = med
		somaMedias += med
		n++
		if med > maior {
			maior = med
		}
	}
	rows.Close()

	fmt.Println("SAZONALIDADE — Meses Historicamente Mais Caros")
	fmt.Println()
	if n == 0 {
		fmt.Println("Sem histórico de gastos para mapear a sazonalidade.")
		return nil
	}
	geral := somaMedias / n
	nomes := []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

	// mapa de calor: os 12 meses do calendário, intensidade ∝ gasto médio
	var calor, iniciais strings.Builder
	for m := 1; m <= 12; m++ {
		frac := 0.0
		if maior > 0 {
			frac = float64(medias[m]) / float64(maior)
		}
		calor.WriteString(pintar(corCalor(frac), shade(frac)+shade(frac)) + " ")
		iniciais.WriteString(nomes[m-1][:2] + " ")
	}
	fmt.Println("Mapa de calor (gasto médio por mês do calendário):")
	fmt.Println("  " + calor.String())
	fmt.Println("  " + iniciais.String())
	fmt.Println()

	for m := 1; m <= 12; m++ {
		med, ok := medias[m]
		if !ok {
			continue
		}
		marca, cor := "  ", cCiano
		if med > geral*115/100 {
			marca, cor = "⚠ ", cVermel // pico sazonal: 15%+ acima da média geral
		}
		fmt.Printf("%s%-4s %s %s\n", marca, nomes[m-1], barraFinaCor(med, maior, 22, cor), money.Format(med))
	}
	fmt.Printf("\nMédia geral mensal: %s\n", money.Format(geral))

	// alerta dos próximos 3 meses se forem picos sazonais
	var avisos []string
	for i := 1; i <= 3; i++ {
		prox := int(time.Now().AddDate(0, i, 0).Month())
		if med, ok := medias[prox]; ok && med > geral*115/100 {
			avisos = append(avisos, fmt.Sprintf("%s (~%s)", nomes[prox-1], money.Format(med)))
		}
	}
	if len(avisos) > 0 {
		fmt.Printf("\n⚠ A caminho de mês(es) historicamente caro(s): %s. Reserve com antecedência.\n",
			strings.Join(avisos, ", "))
	}
	return nil
}

// AnalyticsMetas (RF05) faz a engenharia reversa de uma meta: dada uma quantia e
// um prazo em meses, calcula a parcela mensal, cruza com o superávit médio e diz
// se é viável; se não, sugere categorias variáveis a cortar. args: [valor, prazo].
func AnalyticsMetas(conn *sql.DB, args []string) error {
	fmt.Println("METAS — Planejamento por Engenharia Reversa")
	fmt.Println()
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		fmt.Println("Use a ação \"definir meta\" (tecla m) para informar o valor e o prazo (meses).")
		fmt.Println("Ex.: R$ 50.000,00 em 24 meses → o módulo calcula a parcela e a viabilidade.")
		return nil
	}
	valor, err := money.Parse(args[0])
	if err != nil {
		return err
	}
	prazo, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || prazo < 1 {
		return fmt.Errorf("prazo inválido: informe o número de meses")
	}
	parcela := valor / int64(prazo)
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return err
	}
	recMedia, despMedia, err := fluxoMensalEsperado(conn, mediaRec, mediaDesp)
	if err != nil {
		return err
	}
	superavit := recMedia - despMedia

	fmt.Printf("  Meta: %s em %d meses\n", money.Format(valor), prazo)
	fmt.Printf("  Parcela mensal necessária: %s\n", money.Format(parcela))
	fmt.Printf("  Superávit mensal previsto: %s\n\n", money.Format(superavit))

	if superavit >= parcela {
		fmt.Printf("✓ Viável: o superávit cobre a parcela, com folga de %s/mês.\n", money.Format(superavit-parcela))
		return nil
	}
	fmt.Printf("⚠ Inviável no ritmo atual: faltam %s/mês.\n\n", money.Format(parcela-superavit))

	inicio, hoje, refs := janelaMeses(analyticsJanela)
	cats, err := GastosPorCategoria(conn, inicio, hoje)
	if err != nil {
		return err
	}
	meses := int64(len(refs))
	if meses == 0 {
		meses = 1
	}
	fmt.Println("Categorias variáveis que poderiam ser reduzidas (média mensal):")
	achou := false
	for _, c := range cats {
		if ehNecessidade(c.Rotulo) {
			continue
		}
		if m := c.Valor / meses; m > 0 {
			achou = true
			fmt.Printf("  • %-16s %s/mês\n", c.Rotulo, money.Format(m))
		}
	}
	if !achou {
		fmt.Println("  (sem categorias variáveis relevantes; considere aumentar a renda ou o prazo)")
	}
	if superavit > 0 {
		fmt.Printf("\nNo superávit atual, a meta levaria ~%d meses.\n", int(valor/superavit)+1)
	}
	return nil
}

// AnalyticsSimulador (RF07) é o sandbox what-if: aplica, em memória, uma perda de
// renda e/ou um aumento de despesa fixa mensais e recalcula o fluxo e o runway —
// sem tocar no banco (todas as simulações ocorrem em memória). args: [perda, extra].
func AnalyticsSimulador(conn *sql.DB, args []string) error {
	var perda, extra int64
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		v, err := money.Parse(args[0])
		if err != nil {
			return err
		}
		perda = v
	}
	if len(args) >= 2 && strings.TrimSpace(args[1]) != "" {
		v, err := money.Parse(args[1])
		if err != nil {
			return err
		}
		extra = v
	}
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return err
	}
	recMedia, despMedia, err := fluxoMensalEsperado(conn, mediaRec, mediaDesp)
	if err != nil {
		return err
	}
	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}

	fmt.Println("SIMULADOR — Cenários (What-If)")
	fmt.Println()
	if perda == 0 && extra == 0 {
		fmt.Println("Use a ação \"simular\" (tecla s) para informar uma perda de renda e/ou")
		fmt.Println("uma nova despesa fixa mensal. O fluxo e o runway são recalculados em")
		fmt.Println("memória — nada é gravado no banco.")
		fmt.Println()
	}
	recSim, despSim := recMedia-perda, despMedia+extra
	liqAtual, liqSim := recMedia-despMedia, recSim-despSim

	fmt.Printf("  Receita média/mês:  %s → %s\n", money.Format(recMedia), money.Format(recSim))
	fmt.Printf("  Despesa média/mês:  %s → %s\n", money.Format(despMedia), money.Format(despSim))
	fmt.Printf("  Fluxo livre/mês:    %s → %s\n\n", money.Format(liqAtual), money.Format(liqSim))

	runway := func(rotulo string, liq int64) {
		if liq >= 0 {
			fmt.Printf("  %s: fluxo positivo (sem runway)\n", rotulo)
			return
		}
		fmt.Printf("  %s: runway de %.1f meses (queima %s/mês)\n", rotulo, float64(saldo)/float64(-liq), money.Format(-liq))
	}
	runway("Atual  ", liqAtual)
	runway("Cenário", liqSim)
	if liqSim < 0 && liqAtual >= 0 {
		fmt.Println("\n⚠ O cenário leva o fluxo de positivo para negativo.")
	}
	return nil
}

// anomaliaAlerta é uma categoria com gasto atípico no mês corrente (RF02).
type anomaliaAlerta struct {
	cat          string
	atual, media int64
	acima        float64 // % acima da média histórica
}

// sortAlertasDesc ordena os alertas de anomalia do maior para o menor desvio.
func sortAlertasDesc(a []anomaliaAlerta) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].acima > a[j-1].acima; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
