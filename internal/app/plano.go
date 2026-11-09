package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	"prisma/internal/money"
)

// Plano trata `prisma plano add|listar|status|remover` — o planejamento de
// gastos por categoria, por semana ou por mês.
func Plano(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "add", "adicionar":
		return planoAdd(conn, args[1:])
	case "listar", "ls":
		return planoListar(conn)
	case "status":
		return planoStatus(conn, args[1:])
	case "editar":
		return planoEditar(conn, args[1:])
	case "remover", "rm":
		return planoRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, status, editar, remover)", args[0])
	}
}

// planoEditar altera o limite e/ou a categoria de um plano:
// `prisma plano editar <id> [--valor] [--cat]`.
func planoEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma plano editar <id> [--valor] [--cat] [--acumula|--acumula=false]")
	}
	id := args[0]
	fs := flag.NewFlagSet("plano editar", flag.ContinueOnError)
	valor := fs.String("valor", "", "novo limite")
	cat := fs.String("cat", "", "nova categoria")
	acumula := fs.Bool("acumula", false, "a sobra passa para o período seguinte (--acumula=false desliga)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })

	var sets []string
	var params []any
	if informado["valor"] {
		limite, err := money.Parse(*valor)
		if err != nil {
			return err
		}
		if limite <= 0 {
			return fmt.Errorf("o limite deve ser positivo")
		}
		sets, params = append(sets, "limite = ?"), append(params, limite)
	}
	if informado["cat"] {
		if *cat == "" {
			return fmt.Errorf("a categoria não pode ficar vazia")
		}
		sets, params = append(sets, "categoria = ?"), append(params, strings.ToLower(*cat))
	}
	if informado["acumula"] {
		sets, params = append(sets, "acumula = ?"), append(params, *acumula)
	}
	if len(sets) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}
	params = append(params, id)
	res, err := conn.Exec(`UPDATE planejamentos SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("já existe um plano dessa categoria nesse período")
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("plano #%s não encontrado", id)
	}
	fmt.Printf("Plano #%s atualizado.\n", id)
	return nil
}

func planoAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("plano add", flag.ContinueOnError)
	cat := fs.String("cat", "", "categoria planejada (obrigatório)")
	valor := fs.String("valor", "", "limite de gasto no período (obrigatório)")
	per := fs.String("periodo", "mes", "mes ou semana")
	ref := fs.String("ref", "", "referência: AAAA-MM para mês, AAAA-Wnn para semana (padrão: atual)")
	repetir := fs.Int("repetir", 1, "repete o plano pelos próximos N períodos")
	acumula := fs.Bool("acumula", false, "a sobra de um período passa para o seguinte")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cat == "" || *valor == "" {
		return fmt.Errorf("--cat e --valor são obrigatórios")
	}
	if *per != "mes" && *per != "semana" {
		return fmt.Errorf("--periodo deve ser mes ou semana")
	}
	if *repetir < 1 || *repetir > 60 {
		return fmt.Errorf("--repetir deve estar entre 1 e 60")
	}
	limite, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if limite <= 0 {
		return fmt.Errorf("o limite deve ser positivo")
	}
	if *ref == "" {
		*ref = refAtual(*per)
	}

	refs, err := proximasRefs(*per, *ref, *repetir)
	if err != nil {
		return err
	}
	for _, r := range refs {
		_, err := conn.Exec(`
			INSERT INTO planejamentos (categoria, limite, periodo, ref, acumula) VALUES (?,?,?,?,?)
			ON CONFLICT (categoria, periodo, ref) DO UPDATE SET limite = excluded.limite, acumula = excluded.acumula`,
			strings.ToLower(*cat), limite, *per, r, *acumula,
		)
		if err != nil {
			return err
		}
		extra := ""
		if *acumula {
			extra = " (a sobra passa para o período seguinte)"
		}
		fmt.Printf("Plano: até %s em %q no período %s%s.\n", money.Format(limite), strings.ToLower(*cat), r, extra)
	}
	return nil
}

// proximasRefs gera n referências consecutivas a partir de ref ("2026-06" ou "2026-W24").
func proximasRefs(per, ref string, n int) ([]string, error) {
	refs := make([]string, 0, n)
	atual := ref
	for i := 0; i < n; i++ {
		p, err := resolvePeriodo(per, atual)
		if err != nil {
			return nil, err
		}
		refs = append(refs, atual)
		// a referência seguinte é o período que contém a data de fim (exclusiva)
		prox, err := refDaData(per, p.Fim)
		if err != nil {
			return nil, err
		}
		atual = prox
	}
	return refs, nil
}

// refDaData retorna a referência (AAAA-MM ou AAAA-Wnn) que contém a data AAAA-MM-DD.
func refDaData(per, data string) (string, error) {
	t, err := parseDataT(data)
	if err != nil {
		return "", err
	}
	if per == "semana" {
		ano, sem := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", ano, sem), nil
	}
	return t.Format("2006-01"), nil
}

func planoListar(conn *sql.DB) error {
	rows, err := conn.Query(
		`SELECT id, categoria, limite, periodo, ref, acumula FROM planejamentos ORDER BY ref, categoria`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tPERÍODO\tREF\tCATEGORIA\tLIMITE\tSOBRA")
	achou := false
	for rows.Next() {
		achou = true
		var id, limite int64
		var cat, per, ref string
		var acumula bool
		if err := rows.Scan(&id, &cat, &limite, &per, &ref, &acumula); err != nil {
			return err
		}
		sobra := "-"
		if acumula {
			sobra = "acumula"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", id, per, ref, cat, money.Format(limite), sobra)
	}
	if !achou {
		fmt.Println("Nenhum plano cadastrado. Use: prisma plano add --cat mercado --valor 800")
		return nil
	}
	return w.Flush()
}

// planoStatus compara cada plano do período com os gastos lançados
// (a pagar quitados + pendentes com vencimento dentro do período).
// gastoDoPlano soma o que conta contra o limite de uma categoria no período:
// compras de cartão pela data da compra e, fora do cartão, quitados pela
// quitação e pendentes pelo vencimento. Compartilhado pelo status do plano e
// pela Aderência a Planos do Analytics — os dois têm que bater.
func gastoDoPlano(conn *sql.DB, cat string, p periodo) (int64, error) {
	var gasto int64
	err := conn.QueryRow(`
		SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE tipo = 'pagar' AND categoria = ?
		  AND ((cartao_id IS NOT NULL AND data_compra >= ? AND data_compra < ?)
		    OR (cartao_id IS NULL AND status = 'quitado' AND quitado_em >= ? AND quitado_em < ?)
		    OR (cartao_id IS NULL AND status = 'pendente' AND vencimento >= ? AND vencimento < ?))`,
		cat, p.Inicio, p.Fim, p.Inicio, p.Fim, p.Inicio, p.Fim,
	).Scan(&gasto)
	return gasto, err
}

func planoStatus(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("plano status", flag.ContinueOnError)
	per := fs.String("periodo", "mes", "mes ou semana")
	ref := fs.String("ref", "", "referência (padrão: período atual)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := resolvePeriodo(*per, *ref)
	if err != nil {
		return err
	}

	rows, err := conn.Query(
		`SELECT categoria, limite, acumula FROM planejamentos WHERE periodo = ? AND ref = ? ORDER BY categoria`,
		*per, p.Rotulo,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type linha struct {
		cat                  string
		limite, gasto, sobra int64
		acumula              bool
	}
	var linhas []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.cat, &l.limite, &l.acumula); err != nil {
			return err
		}
		linhas = append(linhas, l)
	}
	rows.Close()
	if len(linhas) == 0 {
		fmt.Printf("Nenhum plano para o período %s. Use: prisma plano add --cat mercado --valor 800 --periodo %s --ref %s\n",
			p.Rotulo, *per, p.Rotulo)
		return nil
	}

	fmt.Printf("PLANEJAMENTO %s (%s a %s)\n\n", p.Rotulo, dataBR(p.Inicio), dataBR(somaDias(p.Fim, -1)))
	w := novaTabela()
	fmt.Fprintln(w, "CATEGORIA\tLIMITE\tSOBRA ANTERIOR\tGASTO\tRESTANTE\tUSO")
	var estourou []string
	for i := range linhas {
		var err error
		if linhas[i].gasto, err = gastoDoPlano(conn, linhas[i].cat, p); err != nil {
			return err
		}
		if linhas[i].acumula {
			if linhas[i].sobra, err = sobraAcumulada(conn, linhas[i].cat, *per, p.Rotulo); err != nil {
				return err
			}
		}
		l := linhas[i]
		efetivo := l.limite + l.sobra
		restante := efetivo - l.gasto
		pct := float64(0)
		if efetivo > 0 {
			pct = float64(l.gasto) / float64(efetivo) * 100
		}
		sobra := "-"
		if l.acumula {
			sobra = "+" + money.Format(l.sobra)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s %.0f%%\n",
			l.cat, money.Format(l.limite), sobra, money.Format(l.gasto), money.Format(restante),
			barra(l.gasto, efetivo), pct)
		if restante < 0 {
			estourou = append(estourou, l.cat)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(estourou) > 0 {
		fmt.Printf("\n⚠ Limite estourado em: %s\n", strings.Join(estourou, ", "))
	}
	return nil
}

// PlanoUso é o consumo de um plano de gastos em um período.
type PlanoUso struct {
	Periodo, Ref  string
	Limite, Gasto int64 // Limite já inclui a sobra acumulada, quando há
	Categoria     string
}

// Pct é o uso do limite, em %.
func (u PlanoUso) Pct() int64 {
	if u.Limite <= 0 {
		return 0
	}
	return u.Gasto * 100 / u.Limite
}

// refAnterior é o período imediatamente antes de ref.
func refAnterior(per, ref string) (string, error) {
	p, err := resolvePeriodo(per, ref)
	if err != nil {
		return "", err
	}
	return refDaData(per, somaDias(p.Inicio, -1))
}

// sobraAcumulada é quanto sobrou dos períodos anteriores para o plano de ref
// (que acumula): o limite efetivo do anterior menos o gasto dele, nunca
// negativo — o estouro não passa adiante. Volta no máximo 24 períodos e só
// segue para trás enquanto o anterior também acumula.
func sobraAcumulada(conn *sql.DB, cat, per, ref string) (int64, error) {
	type elo struct {
		ref     string
		limite  int64
		acumula bool
	}
	var cadeia []elo
	atual := ref
	for i := 0; i < 24; i++ {
		ant, err := refAnterior(per, atual)
		if err != nil {
			return 0, err
		}
		var e elo
		err = conn.QueryRow(`SELECT limite, acumula FROM planejamentos WHERE categoria = ? AND periodo = ? AND ref = ?`,
			cat, per, ant).Scan(&e.limite, &e.acumula)
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return 0, err
		}
		e.ref = ant
		cadeia = append(cadeia, e)
		if !e.acumula {
			break
		}
		atual = ant
	}
	var sobra int64
	for i := len(cadeia) - 1; i >= 0; i-- { // do mais antigo para o mais novo
		e := cadeia[i]
		p, err := resolvePeriodo(per, e.ref)
		if err != nil {
			return 0, err
		}
		gasto, err := gastoDoPlano(conn, cat, p)
		if err != nil {
			return 0, err
		}
		efetivo := e.limite
		if e.acumula {
			efetivo += sobra
		}
		sobra = max(0, efetivo-gasto)
	}
	return sobra, nil
}

// AvisoPlano é a linha de alerta depois de um gasto novo: quando o plano da
// categoria passa de 80% ou estoura ("" quando está tudo bem).
func AvisoPlano(conn *sql.DB, cat, data string) string {
	usos, err := PlanosDaCategoria(conn, cat, data)
	if err != nil {
		return ""
	}
	rot := map[string]string{"mes": "do mês", "semana": "da semana"}
	var b strings.Builder
	for _, u := range usos {
		switch {
		case u.Limite <= 0:
		case u.Gasto > u.Limite:
			fmt.Fprintf(&b, "⚠ Plano de %s %s estourado: %s de %s.\n", u.Categoria, rot[u.Periodo], money.Format(u.Gasto), money.Format(u.Limite))
		case u.Pct() >= 80:
			fmt.Fprintf(&b, "⚠ Plano de %s %s em %d%%: %s de %s.\n", u.Categoria, rot[u.Periodo], u.Pct(), money.Format(u.Gasto), money.Format(u.Limite))
		}
	}
	return b.String()
}

// AlertasOrcamento lista os planos do período atual (mês e semana) que já
// passaram de 80% do limite efetivo.
func AlertasOrcamento(conn *sql.DB, hoje time.Time) ([]PlanoUso, error) {
	var out []PlanoUso
	for _, per := range []string{"mes", "semana"} {
		ref, err := refDaData(per, hoje.Format("2006-01-02"))
		if err != nil {
			return nil, err
		}
		rows, err := conn.Query(`SELECT categoria FROM planejamentos WHERE periodo = ? AND ref = ? ORDER BY categoria`, per, ref)
		if err != nil {
			return nil, err
		}
		var cats []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return nil, err
			}
			cats = append(cats, c)
		}
		rows.Close()
		for _, c := range cats {
			usos, err := PlanosDaCategoria(conn, c, hoje.Format("2006-01-02"))
			if err != nil {
				return nil, err
			}
			for _, u := range usos {
				if u.Periodo == per && u.Limite > 0 && u.Pct() >= 80 {
					out = append(out, u)
				}
			}
		}
	}
	return out, nil
}

// NovosAlertasOrcamento devolve só os alertas ainda não avisados (cada plano
// avisa uma vez ao passar de 80% e outra ao estourar) e os marca como
// avisados — para a notificação do app e a mensagem diária do bot (cada canal,
// "app" ou "bot", avisa por conta própria).
func NovosAlertasOrcamento(conn *sql.DB, hoje time.Time, canal string) ([]PlanoUso, error) {
	todos, err := AlertasOrcamento(conn, hoje)
	if err != nil {
		return nil, err
	}
	var novos []PlanoUso
	for _, u := range todos {
		nivel := "80"
		if u.Gasto > u.Limite {
			nivel = "100"
		}
		chave := "alerta:" + canal + ":" + u.Categoria + ":" + u.Periodo + ":" + u.Ref + ":" + nivel
		v, err := Pref(conn, chave)
		if err != nil {
			return nil, err
		}
		if v != "" {
			continue
		}
		if err := SetPref(conn, chave, hoje.Format("2006-01-02")); err != nil {
			return nil, err
		}
		novos = append(novos, u)
	}
	return novos, nil
}

// PlanosDaCategoria devolve limite e gasto dos planos (mensal e/ou semanal)
// da categoria que cobrem a data informada — para avisar, por exemplo, quando
// um lançamento novo estoura o limite.
func PlanosDaCategoria(conn *sql.DB, cat, data string) ([]PlanoUso, error) {
	cat = strings.ToLower(strings.TrimSpace(cat))
	var usos []PlanoUso
	for _, per := range []string{"mes", "semana"} {
		ref, err := refDaData(per, data)
		if err != nil {
			return nil, err
		}
		var limite int64
		var acumula bool
		err = conn.QueryRow(
			`SELECT limite, acumula FROM planejamentos WHERE categoria = ? AND periodo = ? AND ref = ?`,
			cat, per, ref,
		).Scan(&limite, &acumula)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		p, err := resolvePeriodo(per, ref)
		if err != nil {
			return nil, err
		}
		gasto, err := gastoDoPlano(conn, cat, p)
		if err != nil {
			return nil, err
		}
		if acumula {
			sobra, err := sobraAcumulada(conn, cat, per, ref)
			if err != nil {
				return nil, err
			}
			limite += sobra
		}
		usos = append(usos, PlanoUso{Periodo: per, Ref: ref, Limite: limite, Gasto: gasto, Categoria: cat})
	}
	return usos, nil
}

func planoRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma plano remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM planejamentos WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("plano #%s não encontrado", args[0])
	}
	fmt.Printf("Plano #%s removido.\n", args[0])
	return nil
}
