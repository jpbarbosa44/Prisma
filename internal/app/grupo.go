package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"prisma/internal/money"
)

// Grupo trata os subcomandos `prisma grupo add|listar|editar|remover`.
//
// Um grupo reúne pessoas (ex.: "eu e minha namorada"). Despesas vinculadas a um
// grupo passam a contar, em todo o sistema, apenas pela parte que cabe a você —
// o valor cheio dividido pelo número de pessoas do grupo.
func Grupo(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return grupoAdd(conn, args[1:])
	case "listar", "ls":
		return grupoListar(conn)
	case "editar":
		return grupoEditar(conn, args[1:])
	case "remover", "rm":
		return grupoRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

// parsePessoas separa "Ana, Bia , Carla" em ["Ana","Bia","Carla"], sem vazios.
func parsePessoas(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func grupoAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("grupo add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome do grupo (obrigatório)")
	pessoas := fs.String("pessoas", "", "pessoas separadas por vírgula (ex.: \"Eu, Maria\")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nome == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	nomes := parsePessoas(*pessoas)
	if len(nomes) < 2 {
		return fmt.Errorf("informe ao menos 2 pessoas em --pessoas (ex.: --pessoas \"Eu, Maria\")")
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO grupos (nome) VALUES (?)`, *nome)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	for _, p := range nomes {
		if _, err := tx.Exec(`INSERT INTO grupo_pessoas (grupo_id, nome) VALUES (?,?)`, id, p); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Grupo #%d %q criado com %d pessoas (%s).\n",
		id, *nome, len(nomes), strings.Join(nomes, ", "))
	return nil
}

// grupoEditar altera o nome e/ou a lista de pessoas:
// `prisma grupo editar <id> [--nome] [--pessoas]`. --pessoas substitui a lista
// inteira.
func grupoEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma grupo editar <id> [--nome] [--pessoas]")
	}
	id := args[0]
	fs := flag.NewFlagSet("grupo editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	pessoas := fs.String("pessoas", "", "nova lista de pessoas, separadas por vírgula")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	if !informado["nome"] && !informado["pessoas"] {
		return fmt.Errorf("nada para alterar: informe --nome e/ou --pessoas")
	}

	var nomes []string
	if informado["pessoas"] {
		nomes = parsePessoas(*pessoas)
		if len(nomes) < 2 {
			return fmt.Errorf("informe ao menos 2 pessoas em --pessoas")
		}
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if informado["nome"] {
		if *nome == "" {
			return fmt.Errorf("o nome não pode ficar vazio")
		}
		res, err := tx.Exec(`UPDATE grupos SET nome = ? WHERE id = ?`, *nome, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("grupo #%s não encontrado", id)
		}
	} else {
		// garante que o grupo existe antes de mexer nas pessoas
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM grupos WHERE id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("grupo #%s não encontrado", id)
		}
	}
	if informado["pessoas"] {
		if _, err := tx.Exec(`DELETE FROM grupo_pessoas WHERE grupo_id = ?`, id); err != nil {
			return err
		}
		for _, p := range nomes {
			if _, err := tx.Exec(`INSERT INTO grupo_pessoas (grupo_id, nome) VALUES (?,?)`, id, p); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Grupo #%s atualizado.\n", id)
	return nil
}

func grupoListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT g.id, g.nome,
		       COALESCE(GROUP_CONCAT(gp.nome, ', '), ''),
		       COUNT(gp.id)
		FROM grupos g
		LEFT JOIN grupo_pessoas gp ON gp.grupo_id = g.id
		GROUP BY g.id, g.nome
		ORDER BY g.id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type linha struct {
		id      int64
		nome    string
		pessoas string
		qtd     int
	}
	var grupos []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nome, &l.pessoas, &l.qtd); err != nil {
			return err
		}
		grupos = append(grupos, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(grupos) == 0 {
		fmt.Println("Nenhum grupo cadastrado. Use: prisma grupo add --nome \"Casa\" --pessoas \"Eu, Maria\"")
		return nil
	}

	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tPESSOAS\tDIVIDE POR\tDESPESAS VINCULADAS")
	for _, l := range grupos {
		// total cheio das despesas vinculadas e a parte que cabe a você
		var cheio, minha int64
		err := conn.QueryRow(`
			SELECT COALESCE(SUM(valor), 0), COALESCE(SUM(`+valEf("lancamentos")+`), 0)
			FROM lancamentos WHERE grupo_id = ? AND tipo = 'pagar'`, l.id).Scan(&cheio, &minha)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s (sua parte: %s)\n",
			l.id, l.nome, l.pessoas, l.qtd, money.Format(cheio), money.Format(minha))
	}
	return w.Flush()
}

func grupoRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma grupo remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM grupos WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("grupo #%s não encontrado", args[0])
	}
	fmt.Printf("Grupo #%s removido (as despesas vinculadas voltam a contar pelo valor cheio).\n", args[0])
	return nil
}
