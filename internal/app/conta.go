package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"prisma/internal/money"
)

// Conta trata os subcomandos `prisma conta add|listar|remover`.
func Conta(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return contaAdd(conn, args[1:])
	case "listar", "ls":
		return contaListar(conn)
	case "editar":
		return contaEditar(conn, args[1:])
	case "remover", "rm":
		return contaRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

// contaEditar altera só os campos informados:
// `prisma conta editar <id> [--nome] [--banco] [--tipo] [--saldo]`.
// --saldo redefine o saldo INICIAL (o saldo atual é recalculado).
func contaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma conta editar <id> [--nome] [--banco] [--tipo] [--saldo]")
	}
	id := args[0]
	fs := flag.NewFlagSet("conta editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	banco := fs.String("banco", "", "nova instituição")
	tipo := fs.String("tipo", "", "corrente, poupanca ou investimento")
	saldo := fs.String("saldo", "", "novo saldo inicial")
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
	if informado["banco"] {
		sets, params = append(sets, "banco = ?"), append(params, *banco)
	}
	if informado["tipo"] {
		if *tipo != "corrente" && *tipo != "poupanca" && *tipo != "investimento" {
			return fmt.Errorf("--tipo deve ser corrente, poupanca ou investimento")
		}
		sets, params = append(sets, "tipo = ?"), append(params, *tipo)
	}
	if informado["saldo"] {
		c, err := money.Parse(*saldo)
		if err != nil {
			return err
		}
		sets, params = append(sets, "saldo_inicial = ?"), append(params, c)
	}
	if len(sets) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}
	params = append(params, id)
	res, err := conn.Exec(`UPDATE contas SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("conta #%s não encontrada", id)
	}
	fmt.Printf("Conta #%s atualizada.\n", id)
	return nil
}

func contaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("conta add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome da conta (obrigatório)")
	banco := fs.String("banco", "", "instituição financeira")
	tipo := fs.String("tipo", "corrente", "corrente, poupanca ou investimento")
	saldo := fs.String("saldo", "0", "saldo inicial (ex.: 1.234,56)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nome == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	if *tipo != "corrente" && *tipo != "poupanca" && *tipo != "investimento" {
		return fmt.Errorf("--tipo deve ser corrente, poupanca ou investimento")
	}
	centavos, err := money.Parse(*saldo)
	if err != nil {
		return err
	}
	res, err := conn.Exec(
		`INSERT INTO contas (nome, banco, tipo, saldo_inicial) VALUES (?,?,?,?)`,
		*nome, *banco, *tipo, centavos,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Conta #%d %q criada com saldo inicial %s.\n", id, *nome, money.Format(centavos))
	return nil
}

// saldoConta = saldo_inicial + lançamentos quitados vinculados ± transferências.
func saldoConta(conn *sql.DB, id int64) (int64, error) {
	return saldoLocal(conn, "conta", id)
}

func saldoLocal(conn *sql.DB, tipo string, id int64) (int64, error) {
	tabela := tipo + "s" // contas | carteiras (valores controlados internamente)
	var s int64
	err := conn.QueryRow(`
		SELECT c.saldo_inicial + COALESCE((
			SELECT SUM(CASE l.tipo WHEN 'receber' THEN `+valEf("l")+` ELSE -`+valEf("l")+` END)
			FROM lancamentos l WHERE l.`+tipo+`_id = c.id AND l.status = 'quitado'
		), 0) + COALESCE((
			SELECT SUM(t.valor) FROM transferencias t
			WHERE t.destino_tipo = ? AND t.destino_id = c.id
		), 0) - COALESCE((
			SELECT SUM(t.valor) FROM transferencias t
			WHERE t.origem_tipo = ? AND t.origem_id = c.id
		), 0)
		FROM `+tabela+` c WHERE c.id = ?`, tipo, tipo, id).Scan(&s)
	return s, err
}

func contaListar(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT id, nome, banco, tipo FROM contas ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tBANCO\tTIPO\tSALDO")
	achou := false
	for rows.Next() {
		achou = true
		var id int64
		var nome, banco, tipo string
		if err := rows.Scan(&id, &nome, &banco, &tipo); err != nil {
			return err
		}
		saldo, err := saldoConta(conn, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", id, nome, banco, tipo, money.Format(saldo))
	}
	if !achou {
		fmt.Println("Nenhuma conta cadastrada. Use: prisma conta add --nome \"Minha Conta\"")
		return nil
	}
	return w.Flush()
}

func contaRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma conta remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM contas WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("conta #%s não encontrada", args[0])
	}
	fmt.Printf("Conta #%s removida (lançamentos vinculados foram mantidos, sem vínculo).\n", args[0])
	return nil
}
