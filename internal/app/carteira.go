package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"prisma/internal/money"
)

// Carteira trata os subcomandos `prisma carteira add|listar|remover`.
func Carteira(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return carteiraAdd(conn, args[1:])
	case "listar", "ls":
		return carteiraListar(conn)
	case "editar":
		return carteiraEditar(conn, args[1:])
	case "remover", "rm":
		return carteiraRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

// carteiraEditar altera só os campos informados:
// `prisma carteira editar <id> [--nome] [--desc] [--saldo]`.
// --saldo redefine o saldo INICIAL (o saldo atual é recalculado).
func carteiraEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma carteira editar <id> [--nome] [--desc] [--saldo]")
	}
	id := args[0]
	fs := flag.NewFlagSet("carteira editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	desc := fs.String("desc", "", "nova descrição")
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
	if informado["desc"] {
		sets, params = append(sets, "descricao = ?"), append(params, *desc)
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
	res, err := conn.Exec(`UPDATE carteiras SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("carteira #%s não encontrada", id)
	}
	fmt.Printf("Carteira #%s atualizada.\n", id)
	return nil
}

func carteiraAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("carteira add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome da carteira (obrigatório)")
	desc := fs.String("desc", "", "descrição")
	saldo := fs.String("saldo", "0", "saldo inicial (ex.: 250,00)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nome == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	centavos, err := money.Parse(*saldo)
	if err != nil {
		return err
	}
	res, err := conn.Exec(
		`INSERT INTO carteiras (nome, descricao, saldo_inicial) VALUES (?,?,?)`,
		*nome, *desc, centavos,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Carteira #%d %q criada com saldo inicial %s.\n", id, *nome, money.Format(centavos))
	return nil
}

func saldoCarteira(conn *sql.DB, id int64) (int64, error) {
	return saldoLocal(conn, "carteira", id)
}

func carteiraListar(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT id, nome, descricao FROM carteiras ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tDESCRIÇÃO\tSALDO")
	achou := false
	for rows.Next() {
		achou = true
		var id int64
		var nome, desc string
		if err := rows.Scan(&id, &nome, &desc); err != nil {
			return err
		}
		saldo, err := saldoCarteira(conn, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", id, nome, ouTraco(desc), money.Format(saldo))
	}
	if !achou {
		fmt.Println("Nenhuma carteira cadastrada. Use: prisma carteira add --nome \"Dinheiro\"")
		return nil
	}
	return w.Flush()
}

func carteiraRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma carteira remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM carteiras WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("carteira #%s não encontrada", args[0])
	}
	fmt.Printf("Carteira #%s removida (lançamentos vinculados foram mantidos, sem vínculo).\n", args[0])
	return nil
}
