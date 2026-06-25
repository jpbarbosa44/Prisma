package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prisma/internal/money"
)

// Cartão de crédito: você gasta agora e paga depois, na fatura. Um gasto de
// cartão é um lançamento `pagar` pendente cujo VENCIMENTO é a data da fatura
// (calculada do ciclo do cartão), não a data da compra — por isso ele não mexe
// no saldo do banco até a fatura ser paga. A fatura é identificada pelo mês do
// seu vencimento (substr(vencimento,1,7)); pagá-la quita em bloco os gastos do
// ciclo, debitando a conta vinculada ao cartão. A divisão por grupo (valEf)
// continua valendo: a fatura reflete a SUA parte.

// faturaDe calcula, para uma compra em `compra`, a referência (AAAA-MM) e a
// data de vencimento (AAAA-MM-DD) da fatura em que ela entra, dado o dia de
// fechamento e o de vencimento do cartão. Compras após o fechamento caem na
// fatura seguinte; se o vencimento é igual/antes do fechamento, vence no mês
// seguinte ao do fechamento.
func faturaDe(fech, venc int, compra time.Time) (ref, vencimento string) {
	ano, mes := compra.Year(), compra.Month()
	if compra.Day() > fech {
		t := time.Date(ano, mes, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		ano, mes = t.Year(), t.Month()
	}
	vAno, vMes := ano, mes
	if venc <= fech {
		t := time.Date(vAno, vMes, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		vAno, vMes = t.Year(), t.Month()
	}
	d := clampDia(vAno, vMes, venc)
	return d.Format("2006-01"), d.Format("2006-01-02")
}

// VencimentoFatura devolve, para uma compra HOJE no cartão informado, a data de
// vencimento da fatura em que ela cairia, em DD/MM/AAAA (vazio se o cartão não
// existe). Serve para o formulário sugerir o vencimento ao escolher um cartão.
func VencimentoFatura(conn *sql.DB, cartaoID string) string {
	id, err := strconv.ParseInt(strings.TrimSpace(cartaoID), 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	var fech, venc int
	if err := conn.QueryRow(`SELECT dia_fechamento, dia_vencimento FROM cartoes WHERE id = ?`, id).
		Scan(&fech, &venc); err != nil {
		return ""
	}
	_, vencFat := faturaDe(fech, venc, time.Now())
	return dataBR(vencFat)
}

// clampDia monta uma data prendendo o dia ao último do mês (ex.: dia 31 em
// fevereiro vira 28/29).
func clampDia(ano int, mes time.Month, dia int) time.Time {
	ultimo := time.Date(ano, mes+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if dia > ultimo {
		dia = ultimo
	}
	return time.Date(ano, mes, dia, 0, 0, 0, 0, time.UTC)
}

// Cartao trata `prisma cartao add|listar|editar|remover`.
func Cartao(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return cartaoAdd(conn, args[1:])
	case "listar", "ls":
		return cartaoListar(conn)
	case "editar":
		return cartaoEditar(conn, args[1:])
	case "remover", "rm":
		return cartaoRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

func cartaoAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("cartao add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome do cartão (obrigatório)")
	limite := fs.String("limite", "0", "limite total (ex.: 5.000,00)")
	fech := fs.Int("fechamento", 0, "dia do fechamento da fatura (1 a 31)")
	venc := fs.Int("vencimento", 0, "dia do vencimento da fatura (1 a 31)")
	contaID := fs.Int64("conta", 0, "conta que paga a fatura")
	faturaAtual := fs.String("fatura-atual", "0", "valor da fatura em aberto hoje (evita relançar o passado)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nome == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	if *fech < 1 || *fech > 31 || *venc < 1 || *venc > 31 {
		return fmt.Errorf("--fechamento e --vencimento são obrigatórios (dias de 1 a 31)")
	}
	lim, err := money.Parse(*limite)
	if err != nil {
		return err
	}
	fat, err := money.Parse(*faturaAtual)
	if err != nil {
		return err
	}
	var conta any
	if *contaID != 0 {
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	}
	res, err := conn.Exec(
		`INSERT INTO cartoes (nome, limite, dia_fechamento, dia_vencimento, conta_id) VALUES (?,?,?,?,?)`,
		*nome, lim, *fech, *venc, conta)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Cartão #%d %q criado (fecha dia %d, vence dia %d).\n", id, *nome, *fech, *venc)

	// fatura inicial: um lançamento pendente que já cai na fatura aberta, sem
	// data_compra (não conta como gasto novo nos relatórios, só quando paga)
	if fat > 0 {
		_, vencFat := faturaDe(*fech, *venc, time.Now())
		if _, err := conn.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, conta_id, cartao_id)
			VALUES ('pagar', 'Fatura ao cadastrar', ?, 'cartao', ?, 'pendente', ?, ?)`,
			fat, vencFat, conta, id); err != nil {
			return err
		}
		fmt.Printf("Fatura inicial de %s lançada com vencimento em %s.\n", money.Format(fat), dataBR(vencFat))
	}
	return nil
}

func cartaoEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma cartao editar <id> [--nome] [--limite] [--fechamento] [--vencimento] [--conta]")
	}
	id := args[0]
	fs := flag.NewFlagSet("cartao editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	limite := fs.String("limite", "", "novo limite")
	fech := fs.Int("fechamento", 0, "novo dia de fechamento")
	venc := fs.Int("vencimento", 0, "novo dia de vencimento")
	contaID := fs.Int64("conta", -1, "conta que paga (0 desvincula)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })

	var sets []string
	var params []any
	if informado["nome"] {
		if *nome == "" {
			return fmt.Errorf("o nome não pode ficar vazio")
		}
		sets, params = append(sets, "nome = ?"), append(params, *nome)
	}
	if informado["limite"] {
		c, err := money.Parse(*limite)
		if err != nil {
			return err
		}
		sets, params = append(sets, "limite = ?"), append(params, c)
	}
	if informado["fechamento"] {
		if *fech < 1 || *fech > 31 {
			return fmt.Errorf("--fechamento deve ser de 1 a 31")
		}
		sets, params = append(sets, "dia_fechamento = ?"), append(params, *fech)
	}
	if informado["vencimento"] {
		if *venc < 1 || *venc > 31 {
			return fmt.Errorf("--vencimento deve ser de 1 a 31")
		}
		sets, params = append(sets, "dia_vencimento = ?"), append(params, *venc)
	}
	if informado["conta"] {
		if *contaID > 0 {
			if err := existe(conn, "contas", *contaID); err != nil {
				return err
			}
			sets, params = append(sets, "conta_id = ?"), append(params, *contaID)
		} else {
			sets = append(sets, "conta_id = NULL")
		}
	}
	if len(sets) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}
	params = append(params, id)
	res, err := conn.Exec(`UPDATE cartoes SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("cartão #%s não encontrado", id)
	}
	fmt.Printf("Cartão #%s atualizado.\n", id)
	return nil
}

func cartaoListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT c.id, c.nome, c.limite, c.dia_fechamento, c.dia_vencimento, COALESCE(ct.nome, '')
		FROM cartoes c LEFT JOIN contas ct ON ct.id = c.conta_id
		ORDER BY c.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type linha struct {
		id          int64
		nome, conta string
		limite      int64
		fech, venc  int
	}
	var cartoes []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nome, &l.limite, &l.fech, &l.venc, &l.conta); err != nil {
			return err
		}
		cartoes = append(cartoes, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cartoes) == 0 {
		fmt.Println("Nenhum cartão cadastrado. Use: prisma cartao add --nome \"Nubank\" --fechamento 20 --vencimento 27 --conta 1")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tFECHA\tVENCE\tCONTA\tLIMITE\tFATURA ABERTA")
	for _, l := range cartoes {
		_, vencFat := faturaDe(l.fech, l.venc, time.Now())
		ref := vencFat[:7]
		var aberta int64
		if err := conn.QueryRow(`
			SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
			WHERE cartao_id = ? AND status = 'pendente' AND substr(vencimento,1,7) = ?`,
			l.id, ref).Scan(&aberta); err != nil {
			return err
		}
		conta := ouTraco(l.conta)
		lim := "-"
		if l.limite > 0 {
			lim = money.Format(l.limite)
		}
		fmt.Fprintf(w, "%d\t%s\tdia %d\tdia %d\t%s\t%s\t%s (vence %s)\n",
			l.id, l.nome, l.fech, l.venc, conta, lim, money.Format(aberta), dataBR(vencFat))
	}
	return w.Flush()
}

func cartaoRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma cartao remover <id>")
	}
	id := args[0]
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// só as faturas em aberto (pendentes) somem com o cartão; as já pagas ficam
	// como histórico — apagá-las reescreveria o saldo (saldoTotal soma os
	// quitados). O FK ON DELETE SET NULL desvincula essas pagas do cartão.
	resL, err := tx.Exec(`DELETE FROM lancamentos WHERE cartao_id = ? AND status = 'pendente'`, id)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM cartoes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("cartão #%s não encontrado", id)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	nl, _ := resL.RowsAffected()
	if nl > 0 {
		fmt.Printf("Cartão #%s removido com %d lançamento(s) em aberto; as faturas já pagas viraram histórico.\n", id, nl)
	} else {
		fmt.Printf("Cartão #%s removido.\n", id)
	}
	return nil
}

// Fatura trata `prisma fatura --cartao N [--ref AAAA-MM]` (ver) e
// `prisma fatura pagar --cartao N [--ref] [--data] [--conta]` (pagar).
func Fatura(conn *sql.DB, args []string) error {
	if len(args) > 0 && args[0] == "pagar" {
		return faturaPagar(conn, args[1:])
	}
	return faturaVer(conn, args)
}

// dadosCartao busca os dias do ciclo e a conta pagadora de um cartão.
func dadosCartao(conn *sql.DB, id int64) (nome string, fech, venc int, contaID sql.NullInt64, err error) {
	err = conn.QueryRow(`SELECT nome, dia_fechamento, dia_vencimento, conta_id FROM cartoes WHERE id = ?`, id).
		Scan(&nome, &fech, &venc, &contaID)
	if err == sql.ErrNoRows {
		err = fmt.Errorf("cartão #%d não encontrado", id)
	}
	return
}

func faturaVer(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("fatura", flag.ContinueOnError)
	cartaoID := fs.Int64("cartao", 0, "id do cartão")
	ref := fs.String("ref", "", "fatura (AAAA-MM); padrão: a aberta")
	abertos := fs.Bool("abertos", false, "mostra só os lançamentos ainda em aberto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cartaoID == 0 {
		return fmt.Errorf("informe --cartao N (veja em: prisma cartao listar)")
	}
	nome, fech, venc, _, err := dadosCartao(conn, *cartaoID)
	if err != nil {
		return err
	}
	if *ref == "" {
		*ref, _ = faturaDe(fech, venc, time.Now()) // a fatura aberta
	}

	filtro := ""
	if *abertos {
		filtro = " AND status = 'pendente'"
	}
	rows, err := conn.Query(`
		SELECT id, descricao, COALESCE(data_compra, ''), `+valEf("lancamentos")+`, vencimento, status
		FROM lancamentos
		WHERE cartao_id = ? AND substr(vencimento,1,7) = ?`+filtro+`
		ORDER BY COALESCE(data_compra, vencimento), id`, *cartaoID, *ref)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tCOMPRA\tDESCRIÇÃO\tVALOR\tSTATUS")
	var total, aberto int64
	var vencFat string
	achou, todosQuitados := false, true
	for rows.Next() {
		achou = true
		var id, valor int64
		var desc, compra, vencimento, status string
		if err := rows.Scan(&id, &desc, &compra, &valor, &vencimento, &status); err != nil {
			return err
		}
		vencFat = vencimento
		total += valor
		quando := "-"
		if compra != "" {
			quando = dataBR(compra)
		}
		st := "pendente"
		if status == "quitado" {
			st = "paga"
		} else {
			aberto += valor
			todosQuitados = false
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", id, quando, desc, money.Format(valor), st)
	}
	if !achou {
		fmt.Printf("Fatura %s do cartão %s: nenhum lançamento.\n", *ref, nome)
		return nil
	}
	fmt.Printf("FATURA %s — cartão %s (vence %s)\n\n", *ref, nome, dataBR(vencFat))
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nTotal da fatura: %s\n", money.Format(total))
	if todosQuitados {
		fmt.Println("Status: paga ✓")
	} else {
		fmt.Printf("Em aberto: %s — pague com: prisma fatura pagar --cartao %d --ref %s\n",
			money.Format(aberto), *cartaoID, *ref)
	}
	return nil
}

func faturaPagar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("fatura pagar", flag.ContinueOnError)
	cartaoID := fs.Int64("cartao", 0, "id do cartão")
	ref := fs.String("ref", "", "fatura (AAAA-MM); padrão: a aberta")
	dataPg := fs.String("data", "hoje", "data do pagamento")
	contaID := fs.Int64("conta", 0, "conta que paga (padrão: a do cartão)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cartaoID == 0 {
		return fmt.Errorf("informe --cartao N")
	}
	nome, fech, venc, contaCartao, err := dadosCartao(conn, *cartaoID)
	if err != nil {
		return err
	}
	if *ref == "" {
		*ref, _ = faturaDe(fech, venc, time.Now())
	}
	d, err := parseData(*dataPg)
	if err != nil {
		return err
	}

	// conta que vai debitar: a informada, senão a do cartão
	var conta any
	switch {
	case *contaID != 0:
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	case contaCartao.Valid:
		conta = contaCartao.Int64
	}

	var total int64
	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE cartao_id = ? AND status = 'pendente' AND substr(vencimento,1,7) = ?`,
		*cartaoID, *ref).Scan(&total); err != nil {
		return err
	}
	if total == 0 {
		return fmt.Errorf("fatura %s do cartão %s não tem nada em aberto", *ref, nome)
	}
	res, err := conn.Exec(`
		UPDATE lancamentos SET status = 'quitado', quitado_em = ?, conta_id = COALESCE(?, conta_id)
		WHERE cartao_id = ? AND status = 'pendente' AND substr(vencimento,1,7) = ?`,
		d, conta, *cartaoID, *ref)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	fmt.Printf("Fatura %s do cartão %s paga: %s em %d lançamento(s), em %s.\n",
		*ref, nome, money.Format(total), n, dataBR(d))
	return nil
}
