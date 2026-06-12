package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

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
		return fmt.Errorf("uso: prisma plano editar <id> [--valor] [--cat]")
	}
	id := args[0]
	fs := flag.NewFlagSet("plano editar", flag.ContinueOnError)
	valor := fs.String("valor", "", "novo limite")
	cat := fs.String("cat", "", "nova categoria")
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
			INSERT INTO planejamentos (categoria, limite, periodo, ref) VALUES (?,?,?,?)
			ON CONFLICT (categoria, periodo, ref) DO UPDATE SET limite = excluded.limite`,
			strings.ToLower(*cat), limite, *per, r,
		)
		if err != nil {
			return err
		}
		fmt.Printf("Plano: até %s em %q no período %s.\n", money.Format(limite), strings.ToLower(*cat), r)
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
		`SELECT id, categoria, limite, periodo, ref FROM planejamentos ORDER BY ref, categoria`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tPERÍODO\tREF\tCATEGORIA\tLIMITE")
	achou := false
	for rows.Next() {
		achou = true
		var id, limite int64
		var cat, per, ref string
		if err := rows.Scan(&id, &cat, &limite, &per, &ref); err != nil {
			return err
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", id, per, ref, cat, money.Format(limite))
	}
	if !achou {
		fmt.Println("Nenhum plano cadastrado. Use: prisma plano add --cat mercado --valor 800")
		return nil
	}
	return w.Flush()
}

// planoStatus compara cada plano do período com os gastos lançados
// (a pagar quitados + pendentes com vencimento dentro do período).
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
		`SELECT categoria, limite FROM planejamentos WHERE periodo = ? AND ref = ? ORDER BY categoria`,
		*per, p.Rotulo,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type linha struct {
		cat           string
		limite, gasto int64
	}
	var linhas []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.cat, &l.limite); err != nil {
			return err
		}
		linhas = append(linhas, l)
	}
	if len(linhas) == 0 {
		fmt.Printf("Nenhum plano para o período %s. Use: prisma plano add --cat mercado --valor 800 --periodo %s --ref %s\n",
			p.Rotulo, *per, p.Rotulo)
		return nil
	}

	fmt.Printf("PLANEJAMENTO %s (%s a %s)\n\n", p.Rotulo, dataBR(p.Inicio), dataBR(somaDias(p.Fim, -1)))
	w := novaTabela()
	fmt.Fprintln(w, "CATEGORIA\tLIMITE\tGASTO\tRESTANTE\tUSO")
	var estourou []string
	for i := range linhas {
		err := conn.QueryRow(`
			SELECT COALESCE(SUM(valor), 0) FROM lancamentos
			WHERE tipo = 'pagar' AND categoria = ?
			  AND ((status = 'quitado' AND quitado_em >= ? AND quitado_em < ?)
			    OR (status = 'pendente' AND vencimento >= ? AND vencimento < ?))`,
			linhas[i].cat, p.Inicio, p.Fim, p.Inicio, p.Fim,
		).Scan(&linhas[i].gasto)
		if err != nil {
			return err
		}
		l := linhas[i]
		restante := l.limite - l.gasto
		pct := float64(0)
		if l.limite > 0 {
			pct = float64(l.gasto) / float64(l.limite) * 100
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s %.0f%%\n",
			l.cat, money.Format(l.limite), money.Format(l.gasto), money.Format(restante),
			barra(l.gasto, l.limite), pct)
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
	Limite, Gasto int64
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
		err = conn.QueryRow(
			`SELECT limite FROM planejamentos WHERE categoria = ? AND periodo = ? AND ref = ?`,
			cat, per, ref,
		).Scan(&limite)
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
		var gasto int64
		err = conn.QueryRow(`
			SELECT COALESCE(SUM(valor), 0) FROM lancamentos
			WHERE tipo = 'pagar' AND categoria = ?
			  AND ((status = 'quitado' AND quitado_em >= ? AND quitado_em < ?)
			    OR (status = 'pendente' AND vencimento >= ? AND vencimento < ?))`,
			cat, p.Inicio, p.Fim, p.Inicio, p.Fim,
		).Scan(&gasto)
		if err != nil {
			return nil, err
		}
		usos = append(usos, PlanoUso{Periodo: per, Ref: ref, Limite: limite, Gasto: gasto})
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
