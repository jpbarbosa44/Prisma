package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"prisma/internal/money"
)

// A busca global: acha lançamentos (pela descrição, categoria, observação ou
// valor) e cadastros (contas, carteiras, cartões, recorrências e metas) pelo
// nome, em qualquer mês. Sem caixa e sem acento; vários termos precisam todos
// aparecer. Um termo que é valor ("89,90") casa com o valor exato ou a menos de
// 1%; "#viagem" casa com a tag na descrição.

// Resultado é um achado da busca.
type Resultado struct {
	Tipo    string `json:"tipo"` // lancamento | conta | carteira | cartao | recorrencia | meta
	ID      int64  `json:"id"`
	Titulo  string `json:"titulo"`
	Detalhe string `json:"detalhe"`
	Valor   int64  `json:"valor"`
	Data    string `json:"data,omitempty"` // AAAA-MM-DD (lançamentos)
	Tela    string `json:"tela"`           // título da tela que mostra o achado
	Quitado bool   `json:"quitado,omitempty"`
}

// consultaBusca é o termo já preparado.
type consultaBusca struct {
	palavras []string // sem acento, minúsculas
	valor    int64    // > 0 quando algum termo é um valor
}

func preparaBusca(termo string) consultaBusca {
	var c consultaBusca
	for _, p := range strings.Fields(termo) {
		if v, err := money.Parse(strings.TrimPrefix(p, "R$")); err == nil && v > 0 && strings.ContainsAny(p, "0123456789") && !strings.HasPrefix(p, "#") {
			c.valor = v
			continue
		}
		c.palavras = append(c.palavras, normaliza(p))
	}
	return c
}

// normaliza tira acento e caixa.
func normaliza(s string) string { return tiraAcento.Replace(strings.ToLower(s)) }

// casa diz se o texto (já com os campos juntos) e o valor batem com a busca.
func (c consultaBusca) casa(texto string, valor int64) bool {
	if c.valor > 0 {
		d := valor - c.valor
		if d < 0 {
			d = -d
		}
		if d*100 > c.valor { // mais de 1% de diferença
			return false
		}
	}
	t := normaliza(texto)
	for _, p := range c.palavras {
		if strings.HasPrefix(p, "#") {
			if !temTag(t, p) {
				return false
			}
			continue
		}
		if !strings.Contains(t, p) {
			return false
		}
	}
	return true
}

// temTag diz se a tag (com #) aparece inteira no texto: "#viagem" casa com
// "hotel #viagem" mas não com "#viagem-2026".
func temTag(texto, tag string) bool {
	for i := 0; ; {
		j := strings.Index(texto[i:], tag)
		if j < 0 {
			return false
		}
		fim := i + j + len(tag)
		if fim == len(texto) || !ehLetraTag(texto[fim]) {
			return true
		}
		i = fim
	}
}

func ehLetraTag(b byte) bool {
	return b == '-' || b == '_' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 0x80
}

// Buscar procura o termo em tudo; limite <= 0 vale 50. Lançamentos vêm do mais
// recente para o mais antigo, depois os cadastros.
func Buscar(conn *sql.DB, termo string, limite int) ([]Resultado, error) {
	if limite <= 0 {
		limite = 50
	}
	c := preparaBusca(termo)
	out := []Resultado{}
	if len(c.palavras) == 0 && c.valor == 0 {
		return out, nil
	}

	rows, err := conn.Query(`
		SELECT l.id, l.tipo, l.descricao, l.categoria, l.observacao, ` + valEf("l") + `, l.vencimento, l.status,
		       l.cartao_id IS NOT NULL, COALESCE(c.nome, w.nome, k.nome, '')
		FROM lancamentos l
		LEFT JOIN contas c ON c.id = l.conta_id
		LEFT JOIN carteiras w ON w.id = l.carteira_id
		LEFT JOIN cartoes k ON k.id = l.cartao_id
		ORDER BY l.vencimento DESC, l.id DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r Resultado
		var tipo, cat, obs, status, onde string
		var noCartao bool
		if err := rows.Scan(&r.ID, &tipo, &r.Titulo, &cat, &obs, &r.Valor, &r.Data, &status, &noCartao, &onde); err != nil {
			rows.Close()
			return nil, err
		}
		if !c.casa(r.Titulo+" "+cat+" "+obs, r.Valor) {
			continue
		}
		r.Tipo, r.Quitado = "lancamento", status == "quitado"
		r.Detalhe = tipo + " · " + cat
		if onde != "" {
			r.Detalhe += " · " + onde
		}
		r.Tela = "Pagar/Receber"
		if noCartao {
			r.Tela = "Cartões"
		}
		out = append(out, r)
		if len(out) >= limite {
			break
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cadastros := []struct {
		tipo, tela, sql string
	}{
		{"conta", "Contas", `SELECT id, nome, banco || ' · ' || tipo, saldo_inicial FROM contas`},
		{"carteira", "Carteiras", `SELECT id, nome, descricao, saldo_inicial FROM carteiras`},
		{"cartao", "Cartões", `SELECT id, nome, 'fecha dia ' || dia_fechamento || ', vence dia ' || dia_vencimento, limite FROM cartoes`},
		{"recorrencia", "Recorrências", `SELECT id, descricao, tipo || ' · ' || categoria || ' · dia ' || dia, valor FROM recorrencias WHERE fim IS NULL OR fim >= date('now','localtime')`},
		{"meta", "Metas", `SELECT id, nome, CASE WHEN prazo = '' THEN 'sem prazo' ELSE 'até ' || prazo END, alvo FROM metas WHERE status = 'ativa'`},
	}
	for _, cad := range cadastros {
		if len(out) >= limite {
			break
		}
		rows, err := conn.Query(cad.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			r := Resultado{Tipo: cad.tipo, Tela: cad.tela}
			if err := rows.Scan(&r.ID, &r.Titulo, &r.Detalhe, &r.Valor); err != nil {
				rows.Close()
				return nil, err
			}
			// cadastros casam pelo nome; o valor só conta quando é o que se busca
			if c.casa(r.Titulo+" "+r.Detalhe, r.Valor) && (len(c.palavras) > 0 || c.valor > 0) {
				out = append(out, r)
				if len(out) >= limite {
					break
				}
			}
		}
		rows.Close()
	}
	return out, nil
}

// BuscarCmd trata `prisma buscar <termo> [--limite N]`.
func BuscarCmd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("buscar", flag.ContinueOnError)
	limite := fs.Int("limite", 30, "máximo de resultados")
	var termos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		termos = append(termos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	termo := strings.Join(termos, " ")
	if strings.TrimSpace(termo) == "" {
		return fmt.Errorf(`uso: prisma buscar <termo> (ex.: prisma buscar mercado, prisma buscar 89,90, prisma buscar "#viagem")`)
	}
	rs, err := Buscar(conn, termo, *limite)
	if err != nil {
		return err
	}
	if len(rs) == 0 {
		fmt.Printf("Nada encontrado para %q.\n", termo)
		return nil
	}
	fmt.Printf("%d resultado(s) para %q:\n\n", len(rs), termo)
	for _, r := range rs {
		switch r.Tipo {
		case "lancamento":
			st := "pendente"
			if r.Quitado {
				st = "quitado"
			}
			fmt.Printf("  #%-5d %s  %-34s %14s  %s · %s\n", r.ID, dataBR(r.Data), truncar(r.Titulo, 34), money.Format(r.Valor), r.Detalhe, st)
		default:
			fmt.Printf("  %-11s #%-4d %-34s %14s  %s\n", r.Tipo, r.ID, truncar(r.Titulo, 34), money.Format(r.Valor), r.Detalhe)
		}
	}
	return nil
}
