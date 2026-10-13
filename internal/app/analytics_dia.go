package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"prisma/internal/money"
)

// Análises do dia a dia do Prisma Analytics: Ritmo do Mês, Dia Mais Apertado,
// Gastos Formiga e o painel Hoje, que junta os alertas de todas. Seguem o
// padrão CalcX + Imprimir descrito em analytics.go.

// competencia é a data em que uma despesa pesa no mês: a da compra no cartão,
// senão a da quitação.
const competencia = "COALESCE(data_compra, quitado_em)"

// --- Ritmo do Mês ---

// RitmoCategoria é uma categoria que gastou bem mais que o normal até hoje.
type RitmoCategoria struct {
	Categoria   string
	AteHoje     int64
	MediaAteDia int64
	Maiores     []LancamentoResumo
}

// Ritmo compara o mês corrente, até hoje, com o mesmo trecho dos anteriores.
type Ritmo struct {
	Ref              string // mês corrente (AAAA-MM)
	Dia, DiasNoMes   int
	MesesBase        int   // meses anteriores usados na média (0 = sem base)
	AteHoje          int64 // gasto do mês até hoje
	MediaAteDia      int64 // média dos meses da base até o mesmo dia
	MesPassadoAteDia int64
	MediaMes         int64 // média dos meses da base completos
	Agendado         int64 // despesas pendentes que vencem até o fim do mês
	Projecao         int64 // fechamento estimado do mês
	Renda            int64 // receitas do mês: recebidas + a receber
	Indice           float64
	Aceleraram       []RitmoCategoria
}

// CalcRitmo calcula o Ritmo do Mês contra os o.Meses meses anteriores (padrão 6).
//
// A projeção soma ao gasto até hoje o que ainda deve vir: o maior entre o
// restante médio dos meses anteriores (do dia seguinte ao fim) e as despesas já
// agendadas para o resto do mês — o agendado é um piso conhecido, e o restante
// médio cobre o que costuma aparecer sem estar lançado.
func CalcRitmo(conn *sql.DB, o Opcoes) (Ritmo, error) {
	agora := relogio()
	_, _, refs := janelaMeses(o.meses(analyticsJanela) + 1)
	atual := refs[len(refs)-1]
	anteriores := refs[:len(refs)-1]
	r := Ritmo{
		Ref:       atual,
		Dia:       agora.Day(),
		DiasNoMes: time.Date(agora.Year(), agora.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day(),
	}

	// gasto até o dia D de cada mês (meses mais curtos que D entram inteiros)
	rows, err := conn.Query(`
		SELECT categoria, substr(`+competencia+`,1,7) AS mes, SUM(`+valEf("lancamentos")+`)
		FROM lancamentos WHERE tipo='pagar' AND status='quitado'
		  AND substr(`+competencia+`,1,7) BETWEEN ? AND ?
		  AND CAST(substr(`+competencia+`,9,2) AS INTEGER) <= ?
		GROUP BY categoria, mes`, refs[0], atual, r.Dia)
	if err != nil {
		return Ritmo{}, err
	}
	ateDia := map[string]map[string]int64{} // categoria -> mês -> gasto até o dia D
	for rows.Next() {
		var cat, mes string
		var v int64
		if err := rows.Scan(&cat, &mes, &v); err != nil {
			rows.Close()
			return Ritmo{}, err
		}
		if ateDia[cat] == nil {
			ateDia[cat] = map[string]int64{}
		}
		ateDia[cat][mes] = v
	}
	if err := encerraRows(rows); err != nil {
		return Ritmo{}, err
	}
	completo, err := gastoPorCategoriaMes(conn, refs[0], anteriores[len(anteriores)-1])
	if err != nil {
		return Ritmo{}, err
	}

	// a base começa no primeiro mês com gasto (meses antes do uso do app não
	// são "gasto zero")
	primeiro := atual
	for _, serie := range completo {
		for mes := range serie {
			if mes < primeiro {
				primeiro = mes
			}
		}
	}
	var base []string
	for _, m := range anteriores {
		if m >= primeiro {
			base = append(base, m)
		}
	}
	r.MesesBase = len(base)
	mesPassado := anteriores[len(anteriores)-1]

	type somaCat struct{ atual, baseAteDia int64 }
	porCat := map[string]*somaCat{}
	for cat, serie := range ateDia {
		sc := &somaCat{atual: serie[atual]}
		for _, m := range base {
			sc.baseAteDia += serie[m]
		}
		porCat[cat] = sc
		r.AteHoje += serie[atual]
		r.MesPassadoAteDia += serie[mesPassado]
	}
	var somaCompleto int64
	for _, serie := range completo {
		for _, m := range base {
			somaCompleto += serie[m]
		}
	}
	if len(base) > 0 {
		var somaAteDia int64
		for _, sc := range porCat {
			somaAteDia += sc.baseAteDia
		}
		r.MediaAteDia = somaAteDia / int64(len(base))
		r.MediaMes = somaCompleto / int64(len(base))
		if r.MediaAteDia > 0 {
			r.Indice = float64(r.AteHoje) / float64(r.MediaAteDia) * 100
		}
	}

	hoje := agora.Format("2006-01-02")
	p, err := periodoMes(atual)
	if err != nil {
		return Ritmo{}, err
	}
	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE tipo='pagar' AND status='pendente' AND vencimento > ? AND vencimento < ?`,
		hoje, p.Fim).Scan(&r.Agendado); err != nil {
		return Ritmo{}, err
	}
	restante := r.MediaMes - r.MediaAteDia
	if r.Agendado > restante {
		restante = r.Agendado
	}
	if restante < 0 {
		restante = 0
	}
	r.Projecao = r.AteHoje + restante

	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE tipo='receber' AND (
		      (status='quitado'  AND quitado_em >= ? AND quitado_em < ?)
		   OR (status='pendente' AND vencimento >= ? AND vencimento < ?))`,
		p.Inicio, p.Fim, p.Inicio, p.Fim).Scan(&r.Renda); err != nil {
		return Ritmo{}, err
	}

	// categorias que aceleraram: 30%+ acima da média até o dia e ao menos
	// R$ 50 de diferença (abaixo disso é ruído)
	if len(base) > 0 {
		for cat, sc := range porCat {
			media := sc.baseAteDia / int64(len(base))
			if sc.atual > media*13/10 && sc.atual-media >= 5000 {
				r.Aceleraram = append(r.Aceleraram, RitmoCategoria{Categoria: cat, AteHoje: sc.atual, MediaAteDia: media})
			}
		}
		sort.Slice(r.Aceleraram, func(i, j int) bool {
			di := r.Aceleraram[i].AteHoje - r.Aceleraram[i].MediaAteDia
			dj := r.Aceleraram[j].AteHoje - r.Aceleraram[j].MediaAteDia
			if di != dj {
				return di > dj
			}
			return r.Aceleraram[i].Categoria < r.Aceleraram[j].Categoria
		})
		if len(r.Aceleraram) > 3 {
			r.Aceleraram = r.Aceleraram[:3]
		}
		for i := range r.Aceleraram {
			if r.Aceleraram[i].Maiores, err = lancamentosDoGasto(conn, r.Aceleraram[i].Categoria, atual, atual, 3); err != nil {
				return Ritmo{}, err
			}
		}
	}
	return r, nil
}

// Imprimir desenha o ritmo: barras até hoje, veredito, projeção e categorias.
func (r Ritmo) Imprimir() {
	nome := mesPorExtenso(r.Ref + "-01")
	fmt.Printf("RITMO DO MÊS — Como %s Está Indo\n", strings.ToUpper(nome[:1])+nome[1:])
	fmt.Printf("(dia %d de %d · comparado ao mesmo trecho dos %d meses anteriores)\n\n", r.Dia, r.DiasNoMes, r.MesesBase)
	if r.MesesBase == 0 {
		fmt.Printf("  Gasto até hoje: %s\n\n", money.Format(r.AteHoje))
		fmt.Println("Sem meses anteriores com gastos para comparar o ritmo.")
		return
	}
	maior := r.AteHoje
	for _, v := range []int64{r.MediaAteDia, r.MesPassadoAteDia} {
		if v > maior {
			maior = v
		}
	}
	corAtual := cCiano
	switch {
	case r.Indice >= 115:
		corAtual = cVermel
	case r.Indice > 0 && r.Indice <= 85:
		corAtual = cVerde
	}
	fmt.Printf("  %-22s %s %s\n", "Até hoje", barraFinaCor(r.AteHoje, maior, 24, corAtual), money.Format(r.AteHoje))
	fmt.Printf("  %-22s %s %s\n", fmt.Sprintf("Média até o dia %d", r.Dia), barraFinaCor(r.MediaAteDia, maior, 24, cCinza), money.Format(r.MediaAteDia))
	fmt.Printf("  %-22s %s %s\n\n", fmt.Sprintf("Mês passado até o %d", r.Dia), barraFinaCor(r.MesPassadoAteDia, maior, 24, cCinza), money.Format(r.MesPassadoAteDia))

	dif := r.AteHoje - r.MediaAteDia
	switch {
	case r.MediaAteDia == 0:
		fmt.Println("Sem gasto nos meses anteriores até este dia para comparar.")
	case r.Indice >= 115:
		fmt.Println(pintar(cVermel, fmt.Sprintf("⚠ Ritmo acima do normal: %.0f%% da média até aqui (+%s).", r.Indice, money.Format(dif))))
	case r.Indice <= 85:
		fmt.Println(pintar(cVerde, fmt.Sprintf("👍 Ritmo abaixo do normal: %.0f%% da média até aqui (%s).", r.Indice, money.Format(dif))))
	default:
		fmt.Printf("Ritmo dentro do normal: %.0f%% da média até aqui.\n", r.Indice)
	}

	fmt.Printf("\n  Projeção de fechamento: %s  (média de um mês inteiro: %s)\n", money.Format(r.Projecao), money.Format(r.MediaMes))
	if r.Agendado > 0 {
		fmt.Printf("  Já agendado até o fim do mês: %s\n", money.Format(r.Agendado))
	}
	if r.Renda > 0 {
		sobra := r.Renda - r.Projecao
		cor := cVerde
		if sobra < 0 {
			cor = cVermel
		}
		fmt.Printf("  Renda do mês: %s → sobra projetada %s\n", money.Format(r.Renda), pintar(cor, money.Format(sobra)))
	}

	if len(r.Aceleraram) > 0 {
		fmt.Println("\nCategorias que aceleraram:")
		for _, c := range r.Aceleraram {
			fmt.Printf("  ⚠ %s: %s até hoje vs %s em média (+%s)\n", c.Categoria,
				money.Format(c.AteHoje), money.Format(c.MediaAteDia), money.Format(c.AteHoje-c.MediaAteDia))
			imprimeEvidencias(c.Maiores)
		}
	}
	fmt.Println("\n(projeção = gasto até hoje + o maior entre o restante médio dos meses anteriores e o já agendado)")
}

// AnalyticsRitmo exibe o Ritmo do Mês. args[0], opcional: meses de base.
func AnalyticsRitmo(conn *sql.DB, args ...string) error {
	o, err := opcoesDeArgs(args)
	if err != nil {
		return err
	}
	r, err := CalcRitmo(conn, o)
	if err != nil {
		return err
	}
	r.Imprimir()
	return nil
}

// --- Dia Mais Apertado ---

// horizonteApertado é quantos dias à frente o Dia Mais Apertado projeta.
const horizonteApertado = 45

// Movimento é uma conta a pagar (valor negativo) ou a receber (positivo).
type Movimento struct {
	Data      string // vencimento, AAAA-MM-DD
	Descricao string
	Valor     int64
}

// DiaApertado projeta o saldo dia a dia pelas pendências já lançadas.
type DiaApertado struct {
	Hoje             string
	Saldo            int64   // saldo de hoje, antes das pendências
	Serie            []int64 // saldo ao fim de cada dia; [0] = hoje (inclui vencidos)
	Menor            int64
	MenorData        string
	PrimeiroNegativo string      // primeiro dia com saldo < 0 ("" se nunca)
	Saidas           []Movimento // maiores contas a pagar até o dia mais apertado
}

// CalcDiaApertado projeta os próximos 45 dias. Pendências vencidas entram
// hoje: é quando, na prática, ainda terão de ser pagas ou recebidas.
func CalcDiaApertado(conn *sql.DB, _ Opcoes) (DiaApertado, error) {
	agora := relogio()
	dia0 := time.Date(agora.Year(), agora.Month(), agora.Day(), 0, 0, 0, 0, time.UTC)
	d := DiaApertado{Hoje: dia0.Format("2006-01-02")}
	fim := dia0.AddDate(0, 0, horizonteApertado).Format("2006-01-02")
	var err error
	if d.Saldo, err = saldoTotal(conn); err != nil {
		return DiaApertado{}, err
	}

	rows, err := conn.Query(`
		SELECT vencimento, descricao, tipo, `+valEf("lancamentos")+`
		FROM lancamentos WHERE status='pendente' AND vencimento <= ?
		ORDER BY vencimento`, fim)
	if err != nil {
		return DiaApertado{}, err
	}
	delta := make([]int64, horizonteApertado+1)
	var movs []Movimento
	for rows.Next() {
		var m Movimento
		var tipo string
		if err := rows.Scan(&m.Data, &m.Descricao, &tipo, &m.Valor); err != nil {
			rows.Close()
			return DiaApertado{}, err
		}
		if tipo == "pagar" {
			m.Valor = -m.Valor
		}
		i := 0
		if t, err := time.Parse("2006-01-02", m.Data); err == nil && t.After(dia0) {
			i = int(t.Sub(dia0).Hours() / 24)
		}
		if m.Data < d.Hoje {
			m.Data = d.Hoje // vencido: conta como hoje
		}
		delta[i] += m.Valor
		movs = append(movs, m)
	}
	if err := encerraRows(rows); err != nil {
		return DiaApertado{}, err
	}

	d.Serie = make([]int64, len(delta))
	saldo := d.Saldo
	for i, v := range delta {
		saldo += v
		d.Serie[i] = saldo
		data := dia0.AddDate(0, 0, i).Format("2006-01-02")
		if i == 0 || saldo < d.Menor {
			d.Menor, d.MenorData = saldo, data
		}
		if saldo < 0 && d.PrimeiroNegativo == "" {
			d.PrimeiroNegativo = data
		}
	}
	for _, m := range movs {
		if m.Valor < 0 && m.Data <= d.MenorData {
			d.Saidas = append(d.Saidas, m)
		}
	}
	sort.SliceStable(d.Saidas, func(i, j int) bool { return d.Saidas[i].Valor < d.Saidas[j].Valor })
	if len(d.Saidas) > 5 {
		d.Saidas = d.Saidas[:5]
	}
	return d, nil
}

var diasSemana = []string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

// diaCurto formata AAAA-MM-DD como "25/10 (sáb)".
func diaCurto(data string) string {
	t, err := time.Parse("2006-01-02", data)
	if err != nil {
		return data
	}
	return fmt.Sprintf("%s (%s)", t.Format("02/01"), diasSemana[t.Weekday()])
}

// Imprimir desenha a curva do saldo e o dia mais apertado.
func (d DiaApertado) Imprimir() {
	fmt.Printf("DIA MAIS APERTADO — Saldo Dia a Dia (próximos %d dias)\n", horizonteApertado)
	fmt.Println("(saldo de hoje + contas a pagar e a receber já lançadas, pelo vencimento)")
	fmt.Println()
	n := len(d.Serie)
	fmt.Printf("  Saldo hoje:          %s\n", money.Format(d.Saldo))
	cor := cVerde
	if d.Menor < 0 {
		cor = cVermel
	} else if d.Menor < d.Saldo/5 {
		cor = cAmar
	}
	fmt.Printf("  Menor saldo:         %s em %s\n", pintar(cor, money.Format(d.Menor)), diaCurto(d.MenorData))
	if n > 0 {
		fimData, _ := time.Parse("2006-01-02", d.Hoje)
		fmt.Printf("  Fim da janela:       %s em %s\n\n", money.Format(d.Serie[n-1]), diaCurto(fimData.AddDate(0, 0, n-1).Format("2006-01-02")))
		fmt.Println("  " + sparklineSinal(d.Serie))
		fmt.Printf("  %-*s%s\n\n", n-4, "hoje", fmt.Sprintf("+%dd", n-1))
	}
	switch {
	case d.PrimeiroNegativo != "":
		fmt.Println(pintar(cVermel, fmt.Sprintf("⚠ O saldo fica negativo a partir de %s e chega a %s em %s.",
			diaCurto(d.PrimeiroNegativo), money.Format(d.Menor), diaCurto(d.MenorData))))
		fmt.Println("  Antecipe uma receita, adie uma conta ou transfira de outra reserva antes disso.")
	case d.MenorData == d.Hoje:
		fmt.Println("👍 Hoje é o ponto mais baixo da janela: daqui em diante o saldo só melhora.")
	default:
		fmt.Printf("👍 O saldo não fica negativo; o momento mais justo é %s.\n", diaCurto(d.MenorData))
	}
	if len(d.Saidas) > 0 && d.MenorData != d.Hoje {
		fmt.Println("\nMaiores saídas até o dia mais apertado:")
		w := novaTabela()
		for _, m := range d.Saidas {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", diaCurto(m.Data), truncar(m.Descricao, 28), money.Format(-m.Valor))
		}
		w.Flush()
	}
	fmt.Println("\n(só o que está lançado: recorrências já materializadas e contas agendadas)")
}

// AnalyticsDiaApertado exibe o Dia Mais Apertado.
func AnalyticsDiaApertado(conn *sql.DB) error {
	d, err := CalcDiaApertado(conn, Opcoes{})
	if err != nil {
		return err
	}
	d.Imprimir()
	return nil
}

// --- Gastos Formiga ---

// limiteFormiga é o valor máximo padrão de um gasto "formiga" (R$ 50).
const limiteFormiga = 5000

// Formiga é um pequeno gasto que se repete.
type Formiga struct {
	Descricao  string // como aparece mais vezes
	Categoria  string
	Vezes      int
	Total      int64
	VezesMes   float64
	MediaMes   int64
	ImpactoAno int64
}

// GastosFormiga é o resultado da análise de pequenos gastos repetidos.
type GastosFormiga struct {
	Limite        int64
	Meses         int // meses efetivos da janela
	Itens         []Formiga
	TotalMes      int64 // média mensal somada dos itens
	TotalDespesas int64 // despesas do período, para a proporção
	Total         int64 // gasto somado dos itens no período
}

var (
	reParcela  = regexp.MustCompile(`\(?\b\d{1,2}\s*/\s*\d{1,2}\)?`)
	reNumeros  = regexp.MustCompile(`\b\d{3,}\b`)
	reSimbolos = regexp.MustCompile(`[*#_\-]+`)
)

// normalizaDescricao agrupa variações da mesma despesa: "IFOOD *Rest 1234",
// "iFood " e "ifood (2/3)" viram "ifood rest" / "ifood".
func normalizaDescricao(s string) string {
	s = strings.ToLower(s)
	s = reParcela.ReplaceAllString(s, " ")
	s = reNumeros.ReplaceAllString(s, " ")
	s = reSimbolos.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// CalcFormiga acha despesas de até o limite (padrão R$ 50) que se repetem 4+
// vezes nos últimos o.Meses meses (padrão 3). Recorrências cadastradas ficam
// de fora: são assinaturas conhecidas, não hábito.
func CalcFormiga(conn *sql.DB, o Opcoes, limite int64) (GastosFormiga, error) {
	if limite <= 0 {
		limite = limiteFormiga
	}
	inicio, hoje, refs := janelaMeses(o.meses(3))
	g := GastosFormiga{Limite: limite, Meses: len(refs)}

	// meses antes do primeiro gasto registrado não contam na média mensal
	var primeiro sql.NullString
	if err := conn.QueryRow(`SELECT MIN(substr(` + competencia + `,1,7)) FROM lancamentos
		WHERE tipo='pagar' AND status='quitado'`).Scan(&primeiro); err != nil {
		return GastosFormiga{}, err
	}
	if primeiro.Valid {
		for i, r := range refs {
			if r >= primeiro.String {
				g.Meses = len(refs) - i
				break
			}
		}
	}
	if g.Meses < 1 {
		g.Meses = 1
	}

	if err := conn.QueryRow(`SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE tipo='pagar' AND status='quitado' AND `+competencia+` BETWEEN ? AND ?`,
		inicio, hoje).Scan(&g.TotalDespesas); err != nil {
		return GastosFormiga{}, err
	}

	rows, err := conn.Query(`
		SELECT descricao, categoria, `+valEf("lancamentos")+` AS v FROM lancamentos
		WHERE tipo='pagar' AND status='quitado' AND recorrencia_id IS NULL
		  AND `+competencia+` BETWEEN ? AND ?`, inicio, hoje)
	if err != nil {
		return GastosFormiga{}, err
	}
	type acum struct {
		f     Formiga
		nomes map[string]int
		cats  map[string]int
	}
	grupos := map[string]*acum{}
	for rows.Next() {
		var desc, cat string
		var v int64
		if err := rows.Scan(&desc, &cat, &v); err != nil {
			rows.Close()
			return GastosFormiga{}, err
		}
		if v <= 0 || v > limite {
			continue
		}
		chave := normalizaDescricao(desc)
		if chave == "" {
			continue
		}
		a := grupos[chave]
		if a == nil {
			a = &acum{nomes: map[string]int{}, cats: map[string]int{}}
			grupos[chave] = a
		}
		a.f.Vezes++
		a.f.Total += v
		a.nomes[strings.TrimSpace(desc)]++
		a.cats[cat]++
	}
	if err := encerraRows(rows); err != nil {
		return GastosFormiga{}, err
	}

	maisComum := func(m map[string]int) string {
		melhor, n := "", -1
		for k, c := range m {
			if c > n || (c == n && k < melhor) {
				melhor, n = k, c
			}
		}
		return melhor
	}
	for _, a := range grupos {
		if a.f.Vezes < 4 {
			continue
		}
		f := a.f
		f.Descricao = maisComum(a.nomes)
		f.Categoria = maisComum(a.cats)
		f.VezesMes = float64(f.Vezes) / float64(g.Meses)
		f.MediaMes = f.Total / int64(g.Meses)
		f.ImpactoAno = f.MediaMes * 12
		g.Itens = append(g.Itens, f)
		g.Total += f.Total
		g.TotalMes += f.MediaMes
	}
	sort.Slice(g.Itens, func(i, j int) bool {
		if g.Itens[i].Total != g.Itens[j].Total {
			return g.Itens[i].Total > g.Itens[j].Total
		}
		return g.Itens[i].Descricao < g.Itens[j].Descricao
	})
	if len(g.Itens) > 12 {
		g.Itens = g.Itens[:12]
	}
	return g, nil
}

// Imprimir desenha a tabela dos gastos formiga e o impacto somado.
func (g GastosFormiga) Imprimir() {
	fmt.Println("GASTOS FORMIGA — Pequenos Gastos Repetidos")
	fmt.Printf("(despesas de até %s que se repetem 4+ vezes nos últimos %d meses)\n\n", money.Format(g.Limite), g.Meses)
	if len(g.Itens) == 0 {
		fmt.Println("Nenhum gasto pequeno se repete com frequência. 👍")
		return
	}
	w := novaTabela()
	fmt.Fprintln(w, "DESCRIÇÃO\tCATEGORIA\tVEZES\tPOR MÊS\tMÉDIA/MÊS\tIMPACTO/ANO")
	for _, f := range g.Itens {
		fmt.Fprintf(w, "%s\t%s\t%d\t%.1f\t%s\t%s\n", truncar(f.Descricao, 24), f.Categoria, f.Vezes, f.VezesMes,
			money.Format(f.MediaMes), money.Format(f.ImpactoAno))
	}
	w.Flush()
	fmt.Printf("\nSomados: %s por mês", money.Format(g.TotalMes))
	if g.TotalDespesas > 0 {
		fmt.Printf(" (%.0f%% das despesas do período)", pct(g.Total, g.TotalDespesas))
	}
	fmt.Printf(" → %s por ano.\n", money.Format(g.TotalMes*12))
	if top := g.Itens[0]; top.VezesMes >= 4 {
		fmt.Printf("Cortar pela metade só \"%s\" devolve ~%s por ano.\n", top.Descricao, money.Format(top.ImpactoAno/2))
	}
}

// AnalyticsFormiga exibe os Gastos Formiga. args (opcionais): [meses, limite].
func AnalyticsFormiga(conn *sql.DB, args ...string) error {
	o, err := opcoesDeArgs(args)
	if err != nil {
		return err
	}
	var limite int64
	if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
		if limite, err = money.Parse(args[1]); err != nil {
			return err
		}
		if limite <= 0 {
			return fmt.Errorf("limite deve ser maior que zero")
		}
	}
	g, err := CalcFormiga(conn, o, limite)
	if err != nil {
		return err
	}
	g.Imprimir()
	return nil
}

// --- Hoje (painel) ---

// Alerta é uma linha do painel Hoje.
type Alerta struct {
	Grave bool // vermelho (true) ou amarelo
	Texto string
	Tela  string // onde ver o detalhe
}

// Painel reúne o que merece atenção hoje, já em ordem de prioridade.
type Painel struct {
	Hoje        string
	Alertas     []Alerta // no máximo 5
	Score       int
	RitmoIndice float64 // 0 = sem base para o ritmo
}

// CalcPainel roda as análises e escolhe os alertas, por prioridade: saldo
// negativo previsto, ritmo acima do normal, mês comprometido, anomalia,
// assinatura nova e tendência de alta.
func CalcPainel(conn *sql.DB, _ Opcoes) (Painel, error) {
	agora := relogio()
	p := Painel{Hoje: agora.Format("2006-01-02")}

	d, err := CalcDiaApertado(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	if d.PrimeiroNegativo != "" {
		p.Alertas = append(p.Alertas, Alerta{true,
			fmt.Sprintf("Saldo negativo previsto a partir de %s (chega a %s).", diaCurto(d.PrimeiroNegativo), money.Format(d.Menor)),
			"Dia Mais Apertado"})
	}

	r, err := CalcRitmo(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	p.RitmoIndice = r.Indice
	if r.MesesBase > 0 && r.Indice >= 115 {
		txt := fmt.Sprintf("Gasto do mês em %.0f%% do normal para o dia %d (+%s).", r.Indice, r.Dia, money.Format(r.AteHoje-r.MediaAteDia))
		if len(r.Aceleraram) > 0 {
			txt += " Puxado por " + r.Aceleraram[0].Categoria + "."
		}
		p.Alertas = append(p.Alertas, Alerta{r.Indice >= 130, txt, "Ritmo do Mês"})
	}

	c, err := CalcComprometimento(conn, Opcoes{Meses: 3})
	if err != nil {
		return Painel{}, err
	}
	if c.PiorPct >= 80 {
		p.Alertas = append(p.Alertas, Alerta{c.PiorPct >= 100,
			fmt.Sprintf("%s já tem %.0f%% da renda comprometida.", mesBR(c.Pior), c.PiorPct), "Renda Comprometida"})
	}

	a, err := CalcAnomalias(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	if len(a.Itens) > 0 {
		it := a.Itens[0]
		txt := fmt.Sprintf("%s está %.0f%% acima do padrão neste mês (%s).", it.Categoria, it.Acima, money.Format(it.Atual))
		if len(a.Itens) > 1 {
			txt += fmt.Sprintf(" +%d outra(s).", len(a.Itens)-1)
		}
		p.Alertas = append(p.Alertas, Alerta{false, txt, "Modo Economia"})
	}

	ao, err := CalcAssinaturasOcultas(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	recente := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -2, 0).Format("2006-01")
	for _, it := range ao.Itens {
		if it.Primeira >= recente {
			p.Alertas = append(p.Alertas, Alerta{false,
				fmt.Sprintf("Possível assinatura nova: %s, %s/mês desde %s.", it.Descricao, money.Format(it.Valor), mesBR(it.Primeira)),
				"Assinaturas Ocultas"})
			break
		}
	}

	t, err := CalcTendencias(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	if len(t.Itens) > 0 && t.Itens[0].Slope > 0 {
		td := t.Itens[0]
		p.Alertas = append(p.Alertas, Alerta{false,
			fmt.Sprintf("%s sobe ~%s por mês (+%s em um ano nesse ritmo).", td.Categoria,
				money.Format(int64(td.Slope)), money.Format(int64(td.Slope*12))), "Tendências"})
	}
	if len(p.Alertas) > 5 {
		p.Alertas = p.Alertas[:5]
	}

	h, err := CalcHealthScore(conn, Opcoes{})
	if err != nil {
		return Painel{}, err
	}
	p.Score = h.Score
	return p, nil
}

// Imprimir desenha o painel Hoje.
func (p Painel) Imprimir() {
	fmt.Println("HOJE — O Que Merece Atenção")
	fmt.Printf("(%s · resumo das análises; o detalhe está na tela indicada)\n\n", dataBR(p.Hoje))
	if len(p.Alertas) == 0 {
		fmt.Println(pintar(cVerde, "👍 Tudo dentro do padrão: nenhum alerta nas análises."))
	}
	for _, a := range p.Alertas {
		cor := cAmar
		if a.Grave {
			cor = cVermel
		}
		fmt.Printf("  %s %s\n", pintar(cor, "●"), a.Texto)
		fmt.Println(pintar(cCinza, "    → "+a.Tela))
	}
	fmt.Printf("\nSaúde financeira: %d/100 (%s)", p.Score, rotuloSaude(p.Score))
	if p.RitmoIndice > 0 {
		fmt.Printf(" · ritmo do mês: %.0f%% da média", p.RitmoIndice)
	}
	fmt.Println()
}

// AnalyticsHoje exibe o painel Hoje.
func AnalyticsHoje(conn *sql.DB) error {
	p, err := CalcPainel(conn, Opcoes{})
	if err != nil {
		return err
	}
	p.Imprimir()
	return nil
}
