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

// categoriaEditar renomeia a categoria (atualizando os lançamentos/recorrências
// que a usam, para a classificação não se perder) e/ou define a classe que o
// Analytics usa (necessidade, desejo, utilidade, basica; auto volta à regra por
// palavra-chave).
func categoriaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma categoria editar <id> [--nome <novo>] [--classe necessidade|desejo|utilidade|basica|auto]")
	}
	id := args[0]
	fs := flag.NewFlagSet("categoria editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	classe := fs.String("classe", "", "classe no Analytics: necessidade, desejo, utilidade, basica ou auto")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	novo := strings.ToLower(strings.TrimSpace(*nome))
	if novo == "" && *classe == "" {
		return fmt.Errorf("informe --nome e/ou --classe")
	}
	if *classe != "" {
		k, err := normalizaClasse(*classe)
		if err != nil {
			return err
		}
		res, err := conn.Exec(`UPDATE categorias SET classe = ? WHERE id = ?`, k, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("categoria #%s não encontrada", id)
		}
		if k == "" {
			fmt.Printf("Categoria #%s volta à classificação automática.\n", id)
		} else {
			fmt.Printf("Categoria #%s classificada como %s.\n", id, k)
		}
		if novo == "" {
			return nil
		}
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

// normalizaClasse valida a classe de --classe; "auto" (ou "automatica") vira
// vazio, a classificação por palavra-chave.
func normalizaClasse(s string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(s))
	k = strings.NewReplacer("á", "a", "â", "a", "ê", "e").Replace(k)
	if k == "auto" || k == "automatica" {
		return "", nil
	}
	for _, c := range ClassesCategoria {
		if k == c {
			return k, nil
		}
	}
	return "", fmt.Errorf("classe inválida %q (use: %s ou auto)", s, strings.Join(ClassesCategoria, ", "))
}

func categoriaListar(conn *sql.DB) error {
	cl, err := carregaClassificador(conn)
	if err != nil {
		return err
	}
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
	fmt.Fprintln(w, "ID\tNOME\tLANÇAMENTOS\tCLASSE")
	for _, l := range cats {
		classe, configurada, _ := cl.efetiva(l.nome)
		if !configurada {
			classe = "auto (" + classe + ")"
		}
		fmt.Fprintf(w, "%d\t%s\t%d\t%s\n", l.id, l.nome, l.usos, classe)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("\nA classe orienta o Analytics (50/30/20, Eficiência, Inflação). Ajuste com:")
	fmt.Println("  prisma categoria editar <id> --classe necessidade|desejo|utilidade|basica|auto")
	return nil
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
