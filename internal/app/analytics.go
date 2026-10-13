package app

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/guptarohit/asciigraph"

	"prisma/internal/money"
)

// Prisma Analytics é o módulo somente-leitura de análise financeira
// (`prisma --analytics`). As funções abaixo só LEEM o banco (RNF01) e imprimem
// o resultado — a TUI do módulo captura essa saída para exibir. O cálculo pesado
// fica nas consultas SQL (RNF02), e a visualização usa barras ASCII (RNF03).
//
// Padrão das análises (as que ainda não o seguem migram aos poucos):
//
//	type X struct{...}                              // os dados, sem formatação
//	func CalcX(conn *sql.DB, o Opcoes) (X, error)   // só consulta e conta
//	func (x X) Imprimir()                           // só desenha (fmt + cores)
//	func AnalyticsX(conn *sql.DB) error             // CalcX + Imprimir (TUI/web/MCP)
//
// Separar os dados do desenho deixa uma análise reaproveitar outra (o painel
// Hoje junta várias), e os testes conferem valores em vez de texto.

// analyticsJanela é a janela padrão de análise, em meses completos.
const analyticsJanela = 6

// Opcoes ajusta uma análise. O valor zero reproduz o padrão de cada uma.
type Opcoes struct {
	Meses int // janela em meses (0 = padrão da análise)
}

// meses devolve a janela pedida, ou o padrão, presa a [1, 36].
func (o Opcoes) meses(padrao int) int {
	m := o.Meses
	if m <= 0 {
		m = padrao
	}
	if m > 36 {
		m = 36
	}
	return m
}

// relogio é o "agora" das análises e das janelas de tempo; os testes o fixam
// para exercitar datas de borda (dia 1º, fim de mês, virada de ano).
var relogio = time.Now

// gastoPorCategoriaMes soma as despesas quitadas por categoria e mês de
// competência (data da compra no cartão, senão a quitação), dos meses deMes a
// ateMes (AAAA-MM, inclusive). É a régua única do gasto mensal das análises.
func gastoPorCategoriaMes(conn *sql.DB, deMes, ateMes string) (map[string]map[string]int64, error) {
	rows, err := conn.Query(`
		SELECT categoria, substr(COALESCE(data_compra, quitado_em),1,7) AS mes, SUM(`+valEf("lancamentos")+`)
		FROM lancamentos WHERE tipo='pagar' AND status='quitado'
		  AND substr(COALESCE(data_compra, quitado_em),1,7) BETWEEN ? AND ?
		GROUP BY categoria, mes`, deMes, ateMes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	porCat := map[string]map[string]int64{}
	for rows.Next() {
		var cat, mes string
		var v int64
		if err := rows.Scan(&cat, &mes, &v); err != nil {
			return nil, err
		}
		if porCat[cat] == nil {
			porCat[cat] = map[string]int64{}
		}
		porCat[cat][mes] = v
	}
	return porCat, rows.Err()
}

// LancamentoResumo é um lançamento citado como evidência de uma análise.
type LancamentoResumo struct {
	Data      string // competência, AAAA-MM-DD
	Descricao string
	Valor     int64 // valor efetivo (a sua parte, em grupo)
}

// lancamentosDoGasto devolve as n maiores despesas quitadas da categoria na
// competência deMes..ateMes — os lançamentos por trás de um alerta.
func lancamentosDoGasto(conn *sql.DB, cat, deMes, ateMes string, n int) ([]LancamentoResumo, error) {
	rows, err := conn.Query(`
		SELECT COALESCE(data_compra, quitado_em) AS d, descricao, `+valEf("lancamentos")+` AS v
		FROM lancamentos WHERE tipo='pagar' AND status='quitado' AND categoria = ?
		  AND substr(COALESCE(data_compra, quitado_em),1,7) BETWEEN ? AND ?
		ORDER BY v DESC, d DESC LIMIT ?`, cat, deMes, ateMes, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LancamentoResumo
	for rows.Next() {
		var l LancamentoResumo
		if err := rows.Scan(&l.Data, &l.Descricao, &l.Valor); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AnalyticsHealthScore (RF01) calcula e exibe um índice 0–100 de saúde
// financeira a partir de três pilares: a taxa de poupança média, o nível do
// fundo de emergência (saldo ÷ despesa média mensal) e a constância do fluxo de
// caixa livre (quanto menos o líquido mensal oscila, melhor).
func AnalyticsHealthScore(conn *sql.DB) error {
	h, err := CalcHealthScore(conn, Opcoes{})
	if err != nil {
		return err
	}
	h.Imprimir()
	return nil
}

// HealthScore é o resultado do RF01.
type HealthScore struct {
	Meses          int     // meses efetivos da janela (a partir do primeiro com movimento)
	Score          int     // 0–100
	NotaPoupanca   float64 // 0–100
	NotaFundo      float64 // 0–100
	NotaConstancia float64 // 0–100
	TaxaPoupanca   float64 // fração da renda que sobrou (pode ser negativa)
	Cobertura      float64 // meses de despesa cobertos pelo saldo
	Liquido        []int64 // receita − despesa, mês a mês
}

// CalcHealthScore calcula o RF01 sobre os últimos o.Meses meses (padrão 6).
func CalcHealthScore(conn *sql.DB, o Opcoes) (HealthScore, error) {
	inicio, hoje, refs := janelaMeses(o.meses(analyticsJanela))
	recs, desps, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return HealthScore{}, err
	}
	// a janela útil começa no primeiro mês com movimento: contar os meses de
	// antes do uso do app como "gasto zero" subestimaria a despesa média (e
	// inflaria o fundo de emergência) para quem tem pouco histórico
	if ini := primeiroComDado(recs, desps); ini > 0 && ini < len(refs) {
		recs, desps, refs = recs[ini:], desps[ini:], refs[ini:]
	}
	totRec, totDesp := somaInt(recs), somaInt(desps)
	meses := int64(len(refs))
	if meses == 0 {
		meses = 1
	}
	despMedia := totDesp / meses

	saldo, err := saldoTotal(conn)
	if err != nil {
		return HealthScore{}, err
	}

	h := HealthScore{Meses: len(refs)}
	// 1) taxa de poupança: (receitas - despesas) / receitas; 20%+ = nota cheia
	if totRec > 0 {
		h.TaxaPoupanca = float64(totRec-totDesp) / float64(totRec)
	}
	h.NotaPoupanca = clamp01(h.TaxaPoupanca/0.20) * 100

	// 2) fundo de emergência: meses de despesa cobertos pelo saldo; 6 = cheio
	switch {
	case despMedia > 0:
		h.Cobertura = float64(saldo) / float64(despMedia)
	case saldo > 0:
		h.Cobertura = 6 // sem despesas no período: considera coberto
	}
	h.NotaFundo = clamp01(h.Cobertura/6) * 100

	// 3) constância do fluxo de caixa livre
	h.NotaConstancia = constanciaScore(recs, desps)

	h.Score = int(math.Round(0.40*h.NotaPoupanca + 0.35*h.NotaFundo + 0.25*h.NotaConstancia))
	if h.Score > 100 {
		h.Score = 100
	}
	if h.Score < 0 {
		h.Score = 0
	}
	// líquido mês a mês resume a tendência por trás do score
	h.Liquido = make([]int64, len(recs))
	for i := range recs {
		h.Liquido[i] = recs[i] - desps[i]
	}
	return h, nil
}

// Imprimir desenha o Health Score: medidor, componentes e o líquido mensal.
func (h HealthScore) Imprimir() {
	fmt.Println("HEALTH SCORE — Saúde Financeira")
	fmt.Printf("(janela: últimos %d meses)\n\n", h.Meses)
	barra, regua := medidor(h.Score, 40)
	fmt.Printf("  %s\n", barra)
	fmt.Printf("  %s %d/100 · %s\n", regua, h.Score, rotuloSaude(h.Score))
	fmt.Printf("   %s\n\n", reguaZonas(40))

	fmt.Println("Componentes:")
	linhaComp("Taxa de poupança", h.NotaPoupanca, fmt.Sprintf("%.0f%% da renda sobra", h.TaxaPoupanca*100))
	linhaComp("Fundo de emergência", h.NotaFundo, fmt.Sprintf("%.1f meses de despesa no saldo", h.Cobertura))
	linhaComp("Constância do fluxo", h.NotaConstancia, descConstancia(h.NotaConstancia))
	fmt.Printf("\nLíquido mês a mês: %s\n", pintar(cCiano, sparkline(h.Liquido)))
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
	r, err := CalcRunway(conn, Opcoes{})
	if err != nil {
		return err
	}
	r.Imprimir()
	return nil
}

// Runway é o resultado do RF04.
type Runway struct {
	Saldo      int64
	ReceitaMes int64   // receita esperada por mês
	DespesaMes int64   // despesa esperada por mês (burn rate)
	Liquido    int64   // fluxo livre por mês
	Projecao   []int64 // saldo projetado do mês 0 (hoje) ao mês 6
	Meses      float64 // meses até zerar; 0 se o fluxo não é negativo
}

// CalcRunway calcula o RF04. Opcoes não se aplica (a base é a previsão).
func CalcRunway(conn *sql.DB, _ Opcoes) (Runway, error) {
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return Runway{}, err
	}
	saldo, err := saldoTotal(conn)
	if err != nil {
		return Runway{}, err
	}
	recMedia, despMedia, err := fluxoMensalEsperado(conn, mediaRec, mediaDesp)
	if err != nil {
		return Runway{}, err
	}
	r := Runway{Saldo: saldo, ReceitaMes: recMedia, DespesaMes: despMedia, Liquido: recMedia - despMedia}
	r.Projecao = make([]int64, 7)
	for i := range r.Projecao {
		r.Projecao[i] = saldo + int64(float64(r.Liquido)*float64(i))
	}
	if r.Liquido < 0 && saldo > 0 {
		r.Meses = float64(saldo) / float64(-r.Liquido)
	}
	return r, nil
}

// Imprimir desenha o Runway: médias, projeção 30/90/180 dias e o gráfico.
func (r Runway) Imprimir() {
	fmt.Println("RUNWAY — Projeção de Fluxo de Caixa")
	fmt.Println("(base: recorrências e contas agendadas; média histórica como complemento)")
	fmt.Println()
	fmt.Printf("  Saldo atual:        %s\n", money.Format(r.Saldo))
	fmt.Printf("  Receita média/mês:  %s\n", money.Format(r.ReceitaMes))
	fmt.Printf("  Despesa média/mês:  %s  (burn rate)\n", money.Format(r.DespesaMes))
	fmt.Printf("  Fluxo livre/mês:    %s\n\n", money.Format(r.Liquido))

	fmt.Println("Projeção do saldo:")
	for _, h := range []struct {
		dias  int
		meses float64
	}{{30, 1}, {90, 3}, {180, 6}} {
		proj := r.Saldo + int64(float64(r.Liquido)*h.meses)
		fmt.Printf("  em %3d dias: %s\n", h.dias, money.Format(proj))
	}
	fmt.Println()

	cor := asciigraph.Blue
	if r.Liquido < 0 {
		cor = asciigraph.Red
	}
	fmt.Println(graficoLinha([][]float64{reaisSerie(r.Projecao)}, 8,
		[]asciigraph.AnsiColor{cor}, nil, "saldo projetado — mês 0 (hoje) a mês 6"))
	fmt.Println()

	if r.Liquido >= 0 {
		fmt.Println("Fluxo positivo: o saldo tende a crescer; sem risco de runway.")
		return
	}
	if r.Saldo <= 0 {
		fmt.Printf("⚠ Saldo já zerado/negativo com fluxo negativo (queima de %s/mês).\n", money.Format(-r.Liquido))
		return
	}
	fmt.Printf("⚠ Fluxo negativo. Runway: %.1f meses até o saldo zerar (queima de %s/mês).\n",
		r.Meses, money.Format(-r.Liquido))
}

// AnalyticsAssinaturasOcultas (RF06) varre o histórico atrás de despesas de
// mesmo valor que se repetem em vários meses e ainda não vêm de uma recorrência
// cadastrada — prováveis assinaturas. Exibe o impacto anual de cada uma.
func AnalyticsAssinaturasOcultas(conn *sql.DB) error {
	a, err := CalcAssinaturasOcultas(conn, Opcoes{})
	if err != nil {
		return err
	}
	a.Imprimir()
	return nil
}

// AssinaturaOculta é uma despesa repetida que parece assinatura.
type AssinaturaOculta struct {
	Descricao  string
	Valor      int64
	Meses      int    // meses distintos em que apareceu
	Primeira   string // primeiro mês (AAAA-MM)
	Ultima     string // último mês (AAAA-MM)
	ImpactoAno int64
}

// AssinaturasOcultas é o resultado do RF06, do maior impacto para o menor.
type AssinaturasOcultas struct {
	Itens    []AssinaturaOculta
	TotalAno int64
}

// CalcAssinaturasOcultas calcula o RF06 sobre todo o histórico já vencido.
func CalcAssinaturasOcultas(conn *sql.DB, _ Opcoes) (AssinaturasOcultas, error) {
	// só ocorrências já vencidas: um `--repetir 12` cria meses futuros idênticos
	// de uma vez e seria flagrado no dia do cadastro — o que o usuário acabou de
	// lançar de propósito não é uma assinatura "oculta". O valor usa valEf, como
	// o resto do sistema: numa despesa de grupo, o impacto é o da SUA parte.
	hoje := relogio().Format("2006-01-02")
	rows, err := conn.Query(`
		SELECT descricao, `+valEf("lancamentos")+` AS v, COUNT(DISTINCT substr(vencimento,1,7)) AS meses,
		       MIN(substr(vencimento,1,7)), MAX(substr(vencimento,1,7))
		FROM lancamentos
		WHERE tipo = 'pagar' AND recorrencia_id IS NULL AND vencimento <= ?
		GROUP BY lower(descricao), v
		HAVING meses >= 3
		ORDER BY v * meses DESC`, hoje)
	if err != nil {
		return AssinaturasOcultas{}, err
	}
	defer rows.Close()
	var a AssinaturasOcultas
	for rows.Next() {
		var it AssinaturaOculta
		if err := rows.Scan(&it.Descricao, &it.Valor, &it.Meses, &it.Primeira, &it.Ultima); err != nil {
			return AssinaturasOcultas{}, err
		}
		it.ImpactoAno = it.Valor * 12
		a.TotalAno += it.ImpactoAno
		a.Itens = append(a.Itens, it)
	}
	return a, rows.Err()
}

// Imprimir desenha a tabela de prováveis assinaturas e o impacto anual.
func (a AssinaturasOcultas) Imprimir() {
	fmt.Println("ASSINATURAS OCULTAS — Recorrências prováveis")
	fmt.Println("(despesas de mesmo valor em 3+ meses, fora das recorrências cadastradas)")
	fmt.Println()
	if len(a.Itens) == 0 {
		fmt.Println("Nenhuma assinatura oculta detectada. 👍")
		return
	}
	w := novaTabela()
	fmt.Fprintln(w, "DESCRIÇÃO\tVALOR\tMESES\tIMPACTO/ANO")
	for _, it := range a.Itens {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", it.Descricao, money.Format(it.Valor), it.Meses, money.Format(it.ImpactoAno))
	}
	w.Flush()
	fmt.Printf("\nImpacto anual somado das prováveis assinaturas: %s\n", money.Format(a.TotalAno))
}

// --- helpers de visualização e estatística ---

// primeiroComDado devolve o índice do primeiro mês com algum movimento nas
// séries (receita ou despesa). Meses anteriores são de antes do uso do app e
// não devem entrar nas médias como zero. Sem movimento algum, devolve 0 (a
// janela inteira fica como está).
func primeiroComDado(recs, desps []int64) int {
	for i := range recs {
		if recs[i] != 0 || desps[i] != 0 {
			return i
		}
	}
	return 0
}

// encerraRows fecha o cursor e devolve o erro de iteração pendente, para os
// loops que não usam defer não engolirem um resultado truncado.
func encerraRows(rows *sql.Rows) error {
	err := rows.Err()
	rows.Close()
	return err
}

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

// contemAlguma diz se alguma PALAVRA da categoria é exatamente uma das chaves.
// O casamento é por palavra inteira, não por substring: "gasolina" e "gastos"
// não podem casar a chave "gas" (senão combustível viraria conta da casa).
func contemAlguma(cat string, chaves []string) bool {
	palavras := strings.FieldsFunc(strings.ToLower(cat), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, p := range palavras {
		for _, k := range chaves {
			if p == k {
				return true
			}
		}
	}
	return false
}

// ehNecessidade diz se a categoria é um gasto essencial (vs. estilo de vida).
func ehNecessidade(cat string) bool { return contemAlguma(cat, catsNecessidade) }

// somaPorCategoriaFiltrada soma as despesas quitadas do período cujas categorias
// casam as chaves. O agrupamento vem do SQL, mas o casamento fica no Go
// (contemAlguma), a única fonte da regra de palavra inteira.
func somaPorCategoriaFiltrada(conn *sql.DB, ini, fim string, chaves []string) (int64, error) {
	rows, err := conn.Query(`
		SELECT categoria, SUM(`+valEf("lancamentos")+`) FROM lancamentos
		WHERE tipo='pagar' AND status='quitado'
		  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?
		GROUP BY categoria`, ini, fim)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var cat string
		var v int64
		if err := rows.Scan(&cat, &v); err != nil {
			return 0, err
		}
		if contemAlguma(cat, chaves) {
			total += v
		}
	}
	return total, rows.Err()
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
	r, err := CalcRegra502030(conn, Opcoes{})
	if err != nil {
		return err
	}
	r.Imprimir()
	return nil
}

// Regra502030 é o resultado do RF09: a renda do período dividida em
// necessidades, desejos e o que sobrou (poupança, pode ser negativa).
type Regra502030 struct {
	Meses        int
	Renda        int64
	Necessidades int64
	Desejos      int64
	Poupanca     int64
}

// CalcRegra502030 calcula o RF09 sobre os últimos o.Meses meses (padrão 6).
func CalcRegra502030(conn *sql.DB, o Opcoes) (Regra502030, error) {
	inicio, hoje, refs := janelaMeses(o.meses(analyticsJanela))
	recs, _, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return Regra502030{}, err
	}
	cats, err := GastosPorCategoria(conn, inicio, hoje)
	if err != nil {
		return Regra502030{}, err
	}
	r := Regra502030{Meses: len(refs), Renda: somaInt(recs)}
	var totalDesp int64
	for _, c := range cats {
		totalDesp += c.Valor
		if ehNecessidade(c.Rotulo) {
			r.Necessidades += c.Valor
		} else {
			r.Desejos += c.Valor
		}
	}
	r.Poupanca = r.Renda - totalDesp // o que sobra é o que foi guardado
	return r, nil
}

// Imprimir desenha os três pilares contra o ideal 50/30/20.
func (r Regra502030) Imprimir() {
	fmt.Println("REGRA 50/30/20 — Auditoria do Orçamento")
	fmt.Printf("(janela: últimos %d meses · renda %s)\n\n", r.Meses, money.Format(r.Renda))
	if r.Renda <= 0 {
		fmt.Println("Sem receitas no período para calcular as proporções.")
		return
	}
	linhaPilar("Necessidades", r.Necessidades, r.Renda, 50, cCiano)
	linhaPilar("Desejos", r.Desejos, r.Renda, 30, cMagen)
	linhaPilar("Poupança/Invest.", r.Poupanca, r.Renda, 20, cVerde)
	fmt.Println()

	pN, pP := pct(r.Necessidades, r.Renda), pct(r.Poupanca, r.Renda)
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
	dividas, err := dividasEstimadas(conn)
	if err != nil {
		return err
	}
	liquido := ativos - dividas

	fmt.Println("PATRIMÔNIO LÍQUIDO — Net Worth")
	fmt.Println()
	fmt.Printf("  Ativos (contas + carteiras):  %s\n", money.Format(ativos))
	fmt.Printf("  Dívidas (emergências ativas): %s  (saldo devedor estimado)\n", money.Format(dividas))
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
	fmt.Println("\n(dívida estimada pelo plano de aportes de cada emergência desde o cadastro;")
	fmt.Println(" na evolução, a dívida de hoje é aplicada a todos os meses como referência)")
	return nil
}

// dividasEstimadas soma o saldo devedor atual estimado das emergências ativas:
// projeta o plano de cada uma (simulaPlano) e toma o saldo após os meses
// decorridos desde o cadastro — melhor aproximação que o valor_total original,
// que ignora tudo o que já foi amortizado.
func dividasEstimadas(conn *sql.DB) (int64, error) {
	rows, err := conn.Query(`
		SELECT valor_total, juros_mes, aporte_mensal, criada_em
		FROM emergencias WHERE status='ativa'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	agora := relogio()
	var total int64
	for rows.Next() {
		var valor, aporte int64
		var juros float64
		var criada string
		if err := rows.Scan(&valor, &juros, &aporte, &criada); err != nil {
			return 0, err
		}
		total += saldoDevedorEstimado(valor, juros, aporte, criada, agora)
	}
	return total, rows.Err()
}

// saldoDevedorEstimado devolve o saldo da dívida após os meses-calendário
// decorridos entre o cadastro e hoje, seguindo o plano de aportes. Cadastro no
// mês corrente (ou data inválida) devolve o valor cheio; dívida já percorrida
// além do plano devolve o saldo final (0 se quitável).
func saldoDevedorEstimado(valor int64, jurosPct float64, aporte int64, criadaEm string, hoje time.Time) int64 {
	de, err := parseDataT(criadaEm)
	if err != nil {
		return valor
	}
	meses := (hoje.Year()-de.Year())*12 + int(hoje.Month()) - int(de.Month())
	if meses <= 0 {
		return valor
	}
	plano := simulaPlano(valor, jurosPct, aporte)
	if len(plano) == 0 {
		return valor
	}
	if meses > len(plano) {
		meses = len(plano)
	}
	return plano[meses-1].saldoFinal
}

// AnalyticsInflacao (RF08) calcula a inflação pessoal: compara o gasto com contas
// básicas no trimestre atual com o mesmo trimestre do ano anterior.
func AnalyticsInflacao(conn *sql.DB) error {
	i, err := CalcInflacao(conn, Opcoes{})
	if err != nil {
		return err
	}
	i.Imprimir()
	return nil
}

// Inflacao é o resultado do RF08: gasto com contas básicas no trimestre atual
// e no mesmo trimestre do ano anterior.
type Inflacao struct {
	Atual    int64
	Anterior int64
	Variacao float64 // % de um ano para o outro; só vale se Anterior > 0
}

// CalcInflacao calcula o RF08. Opcoes não se aplica (a janela é o trimestre).
func CalcInflacao(conn *sql.DB, _ Opcoes) (Inflacao, error) {
	soma := func(ini, fim string) (int64, error) {
		return somaPorCategoriaFiltrada(conn, ini, fim, catsBasica)
	}
	agora := relogio()
	aIni, aFim := agora.AddDate(0, -3, 0).Format("2006-01-02"), agora.Format("2006-01-02")
	bIni, bFim := agora.AddDate(-1, -3, 0).Format("2006-01-02"), agora.AddDate(-1, 0, 0).Format("2006-01-02")
	var i Inflacao
	var err error
	if i.Atual, err = soma(aIni, aFim); err != nil {
		return Inflacao{}, err
	}
	if i.Anterior, err = soma(bIni, bFim); err != nil {
		return Inflacao{}, err
	}
	if i.Anterior > 0 {
		i.Variacao = pct(i.Atual-i.Anterior, i.Anterior)
	}
	return i, nil
}

// Imprimir desenha a comparação ano contra ano.
func (i Inflacao) Imprimir() {
	fmt.Println("INFLAÇÃO PESSOAL — Custo de Vida Doméstico")
	fmt.Println("(contas básicas: mercado, luz, água, condomínio — trimestre atual x ano anterior)")
	fmt.Println()
	fmt.Printf("  Trimestre atual:        %s\n", money.Format(i.Atual))
	fmt.Printf("  Mesmo trimestre (-1 ano): %s\n\n", money.Format(i.Anterior))
	if i.Anterior <= 0 {
		fmt.Println("Sem dados do ano anterior para comparar (precisa de ~15 meses de histórico).")
		return
	}
	switch {
	case i.Variacao > 0:
		fmt.Printf("Seu custo de vida básico subiu %.1f%% em um ano. 📈\n", i.Variacao)
	case i.Variacao < 0:
		fmt.Printf("Seu custo de vida básico caiu %.1f%% em um ano. 📉\n", -i.Variacao)
	default:
		fmt.Println("Seu custo de vida básico ficou estável em um ano.")
	}
}

// AnalyticsUtilidades (RF11) acompanha o consumo das contas de utilidade (luz,
// água, gás, internet): gasto mensal, proporção da renda e detecção de picos.
func AnalyticsUtilidades(conn *sql.DB) error {
	u, err := CalcUtilidades(conn, Opcoes{})
	if err != nil {
		return err
	}
	u.Imprimir()
	return nil
}

// Utilidades é o resultado do RF11, mês a mês.
type Utilidades struct {
	Refs    []string // meses (AAAA-MM)
	Gasto   []int64  // gasto com utilidades em cada mês
	Renda   []int64  // receita de cada mês
	TemBase bool     // há meses anteriores com movimento para comparar o atual
	Media   int64    // média dos meses anteriores (base do pico)
	Pico    float64  // % do mês corrente acima da média (≤ 0: sem pico)
}

// CalcUtilidades calcula o RF11 sobre os últimos o.Meses meses (padrão 6).
func CalcUtilidades(conn *sql.DB, o Opcoes) (Utilidades, error) {
	inicio, hoje, refs := janelaMeses(o.meses(analyticsJanela))
	porCat, err := gastoPorCategoriaMes(conn, refs[0], refs[len(refs)-1])
	if err != nil {
		return Utilidades{}, err
	}
	recs, desps, err := estatMensal(conn, inicio, hoje, refs)
	if err != nil {
		return Utilidades{}, err
	}
	u := Utilidades{Refs: refs, Renda: recs, Gasto: make([]int64, len(refs))}
	for cat, serie := range porCat {
		if !contemAlguma(cat, catsUtilidade) {
			continue
		}
		for i, ref := range refs {
			u.Gasto[i] += serie[ref]
		}
	}
	// detecção de pico: mês corrente x média dos anteriores. Meses antes do
	// primeiro movimento registrado ficam de fora — não são "gasto zero", são
	// meses em que o app ainda não era usado, e contá-los derrubaria a média.
	ini := primeiroComDado(recs, desps)
	if n := len(u.Gasto); n >= 2 && ini < n-1 {
		u.TemBase = true
		u.Media = somaInt(u.Gasto[ini:n-1]) / int64(n-1-ini)
		if atual := u.Gasto[n-1]; u.Media > 0 && atual > u.Media*125/100 {
			u.Pico = pct(atual-u.Media, u.Media)
		}
	}
	return u, nil
}

// Imprimir desenha a tabela mensal, a tendência e o aviso de pico.
func (u Utilidades) Imprimir() {
	fmt.Println("EFICIÊNCIA DE UTILIDADES — Contas da Casa")
	fmt.Println("(luz, água, gás, internet)")
	fmt.Println()
	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tUTILIDADES\t% DA RENDA")
	for i, ref := range u.Refs {
		prop := "-"
		if u.Renda[i] > 0 {
			prop = fmt.Sprintf("%.1f%%", pct(u.Gasto[i], u.Renda[i]))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", mesBR(ref), money.Format(u.Gasto[i]), prop)
	}
	w.Flush()
	if len(u.Gasto) >= 2 {
		fmt.Printf("\nTendência (%s a %s): %s\n", mesBR(u.Refs[0]), mesBR(u.Refs[len(u.Refs)-1]), pintar(cCiano, sparkline(u.Gasto)))
	}
	if !u.TemBase {
		return
	}
	fmt.Println()
	if u.Pico > 0 {
		fmt.Printf("⚠ Pico: as utilidades deste mês estão %.0f%% acima da média dos meses anteriores.\n", u.Pico)
	} else if u.Media > 0 {
		fmt.Println("👍 Consumo de utilidades dentro do padrão dos meses anteriores.")
	}
}

// AnalyticsAnomalias (RF02) compara os gastos do mês corrente, por categoria, com
// o histórico (média e desvio padrão dos meses anteriores) e alerta as categorias
// com comportamento atípico (acima do padrão).
func AnalyticsAnomalias(conn *sql.DB) error {
	a, err := CalcAnomalias(conn, Opcoes{})
	if err != nil {
		return err
	}
	a.Imprimir()
	return nil
}

// Anomalia é uma categoria com gasto atípico no mês corrente.
type Anomalia struct {
	Categoria string
	Atual     int64   // gasto no mês corrente
	Media     int64   // média dos meses da base
	Acima     float64 // % acima da média
}

// Anomalias é o resultado do RF02, da maior para a menor.
type Anomalias struct {
	Ref       string // mês corrente (AAAA-MM)
	BaseMeses int    // meses anteriores usados como base
	Itens     []Anomalia
}

// CalcAnomalias calcula o RF02: o mês corrente contra os o.Meses meses
// anteriores (padrão 6).
func CalcAnomalias(conn *sql.DB, o Opcoes) (Anomalias, error) {
	_, _, refs := janelaMeses(o.meses(analyticsJanela) + 1) // anteriores + o corrente
	atual := refs[len(refs)-1]
	anteriores := refs[:len(refs)-1]

	porCat, err := gastoPorCategoriaMes(conn, refs[0], "9999-12")
	if err != nil {
		return Anomalias{}, err
	}

	// a base de comparação começa no primeiro mês com qualquer gasto: os meses
	// de antes do uso do app não são "gasto zero" e derrubariam a média,
	// gerando falsos positivos para quem tem pouco histórico
	primeiro := atual
	for _, serie := range porCat {
		for mes := range serie {
			if mes < primeiro {
				primeiro = mes
			}
		}
	}
	base := anteriores[:0:0]
	for _, m := range anteriores {
		if m >= primeiro {
			base = append(base, m)
		}
	}
	a := Anomalias{Ref: atual, BaseMeses: len(base)}
	if len(base) == 0 {
		return a, nil
	}

	for cat, serie := range porCat {
		vAtual := serie[atual]
		if vAtual == 0 {
			continue
		}
		// média e desvio dos meses da base (faltantes contam como 0)
		var soma float64
		for _, m := range base {
			soma += float64(serie[m])
		}
		media := soma / float64(len(base))
		if media <= 0 {
			continue // categoria nova: sem base de comparação
		}
		var varSum float64
		for _, m := range base {
			d := float64(serie[m]) - media
			varSum += d * d
		}
		desvio := math.Sqrt(varSum / float64(len(base)))
		// atípico: acima de 30% da média E acima de média + 1 desvio
		if float64(vAtual) > media*1.3 && float64(vAtual) > media+desvio {
			a.Itens = append(a.Itens, Anomalia{cat, vAtual, int64(media), pct(vAtual-int64(media), int64(media))})
		}
	}
	// do maior para o menor desvio (empate: ordem alfabética, para ser estável)
	sort.Slice(a.Itens, func(i, j int) bool {
		if a.Itens[i].Acima != a.Itens[j].Acima {
			return a.Itens[i].Acima > a.Itens[j].Acima
		}
		return a.Itens[i].Categoria < a.Itens[j].Categoria
	})
	return a, nil
}

// Imprimir desenha os alertas de anomalia.
func (a Anomalias) Imprimir() {
	fmt.Println("MODO ECONOMIA — Detecção de Anomalias")
	fmt.Printf("(mês corrente %s x média dos %d meses anteriores)\n\n", mesBR(a.Ref), a.BaseMeses)
	if a.BaseMeses == 0 {
		fmt.Println("Sem meses anteriores com gastos para servir de base de comparação.")
		return
	}
	if len(a.Itens) == 0 {
		fmt.Println("Nenhuma anomalia: os gastos do mês seguem o padrão histórico. 👍")
		return
	}
	for _, it := range a.Itens {
		fmt.Printf("⚠ %s: %s neste mês — %.0f%% acima do padrão (média %s).\n",
			it.Categoria, money.Format(it.Atual), it.Acima, money.Format(it.Media))
	}
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
	if err := encerraRows(rows); err != nil {
		return err
	}

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

	// alerta dos próximos 3 meses se forem picos sazonais. O AddDate parte do
	// dia 1º: somar meses a um dia 29–31 normaliza para o mês seguinte (31/jan
	// + 1 mês = 2/mar) e pularia/duplicaria meses no aviso.
	agora := relogio()
	base1 := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC)
	var avisos []string
	for i := 1; i <= 3; i++ {
		prox := int(base1.AddDate(0, i, 0).Month())
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
	// teto da divisão: guardar "pelo menos" a parcela; truncar deixaria a soma
	// das parcelas abaixo da meta
	parcela := (valor + int64(prazo) - 1) / int64(prazo)
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
		fmt.Printf("\nNo superávit atual, a meta levaria ~%d meses.\n", (valor+superavit-1)/superavit)
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

// AnalyticsComprometimento (RF12) mostra, para cada um dos próximos meses,
// quanto da renda esperada já está tomado por obrigações conhecidas — parcelas,
// contas agendadas e recorrências — antes de o mês começar. É a régua para
// decidir se cabe parcelar algo novo: o Runway diz se o fluxo médio fecha; aqui
// aparece QUANDO vem o mês apertado.
func AnalyticsComprometimento(conn *sql.DB) error {
	c, err := CalcComprometimento(conn, Opcoes{})
	if err != nil {
		return err
	}
	c.Imprimir()
	return nil
}

// MesComprometido é um mês futuro com a renda esperada e o que já tem dono.
type MesComprometido struct {
	Ref          string // AAAA-MM
	Renda        int64
	Comprometido int64
	Pct          float64 // % da renda tomado; só vale se Renda > 0
}

// Comprometimento é o resultado do RF12.
type Comprometimento struct {
	Meses   []MesComprometido
	Pior    string // mês mais comprometido ("" se nenhum tem renda)
	PiorPct float64
}

// CalcComprometimento calcula o RF12 para os próximos o.Meses meses (padrão 6).
func CalcComprometimento(conn *sql.DB, o Opcoes) (Comprometimento, error) {
	mediaRec, _, err := mediasHistoricas(conn)
	if err != nil {
		return Comprometimento{}, err
	}
	agora := relogio()
	base := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC)
	var c Comprometimento
	for i := 1; i <= o.meses(6); i++ {
		ref := base.AddDate(0, i, 0).Format("2006-01")
		p, err := periodoMes(ref)
		if err != nil {
			return Comprometimento{}, err
		}
		m := MesComprometido{Ref: ref}
		if m.Renda, _, err = previstoMes(conn, "receber", p, mediaRec); err != nil {
			return Comprometimento{}, err
		}
		// média 0 de fallback: comprometido é só o que JÁ está agendado —
		// projetar despesa média aqui diria "100% comprometido" para todo mundo
		if m.Comprometido, _, err = previstoMes(conn, "pagar", p, 0); err != nil {
			return Comprometimento{}, err
		}
		if m.Renda > 0 {
			m.Pct = pct(m.Comprometido, m.Renda)
			if m.Pct > c.PiorPct {
				c.Pior, c.PiorPct = ref, m.Pct
			}
		}
		c.Meses = append(c.Meses, m)
	}
	return c, nil
}

// Imprimir desenha a tabela mês a mês e o veredito do mês mais apertado.
func (c Comprometimento) Imprimir() {
	fmt.Println("RENDA COMPROMETIDA — Parcelas e Contas Já Agendadas")
	fmt.Println("(quanto da renda esperada de cada mês já está tomado antes de ele começar)")
	fmt.Println()
	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tRENDA ESPERADA\tCOMPROMETIDO\t% TOMADO\t")
	for _, m := range c.Meses {
		prop, barra := "-", ""
		if m.Renda > 0 {
			prop = fmt.Sprintf("%.0f%%", m.Pct)
			barra = barraFinaCor(m.Comprometido, m.Renda, 20, corFaixaUso(m.Pct))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", mesBR(m.Ref), money.Format(m.Renda), money.Format(m.Comprometido), prop, barra)
	}
	w.Flush()
	fmt.Println()
	switch {
	case c.Pior == "":
		fmt.Println("Sem renda prevista nos próximos meses para medir o comprometimento.")
	case c.PiorPct >= 80:
		fmt.Printf("⚠ %s é o mês mais apertado: %.0f%% da renda já tem dono. Evite parcelar algo novo até lá.\n",
			mesBR(c.Pior), c.PiorPct)
	case c.PiorPct >= 50:
		fmt.Printf("O mês mais comprometido é %s (%.0f%% da renda). Há margem, mas parcele com critério.\n",
			mesBR(c.Pior), c.PiorPct)
	default:
		fmt.Printf("👍 Folga boa: nem o mês mais comprometido (%s, %.0f%%) passa da metade da renda.\n",
			mesBR(c.Pior), c.PiorPct)
	}
	fmt.Println("\n(comprometido = parcelas, contas agendadas e recorrências; renda pela previsão)")
}

// corFaixaUso colore um percentual de comprometimento: verde < 50, amarelo < 80,
// vermelho daí em diante — as mesmas faixas de usoLimite.
func corFaixaUso(pc float64) string {
	switch {
	case pc >= 80:
		return cVermel
	case pc >= 50:
		return cAmar
	default:
		return cVerde
	}
}

// AnalyticsAderenciaPlanos (RF13) fecha o ciclo do planejamento: dos planos
// mensais (`prisma plano add`) de meses já encerrados, quantos foram
// respeitados, categoria a categoria — e quem mais estoura. O gasto de cada mês
// vem da MESMA régua do `plano status` (gastoDoPlano).
func AnalyticsAderenciaPlanos(conn *sql.DB) error {
	_, _, refs := janelaMeses(analyticsJanela + 1) // +1: o corrente sai da conta
	atual := refs[len(refs)-1]

	rows, err := conn.Query(`
		SELECT categoria, limite, ref FROM planejamentos
		WHERE periodo = 'mes' AND ref >= ? AND ref < ?
		ORDER BY categoria, ref`, refs[0], atual)
	if err != nil {
		return err
	}
	type plano struct {
		cat, ref string
		limite   int64
	}
	var planos []plano
	for rows.Next() {
		var p plano
		if err := rows.Scan(&p.cat, &p.limite, &p.ref); err != nil {
			rows.Close()
			return err
		}
		planos = append(planos, p)
	}
	if err := encerraRows(rows); err != nil {
		return err
	}

	fmt.Println("ADERÊNCIA A PLANOS — Planejado x Realizado")
	fmt.Printf("(planos mensais de %s a %s — o mês corrente, ainda aberto, fica de fora)\n\n",
		mesBR(refs[0]), mesBR(refs[len(refs)-2]))
	if len(planos) == 0 {
		fmt.Println("Nenhum plano mensal de mês já encerrado na janela.")
		fmt.Println("Crie com: prisma plano add --cat mercado --valor 800 --periodo mes")
		return nil
	}

	// consolida por categoria: meses cumpridos e o tamanho médio do estouro
	type placar struct {
		cat                   string
		cumpridos, total      int
		estouroSoma, estouroN int64
	}
	porCat := map[string]*placar{}
	var ordem []string
	totCumpridos, totPlanos := 0, 0
	for _, pl := range planos {
		p, err := periodoMes(pl.ref)
		if err != nil {
			return err
		}
		gasto, err := gastoDoPlano(conn, pl.cat, p)
		if err != nil {
			return err
		}
		s := porCat[pl.cat]
		if s == nil {
			s = &placar{cat: pl.cat}
			porCat[pl.cat] = s
			ordem = append(ordem, pl.cat)
		}
		s.total++
		totPlanos++
		if gasto <= pl.limite {
			s.cumpridos++
			totCumpridos++
		} else if pl.limite > 0 {
			s.estouroSoma += (gasto - pl.limite) * 100 / pl.limite // % inteiro
			s.estouroN++
		}
	}

	w := novaTabela()
	fmt.Fprintln(w, "CATEGORIA\tCUMPRIDOS\tESTOURO MÉDIO\t")
	for _, cat := range ordem {
		s := porCat[cat]
		estouro := "-"
		if s.estouroN > 0 {
			estouro = fmt.Sprintf("+%d%%", s.estouroSoma/s.estouroN)
		}
		frac := pct(int64(s.cumpridos), int64(s.total))
		fmt.Fprintf(w, "%s\t%d de %d\t%s\t%s\n", cat, s.cumpridos, s.total, estouro,
			barraFinaCor(int64(s.cumpridos), int64(s.total), 16, corFaixaCumprimento(frac)))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println()
	geral := pct(int64(totCumpridos), int64(totPlanos))
	switch {
	case geral >= 80:
		fmt.Printf("👍 Você respeitou %d de %d planos (%.0f%%): o planejamento está funcionando.\n",
			totCumpridos, totPlanos, geral)
	case geral >= 50:
		fmt.Printf("Você respeitou %d de %d planos (%.0f%%). Vale revisar os limites que mais estouram.\n",
			totCumpridos, totPlanos, geral)
	default:
		fmt.Printf("⚠ Só %d de %d planos respeitados (%.0f%%): ou os limites estão irreais, ou o gasto fugiu.\n",
			totCumpridos, totPlanos, geral)
	}
	return nil
}

// corFaixaCumprimento colore a taxa de planos cumpridos: verde ≥ 80%, amarelo
// ≥ 50%, vermelho abaixo (o inverso de corFaixaUso: aqui mais é melhor).
func corFaixaCumprimento(pc float64) string {
	switch {
	case pc >= 80:
		return cVerde
	case pc >= 50:
		return cAmar
	default:
		return cVermel
	}
}

// AnalyticsTendencias (RF14) pega o vazamento lento que a detecção de anomalia
// pontual não pega: ajusta uma reta ao gasto mensal de cada categoria (meses já
// FECHADOS — o corrente, parcial, enviesaria a inclinação para baixo) e mostra
// as que sobem ou caem de forma consistente.
func AnalyticsTendencias(conn *sql.DB) error {
	t, err := CalcTendencias(conn, Opcoes{})
	if err != nil {
		return err
	}
	t.Imprimir()
	return nil
}

// Tendencia é uma categoria cujo gasto sobe ou cai de forma consistente.
type Tendencia struct {
	Categoria string
	Serie     []int64 // gasto mensal desde o primeiro mês da categoria na janela
	Slope     float64 // centavos/mês (positivo = subindo)
	RelMes    float64 // inclinação em % da média por mês
}

// Tendencias é o resultado do RF14, da inclinação mais forte para a mais fraca
// (no máximo 8).
type Tendencias struct {
	De, Ate string // meses fechados analisados (AAAA-MM)
	Itens   []Tendencia
}

// CalcTendencias calcula o RF14 sobre os últimos o.Meses meses FECHADOS
// (padrão 6) — o corrente, parcial, enviesaria a inclinação para baixo.
func CalcTendencias(conn *sql.DB, o Opcoes) (Tendencias, error) {
	_, _, refs := janelaMeses(o.meses(analyticsJanela) + 1)
	fechados := refs[:len(refs)-1]
	t := Tendencias{De: fechados[0], Ate: fechados[len(fechados)-1]}

	porCat, err := gastoPorCategoriaMes(conn, t.De, t.Ate)
	if err != nil {
		return Tendencias{}, err
	}
	for cat, porMes := range porCat {
		serie := make([]int64, len(fechados))
		for i, r := range fechados {
			serie[i] = porMes[r]
		}
		// a série da categoria começa no primeiro mês em que ela existiu
		ini := 0
		for ini < len(serie) && serie[ini] == 0 {
			ini++
		}
		s := serie[ini:]
		if len(s) < 3 {
			continue // menos de 3 meses: não há tendência para ajustar
		}
		slope, media := retaSlope(s)
		if media <= 0 {
			continue
		}
		rel := slope / media * 100
		// relevante: pelo menos ±8% da média ao mês E ±R$ 20/mês de inclinação
		if math.Abs(rel) < 8 || math.Abs(slope) < 2000 {
			continue
		}
		t.Itens = append(t.Itens, Tendencia{cat, s, slope, rel})
	}
	sort.Slice(t.Itens, func(i, j int) bool {
		a, b := math.Abs(t.Itens[i].Slope), math.Abs(t.Itens[j].Slope)
		if a != b {
			return a > b
		}
		return t.Itens[i].Categoria < t.Itens[j].Categoria
	})
	if len(t.Itens) > 8 {
		t.Itens = t.Itens[:8]
	}
	return t, nil
}

// Imprimir desenha a tabela de tendências e o maior vazamento.
func (t Tendencias) Imprimir() {
	fmt.Println("TENDÊNCIAS — Para Onde o Gasto Está Indo")
	fmt.Printf("(reta ajustada ao gasto mensal por categoria, %s a %s)\n\n", mesBR(t.De), mesBR(t.Ate))
	if len(t.Itens) == 0 {
		fmt.Println("Nenhuma tendência relevante: os gastos por categoria estão estáveis. 👍")
		return
	}
	w := novaTabela()
	fmt.Fprintln(w, "CATEGORIA\tMÊS A MÊS\tTENDÊNCIA")
	for _, td := range t.Itens {
		seta, cor := "▲", cVermel // gasto subindo = ruim
		if td.Slope < 0 {
			seta, cor = "▼", cVerde
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", td.Categoria, sparkline(td.Serie),
			pintar(cor, fmt.Sprintf("%s %s/mês (%+.0f%%/mês)", seta, money.Format(int64(math.Abs(td.Slope))), td.RelMes)))
	}
	w.Flush()
	if pior := t.Itens[0]; pior.Slope > 0 {
		fmt.Printf("\n⚠ Maior vazamento: %s sobe ~%s por mês. Nesse ritmo, são +%s em um ano.\n",
			pior.Categoria, money.Format(int64(pior.Slope)), money.Format(int64(pior.Slope*12)))
	}
}

// retaSlope ajusta uma reta y = a + b·x à série (mínimos quadrados) e devolve a
// inclinação b (centavos por mês) e a média da série.
func retaSlope(vals []int64) (slope, media float64) {
	n := float64(len(vals))
	var somaX, somaY, somaXY, somaXX float64
	for i, v := range vals {
		x, y := float64(i), float64(v)
		somaX += x
		somaY += y
		somaXY += x * y
		somaXX += x * x
	}
	den := n*somaXX - somaX*somaX
	if den == 0 {
		return 0, somaY / n
	}
	return (n*somaXY - somaX*somaY) / den, somaY / n
}

// AnalyticsGrupos (RF15) responde se dividir contas está funcionando: por
// grupo, a sua parte das despesas, os reembolsos gerados (despesas em que você
// adiantou o total e recebe a parte dos outros de volta), quanto já voltou e
// quanto ainda devem.
func AnalyticsGrupos(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT g.id, g.nome, COALESCE(SUM(` + valEf("l") + `), 0)
		FROM grupos g LEFT JOIN lancamentos l ON l.grupo_id = g.id AND l.tipo = 'pagar'
		GROUP BY g.id, g.nome ORDER BY g.id`)
	if err != nil {
		return err
	}
	type balanco struct {
		nome                      string
		minha, recebido, aReceber int64
	}
	porGrupo := map[int64]*balanco{}
	var ordem []int64
	for rows.Next() {
		var id int64
		b := &balanco{}
		if err := rows.Scan(&id, &b.nome, &b.minha); err != nil {
			rows.Close()
			return err
		}
		porGrupo[id] = b
		ordem = append(ordem, id)
	}
	if err := encerraRows(rows); err != nil {
		return err
	}

	fmt.Println("BALANÇO DOS GRUPOS — Divisão de Contas")
	fmt.Println("(sua parte, o que você adiantou pelos outros e o que já voltou)")
	fmt.Println()
	if len(ordem) == 0 {
		fmt.Println("Nenhum grupo cadastrado. Crie com: prisma grupo add --nome \"Casa\" --pessoas \"Eu, Maria\"")
		return nil
	}

	// reembolsos: receitas ligadas (reembolso_de) a despesas de cada grupo
	rows, err = conn.Query(`
		SELECT d.grupo_id,
		       COALESCE(SUM(CASE WHEN r.status = 'quitado'  THEN r.valor ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN r.status = 'pendente' THEN r.valor ELSE 0 END), 0)
		FROM lancamentos r JOIN lancamentos d ON r.reembolso_de = d.id
		WHERE r.tipo = 'receber' AND d.grupo_id IS NOT NULL
		GROUP BY d.grupo_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, recebido, pendente int64
		if err := rows.Scan(&id, &recebido, &pendente); err != nil {
			rows.Close()
			return err
		}
		if b := porGrupo[id]; b != nil {
			b.recebido, b.aReceber = recebido, pendente
		}
	}
	if err := encerraRows(rows); err != nil {
		return err
	}

	w := novaTabela()
	fmt.Fprintln(w, "GRUPO\tSUA PARTE\tJÁ VOLTOU\tAINDA DEVEM\tRETORNO\t")
	var totDevem int64
	for _, id := range ordem {
		b := porGrupo[id]
		totDevem += b.aReceber
		adiantado := b.recebido + b.aReceber
		retorno, barra := "-", ""
		if adiantado > 0 {
			rp := pct(b.recebido, adiantado)
			retorno = fmt.Sprintf("%.0f%%", rp)
			barra = barraFinaCor(b.recebido, adiantado, 14, corFaixaCumprimento(rp))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", b.nome, money.Format(b.minha),
			money.Format(b.recebido), money.Format(b.aReceber), retorno, barra)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println()
	if totDevem > 0 {
		fmt.Printf("No total, ainda têm %s para voltar dos reembolsos em aberto.\n", money.Format(totDevem))
		fmt.Println("(receba com: prisma quitar <id> — os reembolsos são lançamentos \"receber\")")
	} else {
		fmt.Println("👍 Nenhum reembolso em aberto: a divisão de contas está em dia.")
	}
	fmt.Println("\n(despesas divididas sem \"recebe pagamento\" não geram reembolso: cada um paga")
	fmt.Println(" a sua parte direto, e aqui conta só a sua)")
	return nil
}

// AnalyticsMelhorDiaCartao (RF16) mostra, por cartão, quantos dias de prazo uma
// compra HOJE ganha até o vencimento da fatura e qual o melhor dia do ciclo
// (logo após o fechamento) com o prazo máximo. Puramente determinístico: só usa
// o ciclo de cada cartão (faturaDe), sem histórico.
func AnalyticsMelhorDiaCartao(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT nome, dia_fechamento, dia_vencimento FROM cartoes ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("MELHOR DIA DE COMPRA — Prazo do Cartão")
	fmt.Println("(comprando logo após o fechamento, a compra só vence na fatura seguinte)")
	fmt.Println()
	agora := relogio()
	hoje := time.Date(agora.Year(), agora.Month(), agora.Day(), 0, 0, 0, 0, time.UTC)

	w := novaTabela()
	fmt.Fprintln(w, "CARTÃO\tCOMPRANDO HOJE\tMELHOR JANELA\t")
	achou := false
	var melhorNome string
	var melhorHoje int
	for rows.Next() {
		achou = true
		var nome string
		var fech, venc int
		if err := rows.Scan(&nome, &fech, &venc); err != nil {
			return err
		}
		prazoHoje := prazoCompra(fech, venc, hoje)
		// varre o ciclo à frente atrás do dia com o prazo máximo
		melhorDia, melhorPrazo := hoje, prazoHoje
		for d := 1; d <= 31; d++ {
			dia := hoje.AddDate(0, 0, d)
			if p := prazoCompra(fech, venc, dia); p > melhorPrazo {
				melhorDia, melhorPrazo = dia, p
			}
		}
		janela := "hoje é o melhor dia ✓"
		if !melhorDia.Equal(hoje) {
			janela = fmt.Sprintf("a partir de %s (%d dias)", melhorDia.Format("02/01"), melhorPrazo)
		}
		fmt.Fprintf(w, "%s\t%d dias (vence %s)\t%s\t%s\n", nome, prazoHoje,
			dataBR(vencimentoCompra(fech, venc, hoje)), janela,
			barraFinaCor(int64(prazoHoje), int64(melhorPrazo), 14, cCiano))
		if prazoHoje > melhorHoje {
			melhorNome, melhorHoje = nome, prazoHoje
		}
	}
	if !achou {
		fmt.Println("Nenhum cartão cadastrado.")
		return nil
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if melhorNome != "" {
		fmt.Printf("\nPara comprar hoje, o maior fôlego é no %s: %d dias até pagar.\n", melhorNome, melhorHoje)
	}
	return rows.Err()
}

// prazoCompra devolve os dias entre uma compra e o vencimento da fatura em que
// ela cai, pelo ciclo do cartão.
func prazoCompra(fech, venc int, compra time.Time) int {
	_, vencFat := faturaDe(fech, venc, compra)
	v, err := parseDataT(vencFat)
	if err != nil {
		return 0
	}
	return int(v.Sub(compra).Hours() / 24)
}

// vencimentoCompra é o vencimento (AAAA-MM-DD) da fatura de uma compra na data dada.
func vencimentoCompra(fech, venc int, compra time.Time) string {
	_, vencFat := faturaDe(fech, venc, compra)
	return vencFat
}
