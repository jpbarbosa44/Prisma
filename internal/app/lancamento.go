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
	Repetir  int // repete o lançamento por N meses (0 ou 1 = não repete)
	Parcelas int // divide o valor TOTAL em N parcelas (0 ou 1 = à vista)
	Quitado  bool
}

// LancamentoCriado resume um lançamento inserido por CriarLancamentos.
type LancamentoCriado struct {
	ID    int64
	Desc  string
	Valor int64
	Venc  string
}

// CriarLancamentos valida os parâmetros e insere os lançamentos (um ou mais,
// conforme parcelas/repetir). Retorna os criados e se a categoria é inédita.
func CriarLancamentos(conn *sql.DB, p LancamentoParams) ([]LancamentoCriado, bool, error) {
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
		return nil, false, fmt.Errorf("a descrição é obrigatória")
	}
	if p.Valor <= 0 {
		return nil, false, fmt.Errorf("o valor deve ser positivo")
	}
	if p.ContaID != 0 && p.CartID != 0 {
		return nil, false, fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
	}
	if p.Repetir > 120 {
		return nil, false, fmt.Errorf("repetir deve estar entre 1 e 120")
	}
	if p.Parcelas > 120 {
		return nil, false, fmt.Errorf("parcelas deve estar entre 1 e 120")
	}
	if p.Parcelas > 1 && p.Repetir > 1 {
		return nil, false, fmt.Errorf("use parcelas (divide o total) OU repetir (repete o valor), não ambos")
	}
	if _, err := parseDataT(p.Venc); err != nil {
		return nil, false, fmt.Errorf("vencimento inválido: %q", p.Venc)
	}
	categoriaNova := avisaCategoriaNova(conn, p.Cat)

	var conta, carteira any
	if p.ContaID != 0 {
		if err := existe(conn, "contas", p.ContaID); err != nil {
			return nil, false, err
		}
		conta = p.ContaID
	}
	if p.CartID != 0 {
		if err := existe(conn, "carteiras", p.CartID); err != nil {
			return nil, false, err
		}
		carteira = p.CartID
	}

	status, quitadoEm := "pendente", any(nil)
	if p.Quitado {
		status, quitadoEm = "quitado", p.Venc
	}

	n := p.Repetir
	if p.Parcelas > 1 {
		n = p.Parcelas
	}
	criados := make([]LancamentoCriado, 0, n)
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
		res, err := conn.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, carteira_id)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			p.Tipo, descItem, valorItem, strings.ToLower(p.Cat), vencItem, status, quitadoEm, conta, carteira,
		)
		if err != nil {
			return criados, categoriaNova, err
		}
		id, _ := res.LastInsertId()
		criados = append(criados, LancamentoCriado{id, descItem, valorItem, vencItem})
	}
	return criados, categoriaNova, nil
}

func lancamentoAdd(conn *sql.DB, tipo string, args []string) error {
	fs := flag.NewFlagSet(tipo+" add", flag.ContinueOnError)
	desc := fs.String("desc", "", "descrição (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório, ex.: 1.200,00)")
	venc := fs.String("venc", "hoje", "vencimento (AAAA-MM-DD ou DD/MM/AAAA)")
	cat := fs.String("cat", "geral", "categoria (ex.: moradia, mercado, salario)")
	contaID := fs.Int64("conta", 0, "id da conta vinculada")
	cartID := fs.Int64("carteira", 0, "id da carteira vinculada")
	repetir := fs.Int("repetir", 1, "repete o lançamento por N meses consecutivos")
	parcelas := fs.Int("parcelas", 1, "divide o valor TOTAL em N parcelas mensais")
	quitado := fs.Bool("quitado", false, "já marca como quitado (pago/recebido)")
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
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	data, err := parseData(*venc)
	if err != nil {
		return err
	}
	criados, categoriaNova, err := CriarLancamentos(conn, LancamentoParams{
		Tipo: tipo, Desc: *desc, Valor: centavos, Cat: *cat, Venc: data,
		ContaID: *contaID, CartID: *cartID, Repetir: *repetir, Parcelas: *parcelas, Quitado: *quitado,
	})
	if err != nil {
		return err
	}
	rotulo := map[string]string{"pagar": "a pagar", "receber": "a receber"}[tipo]
	for _, c := range criados {
		fmt.Printf("Lançamento #%d (%s) %q de %s com vencimento em %s criado.\n",
			c.ID, rotulo, c.Desc, money.Format(c.Valor), dataBR(c.Venc))
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
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := `SELECT id, tipo, descricao, valor, categoria, vencimento, status, quitado_em FROM lancamentos WHERE 1=1`
	var params []any
	if *pendentes {
		query += ` AND status = 'pendente'`
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
	query += ` ORDER BY vencimento, id`

	rows, err := conn.Query(query, params...)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVENCIMENTO\tVALOR\tSTATUS")
	achou := false
	var totPagar, totReceber int64
	for rows.Next() {
		achou = true
		var id, valor int64
		var t, desc, categoria, venc, status string
		var quitadoEm sql.NullString
		if err := rows.Scan(&id, &t, &desc, &valor, &categoria, &venc, &status, &quitadoEm); err != nil {
			return err
		}
		st := "pendente"
		if status == "quitado" {
			st = "quitado em " + dataBR(quitadoEm.String)
		}
		if status == "pendente" {
			if t == "pagar" {
				totPagar += valor
			} else {
				totReceber += valor
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", id, t, desc, categoria, dataBR(venc), money.Format(valor), st)
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
// `prisma lancamentos editar <id> [--desc] [--valor] [--venc] [--cat] [--conta N] [--carteira N]`.
// Use --conta 0 (ou --carteira 0) para remover o vínculo.
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
	res, err := conn.Exec(`DELETE FROM lancamentos WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lançamento #%s não encontrado", args[0])
	}
	fmt.Printf("Lançamento #%s removido.\n", args[0])
	return nil
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
