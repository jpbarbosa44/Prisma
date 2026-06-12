package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	"prisma/internal/money"
)

// Recorrencia trata `prisma recorrencia add|listar|remover|gerar`.
// Uma recorrência é uma regra ("salário todo dia 1") que materializa
// lançamentos pendentes automaticamente, 3 meses à frente.
func Recorrencia(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return recorrenciaAdd(conn, args[1:])
	case "listar", "ls":
		return recorrenciaListar(conn)
	case "editar":
		return recorrenciaEditar(conn, args[1:])
	case "remover", "rm":
		return recorrenciaRemover(conn, args[1:])
	case "gerar":
		n, err := GerarRecorrencias(conn)
		if err != nil {
			return err
		}
		fmt.Printf("%d lançamento(s) gerado(s).\n", n)
		return nil
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, remover, gerar)", args[0])
	}
}

func recorrenciaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("recorrencia add", flag.ContinueOnError)
	tipo := fs.String("tipo", "", "pagar ou receber (obrigatório)")
	desc := fs.String("desc", "", "descrição (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório)")
	dia := fs.Int("dia", 0, "dia do mês, 1 a 31 (obrigatório)")
	cat := fs.String("cat", "geral", "categoria")
	contaID := fs.Int64("conta", 0, "id da conta vinculada")
	cartID := fs.Int64("carteira", 0, "id da carteira vinculada")
	inicio := fs.String("inicio", "hoje", "a partir de quando vale")
	fim := fs.String("fim", "", "até quando vale (vazio = sem fim)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tipo != "pagar" && *tipo != "receber" {
		return fmt.Errorf("--tipo deve ser pagar ou receber")
	}
	if *desc == "" || *valor == "" {
		return fmt.Errorf("--desc e --valor são obrigatórios")
	}
	if *dia < 1 || *dia > 31 {
		return fmt.Errorf("--dia deve estar entre 1 e 31")
	}
	if *contaID != 0 && *cartID != 0 {
		return fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if centavos <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	dIni, err := parseData(*inicio)
	if err != nil {
		return err
	}
	var dFim any
	if *fim != "" {
		d, err := parseData(*fim)
		if err != nil {
			return err
		}
		if d < dIni {
			return fmt.Errorf("--fim não pode ser antes de --inicio")
		}
		dFim = d
	}
	var conta, carteira any
	if *contaID != 0 {
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	}
	if *cartID != 0 {
		if err := existe(conn, "carteiras", *cartID); err != nil {
			return err
		}
		carteira = *cartID
	}
	categoriaNova := avisaCategoriaNova(conn, *cat)

	res, err := conn.Exec(`
		INSERT INTO recorrencias (tipo, descricao, valor, categoria, dia, conta_id, carteira_id, inicio, fim)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		*tipo, *desc, centavos, strings.ToLower(*cat), *dia, conta, carteira, dIni, dFim,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Recorrência #%d criada: %s %q de %s todo dia %d.\n",
		id, *tipo, *desc, money.Format(centavos), *dia)
	if categoriaNova {
		fmt.Printf("Aviso: primeira vez usando a categoria %q — confira se não é um erro de digitação.\n",
			strings.ToLower(*cat))
	}
	n, err := GerarRecorrencias(conn)
	if err != nil {
		return err
	}
	if n > 0 {
		fmt.Printf("%d lançamento(s) gerado(s) para os próximos meses.\n", n)
	}
	return nil
}

// recorrenciaEditar altera a regra E os lançamentos pendentes já gerados por
// ela (os quitados ficam intactos, são histórico):
// `prisma recorrencia editar <id> [--desc] [--valor] [--dia] [--cat] [--conta] [--carteira] [--fim]`.
// Use --fim nunca para remover a data de término; --conta/--carteira 0 desvincula.
func recorrenciaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma recorrencia editar <id> [--desc] [--valor] [--dia] [--cat] [--conta] [--carteira] [--fim]")
	}
	id := args[0]
	fs := flag.NewFlagSet("recorrencia editar", flag.ContinueOnError)
	desc := fs.String("desc", "", "nova descrição")
	valor := fs.String("valor", "", "novo valor")
	dia := fs.Int("dia", 0, "novo dia do mês")
	cat := fs.String("cat", "", "nova categoria")
	contaID := fs.Int64("conta", -1, "vincular à conta (0 desvincula)")
	cartID := fs.Int64("carteira", -1, "vincular à carteira (0 desvincula)")
	fim := fs.String("fim", "", "nova data de término (ou \"nunca\" para remover)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	if len(informado) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}

	// carrega a regra atual e aplica as mudanças por cima
	var r struct {
		desc, cat, inicio string
		valor             int64
		dia               int
		conta, carteira   sql.NullInt64
		fim               sql.NullString
	}
	err := conn.QueryRow(`
		SELECT descricao, valor, categoria, dia, conta_id, carteira_id, inicio, fim
		FROM recorrencias WHERE id = ?`, id,
	).Scan(&r.desc, &r.valor, &r.cat, &r.dia, &r.conta, &r.carteira, &r.inicio, &r.fim)
	if err == sql.ErrNoRows {
		return fmt.Errorf("recorrência #%s não encontrada", id)
	}
	if err != nil {
		return err
	}

	if informado["desc"] {
		if *desc == "" {
			return fmt.Errorf("a descrição não pode ficar vazia")
		}
		r.desc = *desc
	}
	if informado["valor"] {
		v, err := money.Parse(*valor)
		if err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("o valor deve ser positivo")
		}
		r.valor = v
	}
	if informado["dia"] {
		if *dia < 1 || *dia > 31 {
			return fmt.Errorf("--dia deve estar entre 1 e 31")
		}
		r.dia = *dia
	}
	if informado["cat"] {
		if *cat == "" {
			return fmt.Errorf("a categoria não pode ficar vazia")
		}
		r.cat = strings.ToLower(*cat)
	}
	if informado["conta"] {
		if *contaID > 0 {
			if err := existe(conn, "contas", *contaID); err != nil {
				return err
			}
			r.conta = sql.NullInt64{Int64: *contaID, Valid: true}
			r.carteira = sql.NullInt64{}
		} else {
			r.conta = sql.NullInt64{}
		}
	}
	if informado["carteira"] {
		if *cartID > 0 {
			if informado["conta"] && *contaID > 0 {
				return fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
			}
			if err := existe(conn, "carteiras", *cartID); err != nil {
				return err
			}
			r.carteira = sql.NullInt64{Int64: *cartID, Valid: true}
			r.conta = sql.NullInt64{}
		} else {
			r.carteira = sql.NullInt64{}
		}
	}
	if informado["fim"] {
		if strings.ToLower(*fim) == "nunca" {
			r.fim = sql.NullString{}
		} else {
			d, err := parseData(*fim)
			if err != nil {
				return err
			}
			if d < r.inicio {
				return fmt.Errorf("--fim não pode ser antes do início (%s)", dataBR(r.inicio))
			}
			r.fim = sql.NullString{String: d, Valid: true}
		}
	}

	var conta, carteira, dFim any
	if r.conta.Valid {
		conta = r.conta.Int64
	}
	if r.carteira.Valid {
		carteira = r.carteira.Int64
	}
	if r.fim.Valid {
		dFim = r.fim.String
	}
	_, err = conn.Exec(`
		UPDATE recorrencias SET descricao = ?, valor = ?, categoria = ?, dia = ?,
		       conta_id = ?, carteira_id = ?, fim = ? WHERE id = ?`,
		r.desc, r.valor, r.cat, r.dia, conta, carteira, dFim, id,
	)
	if err != nil {
		return err
	}

	// propaga aos pendentes já gerados
	res, err := conn.Exec(`
		UPDATE lancamentos SET descricao = ?, valor = ?, categoria = ?, conta_id = ?, carteira_id = ?
		WHERE recorrencia_id = ? AND status = 'pendente'`,
		r.desc, r.valor, r.cat, conta, carteira, id,
	)
	if err != nil {
		return err
	}
	atualizados, _ := res.RowsAffected()

	if informado["dia"] {
		rows, err := conn.Query(
			`SELECT id, vencimento FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'`, id)
		if err != nil {
			return err
		}
		type mov struct {
			id   int64
			venc string
		}
		var movs []mov
		for rows.Next() {
			var m mov
			if err := rows.Scan(&m.id, &m.venc); err != nil {
				rows.Close()
				return err
			}
			movs = append(movs, m)
		}
		rows.Close()
		for _, m := range movs {
			novo := diaNoMes(m.venc[:7], r.dia)
			if _, err := conn.Exec(`UPDATE lancamentos SET vencimento = ? WHERE id = ?`, novo, m.id); err != nil {
				return err
			}
		}
	}

	removidos := int64(0)
	if r.fim.Valid {
		res, err := conn.Exec(`
			DELETE FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente' AND vencimento > ?`,
			id, r.fim.String)
		if err != nil {
			return err
		}
		removidos, _ = res.RowsAffected()
	}

	fmt.Printf("Recorrência #%s atualizada", id)
	if atualizados > 0 {
		fmt.Printf("; %d lançamento(s) pendente(s) ajustado(s)", atualizados)
	}
	if removidos > 0 {
		fmt.Printf("; %d removido(s) por ficarem após o término", removidos)
	}
	fmt.Println(".")
	return nil
}

// GerarRecorrencias materializa os lançamentos pendentes de todas as regras
// até 3 meses à frente. É chamada a cada execução do Prisma; idempotente.
func GerarRecorrencias(conn *sql.DB) (int, error) {
	rows, err := conn.Query(`
		SELECT id, tipo, descricao, valor, categoria, dia, conta_id, carteira_id,
		       inicio, COALESCE(fim, ''), ultima_ref
		FROM recorrencias`)
	if err != nil {
		return 0, err
	}
	type regra struct {
		id, valor          int64
		dia                int
		tipo, desc, cat    string
		conta, carteira    sql.NullInt64
		inicio, fim, refUl string
	}
	var regras []regra
	for rows.Next() {
		var r regra
		if err := rows.Scan(&r.id, &r.tipo, &r.desc, &r.valor, &r.cat, &r.dia,
			&r.conta, &r.carteira, &r.inicio, &r.fim, &r.refUl); err != nil {
			rows.Close()
			return 0, err
		}
		regras = append(regras, r)
	}
	rows.Close()

	horizonte := time.Now().AddDate(0, 3, 0).Format("2006-01")
	total := 0
	for _, r := range regras {
		ref := r.inicio[:7] // AAAA-MM do início
		if r.refUl != "" && r.refUl >= ref {
			ref = proximoMes(r.refUl)
		}
		for ref <= horizonte {
			venc := diaNoMes(ref, r.dia)
			if venc >= r.inicio && (r.fim == "" || venc <= r.fim) {
				var conta, carteira any
				if r.conta.Valid {
					conta = r.conta.Int64
				}
				if r.carteira.Valid {
					carteira = r.carteira.Int64
				}
				_, err := conn.Exec(`
					INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, conta_id, carteira_id, recorrencia_id)
					VALUES (?,?,?,?,?,?,?,?)`,
					r.tipo, r.desc, r.valor, r.cat, venc, conta, carteira, r.id,
				)
				if err != nil {
					return total, err
				}
				total++
			}
			ref = proximoMes(ref)
		}
		if _, err := conn.Exec(`UPDATE recorrencias SET ultima_ref = ? WHERE id = ?`, horizonte, r.id); err != nil {
			return total, err
		}
	}
	return total, nil
}

// proximoMes avança uma referência AAAA-MM em um mês.
func proximoMes(ref string) string {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return ref
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// diaNoMes monta a data AAAA-MM-DD travando o dia no fim do mês (31 → 28/30).
func diaNoMes(ref string, dia int) string {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return ref + "-01"
	}
	ultimo := t.AddDate(0, 1, -1).Day()
	if dia > ultimo {
		dia = ultimo
	}
	return time.Date(t.Year(), t.Month(), dia, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func recorrenciaListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT id, tipo, descricao, valor, categoria, dia, inicio, COALESCE(fim, '') FROM recorrencias ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVALOR\tDIA\tVIGÊNCIA")
	achou := false
	for rows.Next() {
		achou = true
		var id, valor int64
		var dia int
		var tipo, desc, cat, ini, fim string
		if err := rows.Scan(&id, &tipo, &desc, &valor, &cat, &dia, &ini, &fim); err != nil {
			return err
		}
		vig := "desde " + dataBR(ini)
		if fim != "" {
			vig = dataBR(ini) + " a " + dataBR(fim)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%s\n", id, tipo, desc, cat, money.Format(valor), dia, vig)
	}
	if !achou {
		fmt.Println("Nenhuma recorrência. Use: prisma recorrencia add --tipo receber --desc \"Salário\" --valor 5000 --dia 1")
		return nil
	}
	return w.Flush()
}

// recorrenciaRemover apaga a regra; com --limpar, apaga também os lançamentos
// pendentes que ela gerou (os quitados ficam, são histórico).
func recorrenciaRemover(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma recorrencia remover <id> [--limpar]")
	}
	id := args[0]
	fs := flag.NewFlagSet("recorrencia remover", flag.ContinueOnError)
	limpar := fs.Bool("limpar", false, "remove também os lançamentos pendentes gerados")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *limpar {
		res, err := conn.Exec(
			`DELETE FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			fmt.Printf("%d lançamento(s) pendente(s) removido(s).\n", n)
		}
	}
	res, err := conn.Exec(`DELETE FROM recorrencias WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("recorrência #%s não encontrada", id)
	}
	fmt.Printf("Recorrência #%s removida.\n", id)
	return nil
}
