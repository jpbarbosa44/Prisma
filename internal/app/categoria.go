package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"
)

// Uma categoria classifica lançamentos (mercado, moradia, salário...). No
// lançamento ela continua sendo texto livre; esta tabela é só um catálogo para
// a TUI sugerir/navegar e para o usuário gerenciar a lista. Categorias novas
// usadas num lançamento entram aqui automaticamente (registraCategoria).

// Categoria trata `prisma categoria add|listar|editar|remover`.
func Categoria(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return categoriaAdd(conn, args[1:])
	case "listar", "ls":
		return categoriaListar(conn)
	case "editar":
		return categoriaEditar(conn, args[1:])
	case "remover", "rm":
		return categoriaRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

// registraCategoria insere a categoria no catálogo se ainda não existir. Ignora
// vazio e "geral" (padrão). Erros não são fatais para quem chama.
func registraCategoria(conn *sql.DB, cat string) {
	c := strings.ToLower(strings.TrimSpace(cat))
	if c == "" || c == "geral" {
		return
	}
	conn.Exec(`INSERT OR IGNORE INTO categorias (nome) VALUES (?)`, c)
}

// ListaCategorias devolve os nomes do catálogo em ordem alfabética (para os
// seletores/sugestões da TUI).
func ListaCategorias(conn *sql.DB) []string {
	rows, err := conn.Query(`SELECT nome FROM categorias ORDER BY nome`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			continue
		}
		out = append(out, nome)
	}
	return out
}

func categoriaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("categoria add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome da categoria (obrigatório)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c := strings.ToLower(strings.TrimSpace(*nome))
	if c == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	res, err := conn.Exec(`INSERT OR IGNORE INTO categorias (nome) VALUES (?)`, c)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("a categoria %q já existe", c)
	}
	fmt.Printf("Categoria %q criada.\n", c)
	return nil
}

// categoriaEditar renomeia a categoria e atualiza os lançamentos/recorrências
// que a usam, para a classificação não se perder.
func categoriaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma categoria editar <id> --nome <novo>")
	}
	id := args[0]
	fs := flag.NewFlagSet("categoria editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	novo := strings.ToLower(strings.TrimSpace(*nome))
	if novo == "" {
		return fmt.Errorf("--nome é obrigatório")
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var antigo string
	if err := tx.QueryRow(`SELECT nome FROM categorias WHERE id = ?`, id).Scan(&antigo); err == sql.ErrNoRows {
		return fmt.Errorf("categoria #%s não encontrada", id)
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE categorias SET nome = ? WHERE id = ?`, novo, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE lancamentos SET categoria = ? WHERE categoria = ?`, novo, antigo); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE recorrencias SET categoria = ? WHERE categoria = ?`, novo, antigo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Categoria #%s renomeada de %q para %q.\n", id, antigo, novo)
	return nil
}

func categoriaListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT c.id, c.nome,
		       (SELECT COUNT(*) FROM lancamentos l WHERE l.categoria = c.nome)
		FROM categorias c ORDER BY c.nome`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type linha struct {
		id   int64
		nome string
		usos int
	}
	var cats []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nome, &l.usos); err != nil {
			return err
		}
		cats = append(cats, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cats) == 0 {
		fmt.Println("Nenhuma categoria cadastrada. Use: prisma categoria add --nome mercado")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tLANÇAMENTOS")
	for _, l := range cats {
		fmt.Fprintf(w, "%d\t%s\t%d\n", l.id, l.nome, l.usos)
	}
	return w.Flush()
}

// categoriaRemover apaga só o catálogo; os lançamentos com essa categoria
// continuam intactos (categoria é texto livre).
func categoriaRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma categoria remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM categorias WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("categoria #%s não encontrada", args[0])
	}
	fmt.Printf("Categoria #%s removida do catálogo (os lançamentos foram mantidos).\n", args[0])
	return nil
}
