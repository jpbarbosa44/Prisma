package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Regras de categoria: "uma descrição com IFOOD é delivery". Valem quando o
// lançamento chega sem categoria — cadastro sem --cat, bot sem #categoria e
// importação de extrato sem categoria escolhida. O padrão casa por palavra
// inteira, sem diferenciar maiúsculas nem acentos ("UBER *TRIP" casa "uber";
// "uber eats" casa as duas palavras seguidas). Mais de uma casou: vence a de
// padrão mais longo (a mais específica).

// tiraAcento troca as letras acentuadas do português pela base.
var tiraAcento = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "î", "i", "ì", "i", "ï", "i",
	"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "û", "u", "ù", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// palavrasRegra quebra um texto nas palavras comparáveis: minúsculas, sem
// acento, só letras e dígitos.
func palavrasRegra(s string) []string {
	s = tiraAcento.Replace(strings.ToLower(s))
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// casaPadrao diz se as palavras do padrão aparecem seguidas na descrição.
func casaPadrao(desc, padrao []string) bool {
	if len(padrao) == 0 || len(padrao) > len(desc) {
		return false
	}
	for i := 0; i+len(padrao) <= len(desc); i++ {
		igual := true
		for j, p := range padrao {
			if desc[i+j] != p {
				igual = false
				break
			}
		}
		if igual {
			return true
		}
	}
	return false
}

// RegraCategoria é uma regra cadastrada.
type RegraCategoria struct {
	ID        int64
	Padrao    string
	Categoria string
}

// listaRegras devolve as regras, das mais específicas (padrão mais longo) às
// mais genéricas — a ordem em que são testadas.
func listaRegras(q dbtx) ([]RegraCategoria, error) {
	rows, err := q.Query(`SELECT id, padrao, categoria FROM regras_categoria ORDER BY length(padrao) DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegraCategoria
	for rows.Next() {
		var r RegraCategoria
		if err := rows.Scan(&r.ID, &r.Padrao, &r.Categoria); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CategoriaPorRegra devolve a categoria da regra que casa com a descrição
// ("" se nenhuma casar).
func CategoriaPorRegra(q dbtx, desc string) string {
	regras, err := listaRegras(q)
	if err != nil || len(regras) == 0 {
		return ""
	}
	return categoriaEntre(regras, desc)
}

func categoriaEntre(regras []RegraCategoria, desc string) string {
	d := palavrasRegra(desc)
	for _, r := range regras {
		if casaPadrao(d, palavrasRegra(r.Padrao)) {
			return r.Categoria
		}
	}
	return ""
}

// Regra trata `prisma regra add|listar|remover|testar`.
func Regra(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		if len(args) != 3 {
			return errors.New(`uso: prisma regra add "<palavra(s) da descrição>" <categoria>`)
		}
		return regraAdd(conn, args[1], args[2])
	case "listar", "ls":
		return regraListar(conn)
	case "remover", "rm":
		if len(args) != 2 {
			return errors.New("uso: prisma regra remover <id>")
		}
		return regraRemover(conn, args[1])
	case "testar":
		if len(args) < 2 {
			return errors.New(`uso: prisma regra testar "<descrição>"`)
		}
		desc := strings.Join(args[1:], " ")
		if c := CategoriaPorRegra(conn, desc); c != "" {
			fmt.Printf("%q → %s\n", desc, c)
		} else {
			fmt.Printf("%q: nenhuma regra casa (fica sem categoria: geral, ou importado na importação)\n", desc)
		}
		return nil
	}
	return fmt.Errorf("subcomando inválido %q (use: add, listar, remover, testar)", args[0])
}

func regraAdd(conn *sql.DB, padrao, cat string) error {
	padrao = strings.Join(strings.Fields(padrao), " ")
	cat = strings.ToLower(strings.TrimSpace(cat))
	if len(palavrasRegra(padrao)) == 0 {
		return errors.New("o padrão precisa de ao menos uma palavra (ex.: ifood)")
	}
	if cat == "" {
		return errors.New("informe a categoria")
	}
	// mesmo padrão (ignorando caixa e acento) já cadastrado: atualiza a categoria
	regras, err := listaRegras(conn)
	if err != nil {
		return err
	}
	chave := strings.Join(palavrasRegra(padrao), " ")
	for _, r := range regras {
		if strings.Join(palavrasRegra(r.Padrao), " ") == chave {
			if _, err := conn.Exec(`UPDATE regras_categoria SET categoria = ? WHERE id = ?`, cat, r.ID); err != nil {
				return err
			}
			registraCategoria(conn, cat)
			fmt.Printf("Regra #%d atualizada: %q → %s\n", r.ID, r.Padrao, cat)
			return nil
		}
	}
	res, err := conn.Exec(`INSERT INTO regras_categoria (padrao, categoria) VALUES (?, ?)`, padrao, cat)
	if err != nil {
		return err
	}
	registraCategoria(conn, cat)
	id, _ := res.LastInsertId()
	fmt.Printf("Regra #%d criada: descrições com %q → %s\n", id, padrao, cat)
	return nil
}

func regraListar(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT id, padrao, categoria FROM regras_categoria ORDER BY categoria, padrao`)
	if err != nil {
		return err
	}
	defer rows.Close()
	w := novaTabela()
	fmt.Fprintln(w, "REGRA\tDESCRIÇÃO COM\tCATEGORIA")
	n := 0
	for rows.Next() {
		var r RegraCategoria
		if err := rows.Scan(&r.ID, &r.Padrao, &r.Categoria); err != nil {
			return err
		}
		// "r3", não "3": na TUI, linhas que começam com número são de categoria
		fmt.Fprintf(w, "r%d\t%s\t%s\n", r.ID, r.Padrao, r.Categoria)
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n == 0 {
		fmt.Println(`Nenhuma regra de categoria. Crie com: prisma regra add ifood delivery`)
		return nil
	}
	return w.Flush()
}

func regraRemover(conn *sql.DB, id string) error {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "r"), 10, 64)
	if err != nil {
		return fmt.Errorf("id de regra inválido: %q", id)
	}
	res, err := conn.Exec(`DELETE FROM regras_categoria WHERE id = ?`, n)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return fmt.Errorf("regra #%d não encontrada", n)
	}
	fmt.Printf("Regra #%d removida.\n", n)
	return nil
}

// palavraChave sugere o padrão de uma regra a partir de uma descrição: a
// primeira palavra com letras (ex.: "IFOOD *Restaurante 123" → "ifood").
func palavraChave(desc string) string {
	for _, p := range palavrasRegra(normalizaDescricao(desc)) {
		if strings.IndexFunc(p, unicode.IsLetter) >= 0 && len(p) >= 3 {
			return p
		}
	}
	return ""
}

// sugereRegra, depois de trocar a categoria de um lançamento à mão, ensina a
// criar a regra que faria isso sozinho nas próximas vezes (se ainda não há).
func sugereRegra(conn *sql.DB, id, cat string) {
	var desc string
	if conn.QueryRow(`SELECT descricao FROM lancamentos WHERE id = ?`, id).Scan(&desc) != nil {
		return
	}
	chave := palavraChave(desc)
	if chave == "" || cat == "" || cat == "geral" || CategoriaPorRegra(conn, desc) == cat {
		return
	}
	fmt.Printf("Dica: para as próximas descrições com %q irem sozinhas para %s: prisma regra add %s %s (ou r em Categorias)\n",
		chave, cat, chave, cat)
}

// categoriaImportada é a categoria de um movimento de extrato: a escolhida
// (--cat), senão a da regra que casar, senão "importado".
func categoriaImportada(regras []RegraCategoria, desc, escolhida string) string {
	if c := strings.ToLower(strings.TrimSpace(escolhida)); c != "" {
		return c
	}
	if c := categoriaEntre(regras, desc); c != "" {
		return c
	}
	return "importado"
}
