package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"prisma/internal/money"
)

// Tags são as #palavras da descrição ("Hotel #viagem-2026"): etiquetas livres,
// que cruzam categorias, sem campo novo — funcionam no cadastro, no bot, na
// importação e no boleto. Começam com letra; minúsculas na contagem.

var reTag = regexp.MustCompile(`#(\p{L}[\p{L}\p{N}_\-/]*)`)

// TagsDe devolve as tags de uma descrição, em minúsculas e sem repetir.
func TagsDe(desc string) []string {
	var out []string
	visto := map[string]bool{}
	for _, m := range reTag.FindAllStringSubmatch(desc, -1) {
		t := strings.ToLower(strings.TrimRight(m[1], "-/_"))
		if t != "" && !visto[t] {
			visto[t] = true
			out = append(out, t)
		}
	}
	return out
}

// ResumoTag é o total de uma tag.
type ResumoTag struct {
	Tag      string
	Qtd      int
	Despesas int64
	Receitas int64
	De, Ate  string // primeiro e último vencimento
}

// ResumoTags soma os lançamentos de cada tag.
func ResumoTags(conn *sql.DB) ([]ResumoTag, error) {
	rows, err := conn.Query(`SELECT tipo, descricao, ` + valEf("lancamentos") + `, vencimento FROM lancamentos WHERE descricao LIKE '%#%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	por := map[string]*ResumoTag{}
	for rows.Next() {
		var tipo, desc, venc string
		var v int64
		if err := rows.Scan(&tipo, &desc, &v, &venc); err != nil {
			return nil, err
		}
		for _, t := range TagsDe(desc) {
			r := por[t]
			if r == nil {
				r = &ResumoTag{Tag: t, De: venc, Ate: venc}
				por[t] = r
			}
			r.Qtd++
			if tipo == "pagar" {
				r.Despesas += v
			} else {
				r.Receitas += v
			}
			r.De, r.Ate = min(r.De, venc), max(r.Ate, venc)
		}
	}
	out := make([]ResumoTag, 0, len(por))
	for _, r := range por {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Despesas != out[j].Despesas {
			return out[i].Despesas > out[j].Despesas
		}
		return out[i].Tag < out[j].Tag
	})
	return out, rows.Err()
}

// TagsCmd trata `prisma tags [tag]`: o resumo de todas, ou os lançamentos de
// uma com a soma por categoria.
func TagsCmd(conn *sql.DB, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		rs, err := ResumoTags(conn)
		if err != nil {
			return err
		}
		if len(rs) == 0 {
			fmt.Println(`Nenhuma tag ainda. Ponha #palavras na descrição: prisma pagar add --desc "Hotel #viagem" ...`)
			return nil
		}
		w := novaTabela()
		fmt.Fprintln(w, "TAG\tLANÇAMENTOS\tDESPESAS\tRECEITAS\tPERÍODO")
		for _, r := range rs {
			fmt.Fprintf(w, "#%s\t%d\t%s\t%s\t%s a %s\n", r.Tag, r.Qtd, money.Format(r.Despesas), money.Format(r.Receitas), dataBR(r.De), dataBR(r.Ate))
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Println("\nDetalhe de uma tag: prisma tags viagem")
		return nil
	}
	tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(args[0]), "#"))
	rows, err := conn.Query(`
		SELECT id, tipo, descricao, categoria, ` + valEf("lancamentos") + `, vencimento, status
		FROM lancamentos WHERE descricao LIKE '%#%' ORDER BY vencimento, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	w := novaTabela()
	fmt.Fprintf(w, "#%s\n\nID\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVENCIMENTO\tVALOR\tSTATUS\n", tag)
	porCat := map[string]int64{}
	var desp, rec int64
	n := 0
	for rows.Next() {
		var id, v int64
		var tipo, desc, cat, venc, st string
		if err := rows.Scan(&id, &tipo, &desc, &cat, &v, &venc, &st); err != nil {
			return err
		}
		tem := false
		for _, t := range TagsDe(desc) {
			if t == tag {
				tem = true
				break
			}
		}
		if !tem {
			continue
		}
		n++
		if tipo == "pagar" {
			desp += v
			porCat[cat] += v
		} else {
			rec += v
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", id, tipo, truncar(desc, 36), cat, dataBR(venc), money.Format(v), st)
	}
	if n == 0 {
		fmt.Printf("Nenhum lançamento com #%s.\n", tag)
		return nil
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nTotal: despesas %s · receitas %s\n", money.Format(desp), money.Format(rec))
	if len(porCat) > 0 {
		cats := make([]string, 0, len(porCat))
		for c := range porCat {
			cats = append(cats, c)
		}
		sort.Slice(cats, func(i, j int) bool { return porCat[cats[i]] > porCat[cats[j]] })
		fmt.Println("\nDespesas por categoria:")
		for _, c := range cats {
			fmt.Printf("  %-18s %14s  %s\n", c, money.Format(porCat[c]), barra(porCat[c], desp))
		}
	}
	return nil
}
