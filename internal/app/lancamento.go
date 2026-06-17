package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"prisma/internal/money"
)

// NovoLancamento trata `prisma pagar ...` e `prisma receber ...`.
// `prisma pagar add --desc ...` cria; `prisma pagar listar` lista só esse tipo.
func NovoLancamento(conn *sql.DB, tipo string, args []string) error {
	if len(args) == 0 {
		return Lancamentos(conn, []string{"--tipo", tipo})
	}
	switch args[0] {
	case "add", "adicionar":
		return lancamentoAdd(conn, tipo, args[1:])
	case "listar", "ls":
		return Lancamentos(conn, append([]string{"--tipo", tipo}, args[1:]...))
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar)", args[0])
	}
}

// LancamentoParams descreve um lançamento a criar por código (CLI, bot etc.),
// sem nenhuma interação com o terminal.
type LancamentoParams struct {
	Tipo     string // "pagar" ou "receber"
	Desc     string
	Valor    int64  // centavos, sempre positivo
	Cat      string // vazio vira "geral"
	Venc     string // AAAA-MM-DD
	ContaID  int64
	CartID   int64
	GrupoID  int64  // grupo que divide a despesa (0 = sem grupo)
	CartaoID int64  // cartão de crédito; Venc passa a ser a DATA DA COMPRA
	Repetir  int    // repete o lançamento por N meses (0 ou 1 = não repete)
	Parcelas int    // divide o valor TOTAL em N parcelas (0 ou 1 = à vista)
	Obs      string // observação livre
	AutoQuit bool   // quita sozinho ao chegar o vencimento
	Quitado  bool
	// RecebePagamento só vale com GrupoID e Tipo "pagar": a despesa nasce já
	// com a sua parte (valor ÷ pessoas do grupo) e a parte das outras pessoas
	// vira uma receita pendente separada (o reembolso que elas te devem).
	RecebePagamento bool
}

// LancamentoCriado resume um lançamento inserido por CriarLancamentos.
type LancamentoCriado struct {
	ID    int64
	Desc  string
	Valor int64
	Venc  string
}

// CriarLancamentos valida os parâmetros e insere os lançamentos (um ou mais,
// conforme parcelas/repetir). Retorna os criados, as receitas de reembolso
// (uma por despesa, só quando RecebePagamento) e se a categoria é inédita.
func CriarLancamentos(conn *sql.DB, p LancamentoParams) ([]LancamentoCriado, []LancamentoCriado, bool, error) {
	if p.RecebePagamento && (p.GrupoID == 0 || p.Tipo != "pagar") {
		return nil, nil, false, fmt.Errorf("recebe-pagamento exige --grupo e tipo pagar")
	}
	if p.Repetir < 1 {
		p.Repetir = 1
	}
	if p.Parcelas < 1 {
		p.Parcelas = 1
	}
	if p.Cat == "" {
		p.Cat = "geral"
	}
	if p.Desc == "" {
		return nil, nil, false, fmt.Errorf("a descrição é obrigatória")
	}
	if p.Valor <= 0 {
		return nil, nil, false, fmt.Errorf("o valor deve ser positivo")
	}
	if p.ContaID != 0 && p.CartID != 0 {
		return nil, nil, false, fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
	}
	if p.Repetir > 120 {
		return nil, nil, false, fmt.Errorf("repetir deve estar entre 1 e 120")
	}
	if p.Parcelas > 120 {
		return nil, nil, false, fmt.Errorf("parcelas deve estar entre 1 e 120")
	}
	if p.Parcelas > 1 && p.Repetir > 1 {
		return nil, nil, false, fmt.Errorf("use parcelas (divide o total) OU repetir (repete o valor), não ambos")
	}
	if _, err := parseDataT(p.Venc); err != nil {
		return nil, nil, false, fmt.Errorf("vencimento inválido: %q", p.Venc)
	}
	categoriaNova := avisaCategoriaNova(conn, p.Cat)
	registraCategoria(conn, p.Cat)

	var conta, carteira any
	if p.ContaID != 0 {
		if err := existe(conn, "contas", p.ContaID); err != nil {
			return nil, nil, false, err
		}
		conta = p.ContaID
	}
	if p.CartID != 0 {
		if err := existe(conn, "carteiras", p.CartID); err != nil {
			return nil, nil, false, err
		}
		carteira = p.CartID
	}
	var grupo any
	if p.GrupoID != 0 {
		if err := existe(conn, "grupos", p.GrupoID); err != nil {
			return nil, nil, false, err
		}
		grupo = p.GrupoID
	}

	// Cartão: a despesa cai numa fatura. A data informada vira a DATA DA COMPRA
	// e o vencimento passa a ser o da fatura; o débito sai da conta do cartão.
	var cartao any
	dataCompraBase := "" // vazio = não é cartão
	if p.CartaoID != 0 {
		if p.Tipo != "pagar" {
			return nil, nil, false, fmt.Errorf("cartão só vale para despesas (pagar)")
		}
		var fech, venc int
		var contaCartao sql.NullInt64
		err := conn.QueryRow(
			`SELECT dia_fechamento, dia_vencimento, conta_id FROM cartoes WHERE id = ?`, p.CartaoID,
		).Scan(&fech, &venc, &contaCartao)
		if err == sql.ErrNoRows {
			return nil, nil, false, fmt.Errorf("cartão #%d não encontrado", p.CartaoID)
		}
		if err != nil {
			return nil, nil, false, err
		}
		cartao = p.CartaoID
		// a fatura é paga pela conta do cartão, não pela conta/carteira informada
		conta, carteira = nil, nil
		if contaCartao.Valid {
			conta = contaCartao.Int64
		}
		dataCompraBase = p.Venc
		compraT, _ := parseDataT(p.Venc)
		_, p.Venc = faturaDe(fech, venc, compraT) // vencimento vira o da fatura
		p.Quitado = false                         // nasce pendente; quita ao pagar a fatura
	}

	status, quitadoEm := "pendente", any(nil)
	if p.Quitado {
		status, quitadoEm = "quitado", p.Venc
	}

	n := p.Repetir
	if p.Parcelas > 1 {
		n = p.Parcelas
	}
	autoQuit := 0
	if p.AutoQuit {
		autoQuit = 1
	}
	// recebe-pagamento: cada despesa nasce só com a sua parte; o resto vira
	// uma receita de reembolso pendente, vinculada à despesa que a gerou.
	var pessoasGrupo int64
	if p.RecebePagamento {
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM grupo_pessoas WHERE grupo_id = ?`, p.GrupoID,
		).Scan(&pessoasGrupo); err != nil {
			return nil, nil, false, err
		}
		if pessoasGrupo < 1 {
			pessoasGrupo = 1
		}
		registraCategoria(conn, "reembolso")
	}

	criados := make([]LancamentoCriado, 0, n)
	var reembolsos []LancamentoCriado
	var ids []int64
	for i := 0; i < n; i++ {
		valorItem, descItem := p.Valor, p.Desc
		if p.Parcelas > 1 {
			// divide o total; a última parcela absorve o resto da divisão
			valorItem = p.Valor / int64(n)
			if i == n-1 {
				valorItem = p.Valor - valorItem*int64(n-1)
			}
			descItem = fmt.Sprintf("%s (%d/%d)", p.Desc, i+1, n)
		}
		vencItem := somaMeses(p.Venc, i)
		var dataCompraItem any
		if dataCompraBase != "" {
			// cada parcela do cartão entra na fatura seguinte e conta no seu mês
			dataCompraItem = somaMeses(dataCompraBase, i)
		}
		minhaParte, outrosDevem := valorItem, int64(0)
		recebePagItem := 0
		if p.RecebePagamento {
			minhaParte = valorItem / pessoasGrupo
			outrosDevem = valorItem - minhaParte
			recebePagItem = 1
		}
		res, err := conn.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, carteira_id, grupo_id, cartao_id, data_compra, observacao, auto_quitar, recebe_pagamento)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Tipo, descItem, minhaParte, strings.ToLower(p.Cat), vencItem, status, quitadoEm, conta, carteira, grupo, cartao, dataCompraItem, p.Obs, autoQuit, recebePagItem,
		)
		if err != nil {
			return criados, reembolsos, categoriaNova, err
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
		criados = append(criados, LancamentoCriado{id, descItem, minhaParte, vencItem})

		if p.RecebePagamento && outrosDevem > 0 {
			descReembolso := fmt.Sprintf("Reembolso: %s", descItem)
			resR, err := conn.Exec(`
				INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, conta_id, carteira_id, reembolso_de)
				VALUES ('receber',?,?,'reembolso',?,'pendente',NULL,NULL,?)`,
				descReembolso, outrosDevem, vencItem, id,
			)
			if err != nil {
				return criados, reembolsos, categoriaNova, err
			}
			idR, _ := resR.LastInsertId()
			reembolsos = append(reembolsos, LancamentoCriado{idR, descReembolso, outrosDevem, vencItem})
		}
	}
	// parcelado: liga todas as parcelas à raiz (a 1ª), para excluir em bloco
	if p.Parcelas > 1 && len(ids) > 0 {
		raiz := ids[0]
		for _, id := range ids {
			if _, err := conn.Exec(`UPDATE lancamentos SET parcela_grupo = ? WHERE id = ?`, raiz, id); err != nil {
				return criados, reembolsos, categoriaNova, err
			}
		}
	}
	return criados, reembolsos, categoriaNova, nil
}

func lancamentoAdd(conn *sql.DB, tipo string, args []string) error {
	fs := flag.NewFlagSet(tipo+" add", flag.ContinueOnError)
	desc := fs.String("desc", "", "descrição (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório, ex.: 1.200,00)")
	venc := fs.String("venc", "hoje", "vencimento (AAAA-MM-DD ou DD/MM/AAAA)")
	cat := fs.String("cat", "geral", "categoria (ex.: moradia, mercado, salario)")
	contaID := fs.Int64("conta", 0, "id da conta vinculada")
	cartID := fs.Int64("carteira", 0, "id da carteira vinculada")
	grupoID := fs.Int64("grupo", 0, "id do grupo que divide a despesa")
	cartaoID := fs.Int64("cartao", 0, "id do cartão (a data vira a da compra; vai pra fatura)")
	repetir := fs.Int("repetir", 1, "repete o lançamento por N meses consecutivos")
	parcelas := fs.Int("parcelas", 1, "divide o valor TOTAL em N parcelas mensais")
	obs := fs.String("obs", "", "observação livre")
	autoQuit := fs.Bool("auto-quitar", false, "quita sozinho ao chegar o vencimento")
	quitado := fs.Bool("quitado", false, "já marca como quitado (pago/recebido)")
	recebePag := fs.Bool("recebe-pagamento", false, "as outras pessoas do grupo te pagam: a despesa fica só com a sua parte e o resto vira receita de reembolso")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *desc == "" || *valor == "" {
		return fmt.Errorf("--desc e --valor são obrigatórios")
	}
	if *repetir < 1 {
		return fmt.Errorf("--repetir deve estar entre 1 e 120")
	}
	if *parcelas < 1 {
		return fmt.Errorf("--parcelas deve estar entre 1 e 120")
	}
	if *recebePag && *grupoID == 0 {
		return fmt.Errorf("--recebe-pagamento exige --grupo")
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	data, err := parseData(*venc)
	if err != nil {
		return err
	}
	criados, reembolsos, categoriaNova, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: tipo, Desc: *desc, Valor: centavos, Cat: *cat, Venc: data,
		ContaID: *contaID, CartID: *cartID, GrupoID: *grupoID, CartaoID: *cartaoID,
		Repetir: *repetir, Parcelas: *parcelas, Obs: *obs, AutoQuit: *autoQuit, Quitado: *quitado,
		RecebePagamento: *recebePag,
	})
	if err != nil {
		return err
	}
	rotulo := map[string]string{"pagar": "a pagar", "receber": "a receber"}[tipo]
	for _, c := range criados {
		fmt.Printf("Lançamento #%d (%s) %q de %s com vencimento em %s criado.\n",
			c.ID, rotulo, c.Desc, money.Format(c.Valor), dataBR(c.Venc))
	}
	for _, r := range reembolsos {
		fmt.Printf("Receita de reembolso #%d %q de %s com vencimento em %s criada.\n",
			r.ID, r.Desc, money.Format(r.Valor), dataBR(r.Venc))
	}
	if categoriaNova {
		fmt.Printf("Aviso: primeira vez usando a categoria %q — confira se não é um erro de digitação.\n",
			strings.ToLower(*cat))
	}
	return nil
}

// avisaCategoriaNova diz se a categoria nunca foi usada (proteção contra typos
// como "mercado" vs "mercados", que viram categorias diferentes em silêncio).
func avisaCategoriaNova(conn *sql.DB, cat string) bool {
	c := strings.ToLower(strings.TrimSpace(cat))
	if c == "" || c == "geral" {
		return false
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE categoria = ?`, c).Scan(&n); err != nil {
		return false
	}
	return n == 0
}

func existe(conn *sql.DB, tabela string, id int64) error {
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM `+tabela+` WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		singular := strings.TrimSuffix(tabela, "s")
		return fmt.Errorf("%s #%d não encontrada", singular, id)
	}
	return nil
}

// Lancamentos lista lançamentos: `prisma lancamentos [--pendentes] [--tipo pagar|receber] [--mes AAAA-MM]`.
// Também aceita `prisma lancamentos remover <id>`.
func Lancamentos(conn *sql.DB, args []string) error {
	if len(args) > 0 && (args[0] == "remover" || args[0] == "rm") {
		return lancamentoRemover(conn, args[1:])
	}
	if len(args) > 0 && args[0] == "editar" {
		return lancamentoEditar(conn, args[1:])
	}
	fs := flag.NewFlagSet("lancamentos", flag.ContinueOnError)
	pendentes := fs.Bool("pendentes", false, "mostra só os pendentes")
	tipo := fs.String("tipo", "", "filtra por tipo: pagar ou receber")
	mes := fs.String("mes", "", "filtra por mês de vencimento (AAAA-MM)")
	de := fs.String("de", "", "vencimento a partir desta data")
	ate := fs.String("ate", "", "vencimento até esta data (inclusive)")
	cat := fs.String("cat", "", "filtra por categoria")
	grupo := fs.String("grupo", "", "filtra pelos lançamentos vinculados a um grupo")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := `SELECT l.id, l.tipo, l.descricao, ` + valEf("l") + `, l.categoria, l.vencimento, l.status, l.quitado_em,
		g.nome, (SELECT COUNT(*) FROM grupo_pessoas gp WHERE gp.grupo_id = l.grupo_id),
		ca.nome, l.vencimento, l.observacao, l.auto_quitar
		FROM lancamentos l
		LEFT JOIN grupos g ON g.id = l.grupo_id
		LEFT JOIN cartoes ca ON ca.id = l.cartao_id WHERE 1=1`
	var params []any
	if *pendentes {
		query += ` AND status = 'pendente'`
	}
	if *grupo != "" {
		query += ` AND l.grupo_id = ?`
		params = append(params, *grupo)
	}
	if *tipo != "" {
		query += ` AND tipo = ?`
		params = append(params, *tipo)
	}
	if *mes != "" {
		p, err := periodoMes(*mes)
		if err != nil {
			return err
		}
		query += ` AND vencimento >= ? AND vencimento < ?`
		params = append(params, p.Inicio, p.Fim)
	}
	if *de != "" {
		d, err := parseData(*de)
		if err != nil {
			return err
		}
		query += ` AND vencimento >= ?`
		params = append(params, d)
	}
	if *ate != "" {
		d, err := parseData(*ate)
		if err != nil {
			return err
		}
		query += ` AND vencimento <= ?`
		params = append(params, d)
	}
	if *cat != "" {
		query += ` AND categoria = ?`
		params = append(params, strings.ToLower(*cat))
	}
	query += ` ORDER BY l.vencimento, l.id`

	rows, err := conn.Query(query, params...)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVENCIMENTO\tVALOR\tGRUPO\tCARTÃO\tOBS\tSTATUS")
	achou := false
	hoje, _ := parseData("hoje")
	var totPagar, totReceber int64
	for rows.Next() {
		achou = true
		var id, valor int64
		var t, desc, categoria, venc, status, obs string
		var quitadoEm, grupo, cartao, cartaoVenc sql.NullString
		var pessoas, autoQuit int
		if err := rows.Scan(&id, &t, &desc, &valor, &categoria, &venc, &status, &quitadoEm, &grupo, &pessoas, &cartao, &cartaoVenc, &obs, &autoQuit); err != nil {
			return err
		}
		st := "pendente"
		switch {
		case status == "quitado":
			st = "quitado em " + dataBR(quitadoEm.String)
		case venc < hoje:
			st = "⚠ atrasada" // vencida e ainda não quitada
		}
		if autoQuit == 1 && status == "pendente" {
			st += " ⏱" // quita sozinho no vencimento
		}
		if status == "pendente" {
			if t == "pagar" {
				totPagar += valor
			} else {
				totReceber += valor
			}
		}
		// valor já vem como a parte efetiva (dividida pelo grupo, se houver);
		// a coluna grupo mostra qual grupo divide e por quanto ("-" se nenhum)
		grp := "-"
		if grupo.Valid && pessoas > 0 {
			grp = fmt.Sprintf("%s ÷%d", grupo.String, pessoas)
		}
		// a coluna cartão mostra o cartão e a fatura (mês do vencimento)
		crt := "-"
		if cartao.Valid {
			crt = cartao.String
			if len(cartaoVenc.String) >= 7 {
				crt += " " + cartaoVenc.String[5:7] + "/" + cartaoVenc.String[2:4]
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			id, t, desc, categoria, dataBR(venc), money.Format(valor), grp, crt, ouTraco(truncar(obs, 24)), st)
	}
	if !achou {
		fmt.Println("Nenhum lançamento encontrado.")
		return nil
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nPendente a pagar: %s | Pendente a receber: %s\n",
		money.Format(totPagar), money.Format(totReceber))
	return nil
}

// lancamentoEditar altera só os campos informados:
// `prisma lancamentos editar <id> [--desc] [--valor] [--venc] [--cat] [--conta N] [--carteira N] [--grupo N]`.
// Use --conta 0 (ou --carteira 0, --grupo 0) para remover o vínculo.
func lancamentoEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma lancamentos editar <id> [--desc] [--valor] [--venc] [--cat] [--conta] [--carteira]")
	}
	id := args[0]
	fs := flag.NewFlagSet("lancamentos editar", flag.ContinueOnError)
	desc := fs.String("desc", "", "nova descrição")
	valor := fs.String("valor", "", "novo valor")
	venc := fs.String("venc", "", "novo vencimento")
	cat := fs.String("cat", "", "nova categoria")
	contaID := fs.Int64("conta", -1, "vincular à conta (0 desvincula)")
	cartID := fs.Int64("carteira", -1, "vincular à carteira (0 desvincula)")
	grupoID := fs.Int64("grupo", -1, "vincular ao grupo que divide (0 desvincula)")
	cartaoID := fs.Int64("cartao", -1, "mover para o cartão, na fatura (0 desvincula)")
	obs := fs.String("obs", "", "observação livre (use \"-\" para limpar)")
	autoQuit := fs.String("auto-quitar", "", "quita no vencimento: sim | nao")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })

	var sets []string
	var params []any
	if informado["desc"] {
		if *desc == "" {
			return fmt.Errorf("a descrição não pode ficar vazia")
		}
		sets, params = append(sets, "descricao = ?"), append(params, *desc)
	}
	if informado["valor"] {
		c, err := money.Parse(*valor)
		if err != nil {
			return err
		}
		if c <= 0 {
			return fmt.Errorf("o valor deve ser positivo")
		}
		sets, params = append(sets, "valor = ?"), append(params, c)
	}
	if informado["venc"] {
		d, err := parseData(*venc)
		if err != nil {
			return err
		}
		sets, params = append(sets, "vencimento = ?"), append(params, d)
	}
	if informado["cat"] {
		if avisaCategoriaNova(conn, *cat) {
			defer fmt.Printf("Aviso: primeira vez usando a categoria %q — confira se não é um erro de digitação.\n",
				strings.ToLower(*cat))
		}
		registraCategoria(conn, *cat)
		sets, params = append(sets, "categoria = ?"), append(params, strings.ToLower(*cat))
	}
	if informado["conta"] && informado["carteira"] && *contaID > 0 && *cartID > 0 {
		return fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
	}
	if informado["conta"] {
		if *contaID > 0 {
			if err := existe(conn, "contas", *contaID); err != nil {
				return err
			}
			sets = append(sets, "conta_id = ?", "carteira_id = NULL")
			params = append(params, *contaID)
		} else {
			sets = append(sets, "conta_id = NULL")
		}
	}
	if informado["carteira"] {
		if *cartID > 0 {
			if err := existe(conn, "carteiras", *cartID); err != nil {
				return err
			}
			sets = append(sets, "carteira_id = ?", "conta_id = NULL")
			params = append(params, *cartID)
		} else {
			sets = append(sets, "carteira_id = NULL")
		}
	}
	if informado["grupo"] {
		if *grupoID > 0 {
			if err := existe(conn, "grupos", *grupoID); err != nil {
				return err
			}
			sets = append(sets, "grupo_id = ?")
			params = append(params, *grupoID)
		} else {
			sets = append(sets, "grupo_id = NULL")
		}
	}
	if informado["obs"] {
		valor := *obs
		if valor == "-" { // "-" limpa a observação
			valor = ""
		}
		sets, params = append(sets, "observacao = ?"), append(params, valor)
	}
	if informado["auto-quitar"] {
		v := 0
		switch strings.ToLower(strings.TrimSpace(*autoQuit)) {
		case "sim", "s", "1", "true":
			v = 1
		case "nao", "não", "n", "0", "false":
			v = 0
		default:
			return fmt.Errorf("--auto-quitar deve ser sim ou nao")
		}
		sets, params = append(sets, "auto_quitar = ?"), append(params, v)
	}
	if informado["cartao"] {
		if *cartaoID > 0 {
			var fech, venc int
			var contaCartao sql.NullInt64
			err := conn.QueryRow(
				`SELECT dia_fechamento, dia_vencimento, conta_id FROM cartoes WHERE id = ?`, *cartaoID,
			).Scan(&fech, &venc, &contaCartao)
			if err == sql.ErrNoRows {
				return fmt.Errorf("cartão #%d não encontrado", *cartaoID)
			}
			if err != nil {
				return err
			}
			// o vencimento atual vira a data da compra; recalcula a fatura
			var vencAtual string
			if err := conn.QueryRow(`SELECT vencimento FROM lancamentos WHERE id = ?`, id).Scan(&vencAtual); err != nil {
				return err
			}
			compraT, _ := parseDataT(vencAtual)
			_, novoVenc := faturaDe(fech, venc, compraT)
			sets = append(sets, "cartao_id = ?", "data_compra = ?", "vencimento = ?", "carteira_id = NULL")
			params = append(params, *cartaoID, vencAtual, novoVenc)
			if contaCartao.Valid {
				sets = append(sets, "conta_id = ?")
				params = append(params, contaCartao.Int64)
			} else {
				sets = append(sets, "conta_id = NULL")
			}
		} else {
			sets = append(sets, "cartao_id = NULL", "data_compra = NULL")
		}
	}
	if len(sets) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}

	params = append(params, id)
	res, err := conn.Exec(`UPDATE lancamentos SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lançamento #%s não encontrado", id)
	}
	fmt.Printf("Lançamento #%s atualizado.\n", id)
	return nil
}

func lancamentoRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma lancamentos remover <id>")
	}
	id := args[0]
	// se for a parcela raiz (parcela_grupo aponta para si mesma), remove todas as
	// parcelas do grupo; senão, só o lançamento informado.
	var parcelaGrupo sql.NullInt64
	if err := conn.QueryRow(`SELECT parcela_grupo FROM lancamentos WHERE id = ?`, id).Scan(&parcelaGrupo); err == sql.ErrNoRows {
		return fmt.Errorf("lançamento #%s não encontrado", id)
	} else if err != nil {
		return err
	}
	if parcelaGrupo.Valid && fmt.Sprint(parcelaGrupo.Int64) == id {
		res, err := conn.Exec(`DELETE FROM lancamentos WHERE parcela_grupo = ?`, parcelaGrupo.Int64)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		fmt.Printf("Parcela raiz #%s removida com todas as %d parcela(s) do grupo.\n", id, n)
		return nil
	}
	res, err := conn.Exec(`DELETE FROM lancamentos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lançamento #%s não encontrado", id)
	}
	fmt.Printf("Lançamento #%s removido.\n", id)
	return nil
}

// QuitarVencidos quita automaticamente os lançamentos pendentes marcados com
// auto_quitar cujo vencimento já chegou, usando a própria data de vencimento.
// Idempotente; roda a cada inicialização do Prisma e do bot.
func QuitarVencidos(conn *sql.DB) (int, error) {
	hoje, _ := parseData("hoje")
	res, err := conn.Exec(`
		UPDATE lancamentos SET status = 'quitado', quitado_em = vencimento
		WHERE status = 'pendente' AND auto_quitar = 1 AND vencimento <= ?`, hoje)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Quitar marca um lançamento como pago/recebido: `prisma quitar <id> [--data AAAA-MM-DD]`.
func Quitar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma quitar <id> [--data AAAA-MM-DD]")
	}
	id := args[0]
	fs := flag.NewFlagSet("quitar", flag.ContinueOnError)
	data := fs.String("data", "hoje", "data do pagamento/recebimento")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	res, err := conn.Exec(
		`UPDATE lancamentos SET status = 'quitado', quitado_em = ? WHERE id = ? AND status = 'pendente'`,
		d, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lançamento #%s não encontrado ou já quitado", id)
	}
	fmt.Printf("Lançamento #%s quitado em %s.\n", id, dataBR(d))
	return nil
}
