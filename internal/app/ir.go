package app

import (
	"database/sql"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"prisma/internal/money"
)

// Ajuda para a declaração do Imposto de Renda: soma, do ano-calendário, as
// despesas dedutíveis (saúde, educação, previdência privada), os rendimentos
// recebidos por categoria e os saldos de contas, carteiras e aplicações em
// 31/12 (as fichas de Bens e Direitos pedem o ano e o anterior). É uma ajuda
// para preencher o programa da Receita, não um substituto dele.

// Grupos de despesas dedutíveis, com as categorias que caem em cada um (o
// padrão; `prisma ir categorias` muda e guarda nas preferências).
var gruposIR = []struct {
	chave, nome string
	padrao      []string
}{
	{"saude", "Saúde", []string{"saude", "medico", "dentista", "plano de saude", "plano-de-saude", "hospital", "exames", "psicologo", "terapia", "fisioterapia"}},
	{"educacao", "Educação", []string{"educacao", "escola", "faculdade", "mensalidade escolar"}},
	{"previdencia", "Previdência privada (PGBL)", []string{"previdencia", "pgbl"}},
}

// Valores-limite da declaração. REVISAR TODO ANO: a Receita atualiza (ou não)
// na regra da declaração; aqui estão os vigentes em 2026.
const (
	limiteEducacaoPessoa = 356150 // R$ 3.561,50 por pessoa (titular ou dependente)
	pgblPctRenda         = 12     // PGBL deduz até 12% da renda tributável
)

// ItemIR é um lançamento que entra no relatório.
type ItemIR struct {
	ID        int64  `json:"id"`
	Data      string `json:"data"`
	Descricao string `json:"descricao"`
	Categoria string `json:"categoria"`
	Valor     int64  `json:"valor"`
}

// GrupoIR é um grupo de despesas dedutíveis.
type GrupoIR struct {
	Chave      string   `json:"chave"`
	Nome       string   `json:"nome"`
	Categorias []string `json:"categorias"`
	Total      int64    `json:"total"`
	Itens      []ItemIR `json:"itens"`
	Aviso      string   `json:"aviso,omitempty"`
}

// RendimentoIR é a receita de uma categoria no ano.
type RendimentoIR struct {
	Categoria string `json:"categoria"`
	Total     int64  `json:"total"`
	Qtd       int    `json:"qtd"`
}

// BemIR é uma conta, carteira ou aplicação com o saldo em 31/12.
type BemIR struct {
	Tipo     string `json:"tipo"` // conta | carteira
	Nome     string `json:"nome"`
	Detalhe  string `json:"detalhe"`
	Anterior int64  `json:"anterior"` // 31/12 do ano anterior
	Atual    int64  `json:"atual"`    // 31/12 do ano
}

// DadosIR é o relatório de um ano-calendário.
type DadosIR struct {
	Ano          int            `json:"ano"`
	Gerado       string         `json:"gerado"`
	Deducoes     []GrupoIR      `json:"deducoes"`
	TotalDeduz   int64          `json:"totalDeduz"`
	Rendimentos  []RendimentoIR `json:"rendimentos"`
	TotalRend    int64          `json:"totalRend"`
	Bens         []BemIR        `json:"bens"`
	LimiteEduc   int64          `json:"limiteEduc"`
	PgblPctRenda int            `json:"pgblPctRenda"`
}

// categoriasIR devolve as categorias de um grupo (preferência ou padrão).
func categoriasIR(conn *sql.DB, chave string, padrao []string) ([]string, error) {
	v, err := Pref(conn, "ir:"+chave)
	if err != nil {
		return nil, err
	}
	if v == "" {
		return padrao, nil
	}
	var out []string
	for _, c := range strings.Split(v, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// saldoLocalEm é o saldo de uma conta ou carteira ao fim do dia `data`.
func saldoLocalEm(conn *sql.DB, tipo string, id int64, data string) (int64, error) {
	tabela := tipo + "s"
	var s int64
	err := conn.QueryRow(`
		SELECT c.saldo_inicial + COALESCE((
			SELECT SUM(CASE l.tipo WHEN 'receber' THEN `+valEf("l")+` ELSE -`+valEf("l")+` END)
			FROM lancamentos l WHERE l.`+tipo+`_id = c.id AND l.status = 'quitado' AND l.quitado_em <= ?
		), 0) + COALESCE((
			SELECT SUM(t.valor) FROM transferencias t
			WHERE t.destino_tipo = ? AND t.destino_id = c.id AND t.data <= ?
		), 0) - COALESCE((
			SELECT SUM(t.valor) FROM transferencias t
			WHERE t.origem_tipo = ? AND t.origem_id = c.id AND t.data <= ?
		), 0)
		FROM `+tabela+` c WHERE c.id = ?`, data, tipo, data, tipo, data, id).Scan(&s)
	return s, err
}

// RelatorioIR monta o relatório do ano-calendário (0 = o ano passado, que é o
// que se declara no ano corrente).
func RelatorioIR(conn *sql.DB, ano int) (DadosIR, error) {
	if ano == 0 {
		ano = relogio().Year() - 1
	}
	d := DadosIR{Ano: ano, Gerado: relogio().Format("2006-01-02 15:04"), LimiteEduc: limiteEducacaoPessoa, PgblPctRenda: pgblPctRenda,
		Deducoes: []GrupoIR{}, Rendimentos: []RendimentoIR{}, Bens: []BemIR{}}
	de, ate := fmt.Sprintf("%d-01-01", ano), fmt.Sprintf("%d-12-31", ano)

	// despesas pagas no ano, por categoria normalizada
	rows, err := conn.Query(`
		SELECT id, `+competencia+`, descricao, categoria, `+valEf("lancamentos")+` FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado' AND `+competencia+` >= ? AND `+competencia+` <= ?
		ORDER BY `+competencia+`, id`, de, ate)
	if err != nil {
		return d, err
	}
	var despesas []ItemIR
	for rows.Next() {
		var it ItemIR
		if err := rows.Scan(&it.ID, &it.Data, &it.Descricao, &it.Categoria, &it.Valor); err != nil {
			rows.Close()
			return d, err
		}
		despesas = append(despesas, it)
	}
	rows.Close()
	for _, g := range gruposIR {
		cats, err := categoriasIR(conn, g.chave, g.padrao)
		if err != nil {
			return d, err
		}
		alvo := map[string]bool{}
		for _, c := range cats {
			alvo[normaliza(c)] = true
		}
		gr := GrupoIR{Chave: g.chave, Nome: g.nome, Categorias: cats, Itens: []ItemIR{}}
		for _, it := range despesas {
			if alvo[normaliza(it.Categoria)] {
				gr.Itens = append(gr.Itens, it)
				gr.Total += it.Valor
			}
		}
		d.Deducoes = append(d.Deducoes, gr)
		d.TotalDeduz += gr.Total
	}

	// rendimentos: receitas recebidas no ano por categoria (reembolso não é renda)
	rows, err = conn.Query(`
		SELECT categoria, SUM(`+valEf("lancamentos")+`), COUNT(*) FROM lancamentos
		WHERE tipo = 'receber' AND status = 'quitado' AND categoria <> 'reembolso'
		  AND quitado_em >= ? AND quitado_em <= ?
		GROUP BY categoria ORDER BY 2 DESC`, de, ate)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var r RendimentoIR
		if err := rows.Scan(&r.Categoria, &r.Total, &r.Qtd); err != nil {
			rows.Close()
			return d, err
		}
		d.Rendimentos = append(d.Rendimentos, r)
		d.TotalRend += r.Total
	}
	rows.Close()
	for i := range d.Deducoes {
		g := &d.Deducoes[i]
		switch g.Chave {
		case "educacao":
			if g.Total > limiteEducacaoPessoa {
				g.Aviso = fmt.Sprintf("O limite é %s por pessoa (titular ou dependente): o excedente não deduz.", money.Format(limiteEducacaoPessoa))
			}
		case "previdencia":
			if teto := d.TotalRend * pgblPctRenda / 100; g.Total > 0 && g.Total > teto {
				g.Aviso = fmt.Sprintf("PGBL deduz até %d%% da renda tributável (≈ %s com as receitas registradas).", pgblPctRenda, money.Format(teto))
			}
		}
	}

	// bens e direitos: saldo em 31/12 do ano e do anterior
	antes := fmt.Sprintf("%d-12-31", ano-1)
	for _, tipo := range []string{"conta", "carteira"} {
		q := `SELECT id, nome, banco, tipo FROM contas ORDER BY id`
		if tipo == "carteira" {
			q = `SELECT id, nome, descricao, 'carteira' FROM carteiras ORDER BY id`
		}
		rows, err := conn.Query(q)
		if err != nil {
			return d, err
		}
		type loc struct {
			id             int64
			nome, det, sub string
		}
		var ls []loc
		for rows.Next() {
			var l loc
			if err := rows.Scan(&l.id, &l.nome, &l.det, &l.sub); err != nil {
				rows.Close()
				return d, err
			}
			ls = append(ls, l)
		}
		rows.Close()
		for _, l := range ls {
			b := BemIR{Tipo: tipo, Nome: l.nome, Detalhe: strings.TrimSpace(strings.Trim(l.det+" · "+l.sub, " ·"))}
			// conta de uma aplicação: o custo vem dos aportes e resgates de cada
			// data (o "já aplicado" do cadastro entra no saldo inicial, que não tem data)
			var inv int64
			if tipo == "conta" {
				conn.QueryRow(`SELECT id FROM investimentos WHERE conta_id = ? ORDER BY id LIMIT 1`, l.id).Scan(&inv)
			}
			if inv != 0 {
				movs, err := movsInvestimento(conn, inv)
				if err != nil {
					return d, err
				}
				b.Anterior, _, _ = posicaoEm(movs, antes)
				b.Atual, _, _ = posicaoEm(movs, ate)
				if b.Anterior != 0 || b.Atual != 0 {
					d.Bens = append(d.Bens, b)
				}
				continue
			}
			if b.Anterior, err = saldoLocalEm(conn, tipo, l.id, antes); err != nil {
				return d, err
			}
			if b.Atual, err = saldoLocalEm(conn, tipo, l.id, ate); err != nil {
				return d, err
			}
			if b.Anterior != 0 || b.Atual != 0 {
				d.Bens = append(d.Bens, b)
			}
		}
	}
	sort.SliceStable(d.Bens, func(i, j int) bool { return d.Bens[i].Atual > d.Bens[j].Atual })
	return d, nil
}

// IRCmd trata `prisma ir [--ano AAAA]` e `prisma ir categorias [--saude a,b] [--educacao ...] [--previdencia ...]`.
func IRCmd(conn *sql.DB, args []string) error {
	if len(args) > 0 && args[0] == "categorias" {
		return irCategorias(conn, args[1:])
	}
	fs := flag.NewFlagSet("ir", flag.ContinueOnError)
	ano := fs.Int("ano", 0, "ano-calendário (padrão: o ano passado)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	d, err := RelatorioIR(conn, *ano)
	if err != nil {
		return err
	}
	fmt.Printf("AJUDA PARA O IMPOSTO DE RENDA — ano-calendário %d\n", d.Ano)
	fmt.Println("(confira tudo no programa da Receita; aqui só o que foi registrado no Prisma)")
	fmt.Println()
	fmt.Println("DESPESAS DEDUTÍVEIS")
	for _, g := range d.Deducoes {
		fmt.Printf("\n  %s: %s   (categorias: %s)\n", g.Nome, money.Format(g.Total), strings.Join(g.Categorias, ", "))
		for _, it := range g.Itens {
			fmt.Printf("    %s  %-36s %14s\n", dataBR(it.Data), truncar(it.Descricao, 36), money.Format(it.Valor))
		}
		if g.Aviso != "" {
			fmt.Printf("    ⚠ %s\n", g.Aviso)
		}
	}
	fmt.Println()
	fmt.Println("RENDIMENTOS RECEBIDOS (por categoria)")
	if len(d.Rendimentos) == 0 {
		fmt.Println("  nenhum registrado")
	}
	for _, r := range d.Rendimentos {
		fmt.Printf("  %-24s %14s  (%d lançamento(s))\n", r.Categoria, money.Format(r.Total), r.Qtd)
	}
	if d.TotalRend > 0 {
		fmt.Printf("  %-24s %14s\n", "total", money.Format(d.TotalRend))
	}
	fmt.Println()
	fmt.Printf("BENS E DIREITOS (saldo em 31/12/%d e 31/12/%d)\n", d.Ano-1, d.Ano)
	if len(d.Bens) == 0 {
		fmt.Println("  nenhuma conta com saldo")
	}
	for _, b := range d.Bens {
		fmt.Printf("  %-28s %14s %14s  %s\n", truncar(b.Nome, 28), money.Format(b.Anterior), money.Format(b.Atual), b.Detalhe)
	}
	fmt.Println("\n(aplicações aparecem pelo valor aplicado, que é o que se declara; categorias dedutíveis: prisma ir categorias)")
	return nil
}

func irCategorias(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ir categorias", flag.ContinueOnError)
	vals := map[string]*string{}
	for _, g := range gruposIR {
		vals[g.chave] = fs.String(g.chave, "", "categorias de "+g.nome+", separadas por vírgula (\"-\" volta ao padrão)")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	for _, g := range gruposIR {
		if !informado[g.chave] {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(*vals[g.chave]))
		if v == "-" {
			v = ""
		}
		if err := SetPref(conn, "ir:"+g.chave, v); err != nil {
			return err
		}
	}
	for _, g := range gruposIR {
		cats, err := categoriasIR(conn, g.chave, g.padrao)
		if err != nil {
			return err
		}
		fmt.Printf("%-28s %s\n", g.nome+":", strings.Join(cats, ", "))
	}
	if len(informado) == 0 {
		fmt.Println("\nMude com: prisma ir categorias --saude saude,dentista --educacao escola")
	}
	return nil
}

// AnoIR lê o ano de uma string ("" = o ano passado).
func AnoIR(s string) (int, error) {
	if s == "" {
		return relogio().Year() - 1, nil
	}
	a, err := strconv.Atoi(s)
	if err != nil || a < 2000 || a > time.Now().Year()+1 {
		return 0, fmt.Errorf("ano inválido: %q", s)
	}
	return a, nil
}
