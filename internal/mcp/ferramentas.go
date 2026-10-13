package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"prisma/internal/app"
	"prisma/internal/money"
)

// param descreve um parâmetro de ferramenta: vira o inputSchema e guia a
// validação genérica feita antes de rodar.
type param struct {
	nome        string
	tipo        string // "string" | "integer" | "number" | "boolean" | "dinheiro" (texto ou número)
	descricao   string
	enum        []string
	obrigatorio bool
}

// ferramenta é uma consulta exposta ao modelo. rodar recebe argumentos já
// validados contra params e monta os args das funções do app só com flags
// (--nome=valor): nenhum texto do modelo chega à posição de subcomando.
type ferramenta struct {
	nome      string
	descricao string
	params    []param
	rodar     func(conn *sql.DB, a argumentos) (string, error)
}

func (f ferramenta) esquema() map[string]any {
	props := map[string]any{}
	obrig := []string{}
	for _, p := range f.params {
		prop := map[string]any{"type": p.tipo, "description": p.descricao}
		if p.tipo == "dinheiro" {
			prop["type"] = []string{"string", "number"}
		}
		if len(p.enum) > 0 {
			prop["enum"] = p.enum
		}
		props[p.nome] = prop
		if p.obrigatorio {
			obrig = append(obrig, p.nome)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": obrig, "additionalProperties": false}
}

// valida confere obrigatórios, nomes desconhecidos, tipos e enums.
func (f ferramenta) valida(a argumentos) error {
	conhecidos := map[string]param{}
	for _, p := range f.params {
		conhecidos[p.nome] = p
		if _, ok := a[p.nome]; p.obrigatorio && !ok {
			return errArg(p.nome, "obrigatório")
		}
	}
	for nome, v := range a {
		p, ok := conhecidos[nome]
		if !ok {
			return fmt.Errorf("argumento desconhecido: %q", nome)
		}
		if v == nil {
			continue
		}
		switch p.tipo {
		case "string":
			s, ok := v.(string)
			if !ok {
				return errArg(nome, "esperava texto")
			}
			if len(p.enum) > 0 && !contem(p.enum, s) {
				return errArg(nome, "use um de: "+strings.Join(p.enum, ", "))
			}
		case "integer":
			n, ok := v.(float64)
			if !ok || n != math.Trunc(n) {
				return errArg(nome, "esperava número inteiro")
			}
		case "number":
			if _, ok := v.(float64); !ok {
				return errArg(nome, "esperava número")
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return errArg(nome, "esperava true/false")
			}
		case "dinheiro":
			switch v.(type) {
			case string, float64:
			default:
				return errArg(nome, "esperava valor em reais, ex.: \"1.299,90\" ou 1299.90")
			}
		}
	}
	return nil
}

// argumentos são os "arguments" de tools/call, já decodificados do JSON.
type argumentos map[string]any

func (a argumentos) texto(nome string) string {
	s, _ := a[nome].(string)
	return strings.TrimSpace(s)
}

func (a argumentos) inteiro(nome string, padrao, min, max int) (int, error) {
	v, ok := a[nome].(float64)
	if !ok {
		return padrao, nil
	}
	n := int(v)
	if n < min || n > max {
		return 0, errArg(nome, fmt.Sprintf("use de %d a %d", min, max))
	}
	return n, nil
}

func (a argumentos) booleano(nome string) bool {
	b, _ := a[nome].(bool)
	return b
}

var (
	reMes  = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
	reData = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func (a argumentos) mes(nome string) (string, error) {
	s := a.texto(nome)
	if s != "" && !reMes.MatchString(s) {
		return "", errArg(nome, "use AAAA-MM")
	}
	return s, nil
}

func (a argumentos) data(nome string) (string, error) {
	s := a.texto(nome)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil || !reData.MatchString(s) {
		return "", errArg(nome, "use AAAA-MM-DD")
	}
	return s, nil
}

// dinheiro aceita "1.234,56", "1234.56" ou um número JSON e devolve no formato
// que money.Parse entende (validado).
func (a argumentos) dinheiro(nome string) (string, error) {
	var s string
	switch v := a[nome].(type) {
	case float64:
		s = strconv.FormatFloat(v, 'f', 2, 64)
	case string:
		s = strings.TrimSpace(v)
	}
	if s == "" {
		return "", nil
	}
	c, err := money.Parse(s)
	if err != nil {
		return "", errArg(nome, err.Error())
	}
	if c < 0 {
		return "", errArg(nome, "não pode ser negativo")
	}
	return s, nil
}

func contem(lista []string, s string) bool {
	for _, x := range lista {
		if x == s {
			return true
		}
	}
	return false
}

// pMeses é o parâmetro de período comum a várias ferramentas.
func pMeses(padrao int) param {
	return param{nome: "meses", tipo: "integer", descricao: fmt.Sprintf("período em meses, de 1 a 36 (padrão %d)", padrao)}
}

// comMeses roda uma função do app que aceita só --meses.
func comMeses(padrao int, f func(*sql.DB, []string) error) func(*sql.DB, argumentos) (string, error) {
	return func(conn *sql.DB, a argumentos) (string, error) {
		m, err := a.inteiro("meses", padrao, 1, 36)
		if err != nil {
			return "", err
		}
		return captura(func() error { return f(conn, []string{"--meses=" + strconv.Itoa(m)}) })
	}
}

// analises mapeia o enum da ferramenta "analise" para as funções do Analytics
// que não precisam de parâmetros.
var analises = map[string]func(*sql.DB) error{
	"health_score":        app.AnalyticsHealthScore,
	"runway":              app.AnalyticsRunway,
	"anomalias":           app.AnalyticsAnomalias,
	"tendencias":          app.AnalyticsTendencias,
	"sazonalidade":        app.AnalyticsSazonalidade,
	"regra_50_30_20":      app.AnalyticsRegra502030,
	"patrimonio":          app.AnalyticsPatrimonio,
	"comprometimento":     app.AnalyticsComprometimento,
	"assinaturas_ocultas": app.AnalyticsAssinaturasOcultas,
	"aderencia_planos":    app.AnalyticsAderenciaPlanos,
	"inflacao":            app.AnalyticsInflacao,
	"utilidades":          app.AnalyticsUtilidades,
	"grupos":              app.AnalyticsGrupos,
	"melhor_dia_cartao":   app.AnalyticsMelhorDiaCartao,
}

// nomesAnalises lista as chaves de analises numa ordem estável.
var nomesAnalises = []string{
	"health_score", "runway", "anomalias", "tendencias", "sazonalidade",
	"regra_50_30_20", "patrimonio", "comprometimento", "assinaturas_ocultas",
	"aderencia_planos", "inflacao", "utilidades", "grupos", "melhor_dia_cartao",
}

func buscaFerramenta(nome string) (ferramenta, bool) {
	for _, f := range ferramentas {
		if f.nome == nome {
			return f, true
		}
	}
	return ferramenta{}, false
}

var ferramentas []ferramenta

func init() {
	ferramentas = []ferramenta{
		{
			nome:      "saldo",
			descricao: "Posição geral consolidada: saldo de cada conta e carteira, pendências e total.",
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				return captura(func() error { return app.Saldo(conn, nil) })
			},
		},
		{
			nome:      "lancamentos",
			descricao: "Lista lançamentos (despesas = pagar, receitas = receber) com id, descrição, valor efetivo, categoria, vencimento e status. Filtre sempre que possível.",
			params: []param{
				{nome: "mes", tipo: "string", descricao: "mês de vencimento, AAAA-MM"},
				{nome: "de", tipo: "string", descricao: "vencimento a partir desta data, AAAA-MM-DD"},
				{nome: "ate", tipo: "string", descricao: "vencimento até esta data (inclusive), AAAA-MM-DD"},
				{nome: "categoria", tipo: "string", descricao: "nome da categoria (veja a ferramenta categorias)"},
				{nome: "tipo", tipo: "string", descricao: "pagar (despesa) ou receber (receita)", enum: []string{"pagar", "receber"}},
				{nome: "so_pendentes", tipo: "boolean", descricao: "só o que ainda falta pagar/receber"},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				var args []string
				if m, err := a.mes("mes"); err != nil {
					return "", err
				} else if m != "" {
					args = append(args, "--mes="+m)
				}
				for _, nome := range []string{"de", "ate"} {
					d, err := a.data(nome)
					if err != nil {
						return "", err
					}
					if d != "" {
						args = append(args, "--"+nome+"="+d)
					}
				}
				if c := a.texto("categoria"); c != "" {
					args = append(args, "--cat="+c)
				}
				if t := a.texto("tipo"); t != "" {
					args = append(args, "--tipo="+t)
				}
				if a.booleano("so_pendentes") {
					args = append(args, "--pendentes")
				}
				return captura(func() error { return app.Lancamentos(conn, args) })
			},
		},
		{
			nome:      "relatorio",
			descricao: "Gastos por categoria e receita/despesa mês a mês no período.",
			params:    []param{pMeses(6)},
			rodar:     comMeses(6, app.Relatorio),
		},
		{
			nome:      "extrato",
			descricao: "Extrato de uma conta ou carteira (informe exatamente uma; ids na ferramenta saldo): movimentações quitadas com saldo corrente.",
			params: []param{
				pMeses(3),
				{nome: "conta", tipo: "integer", descricao: "id da conta"},
				{nome: "carteira", tipo: "integer", descricao: "id da carteira"},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				m, err := a.inteiro("meses", 3, 1, 36)
				if err != nil {
					return "", err
				}
				args := []string{"--meses=" + strconv.Itoa(m)}
				for _, nome := range []string{"conta", "carteira"} {
					id, err := a.inteiro(nome, 0, 0, math.MaxInt32)
					if err != nil {
						return "", err
					}
					if id > 0 {
						args = append(args, "--"+nome+"="+strconv.Itoa(id))
					}
				}
				if len(args) != 2 {
					return "", fmt.Errorf("informe exatamente uma: conta ou carteira")
				}
				return captura(func() error { return app.Extrato(conn, args) })
			},
		},
		{
			nome:      "previsao",
			descricao: "Projeção do saldo futuro mês a mês, com pendências, parcelas e recorrências.",
			params:    []param{pMeses(6)},
			rodar:     comMeses(6, app.Previsao),
		},
		{
			nome:      "estatisticas",
			descricao: "Resumo estatístico do período: médias, tendência, maiores gastos e saúde financeira.",
			params:    []param{pMeses(6)},
			rodar:     comMeses(6, app.Estatisticas),
		},
		{
			nome:      "cartoes",
			descricao: "Lista os cartões de crédito com id, limite, fechamento e vencimento.",
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				return captura(func() error { return app.Cartao(conn, []string{"listar"}) })
			},
		},
		{
			nome:      "fatura",
			descricao: "Fatura de um cartão (id na ferramenta cartoes): lançamentos, total e gastos por categoria.",
			params: []param{
				{nome: "cartao", tipo: "integer", descricao: "id do cartão", obrigatorio: true},
				{nome: "ref", tipo: "string", descricao: "fatura AAAA-MM (padrão: a aberta)"},
				{nome: "so_abertos", tipo: "boolean", descricao: "só lançamentos ainda em aberto"},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				id, err := a.inteiro("cartao", 0, 1, math.MaxInt32)
				if err != nil {
					return "", err
				}
				args := []string{"--cartao=" + strconv.Itoa(id)}
				if r, err := a.mes("ref"); err != nil {
					return "", err
				} else if r != "" {
					args = append(args, "--ref="+r)
				}
				if a.booleano("so_abertos") {
					args = append(args, "--abertos")
				}
				return captura(func() error { return app.Fatura(conn, args) })
			},
		},
		{
			nome:      "simular",
			descricao: "Simula uma compra (à vista ou parcelada) e mostra o impacto na previsão de saldo. Não grava nada.",
			params: []param{
				{nome: "valor", tipo: "dinheiro", descricao: "preço total em reais, ex.: \"1.299,90\" ou 1299.90", obrigatorio: true},
				{nome: "parcelas", tipo: "integer", descricao: "número de parcelas mensais (padrão 1 = à vista)"},
				{nome: "juros", tipo: "number", descricao: "juros do parcelamento em % ao mês"},
				{nome: "entrada", tipo: "dinheiro", descricao: "valor de entrada pago à vista, em reais"},
				{nome: "descricao", tipo: "string", descricao: "o que vai comprar"},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				v, err := a.dinheiro("valor")
				if err != nil {
					return "", err
				}
				if v == "" {
					return "", errArg("valor", "obrigatório")
				}
				p, err := a.inteiro("parcelas", 1, 1, 120)
				if err != nil {
					return "", err
				}
				args := []string{"--valor=" + v, "--parcelas=" + strconv.Itoa(p)}
				if j, ok := a["juros"].(float64); ok {
					if j < 0 || j > 100 {
						return "", errArg("juros", "use de 0 a 100")
					}
					args = append(args, "--juros="+strconv.FormatFloat(j, 'f', -1, 64))
				}
				e, err := a.dinheiro("entrada")
				if err != nil {
					return "", err
				}
				if e != "" {
					args = append(args, "--entrada="+e)
				}
				if d := a.texto("descricao"); d != "" {
					args = append(args, "--desc="+d)
				}
				return captura(func() error { return app.Simular(conn, args) })
			},
		},
		{
			nome:      "analise",
			descricao: "Análises do Prisma Analytics: health_score (saúde 0–100), runway (quanto tempo o caixa dura), anomalias (gastos fora do padrão), tendencias (categorias subindo/caindo), sazonalidade (picos anuais), regra_50_30_20, patrimonio (patrimônio líquido), comprometimento (renda já tomada nos próximos meses), assinaturas_ocultas, aderencia_planos (planejado x realizado), inflacao (inflação pessoal), utilidades (contas da casa), grupos (balanço de despesas divididas), melhor_dia_cartao.",
			params: []param{
				{nome: "tipo", tipo: "string", descricao: "qual análise", enum: nomesAnalises, obrigatorio: true},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				f := analises[a.texto("tipo")]
				return captura(func() error { return f(conn) })
			},
		},
		{
			nome:      "categorias",
			descricao: "Lista as categorias cadastradas.",
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				return strings.Join(app.ListaCategorias(conn), "\n"), nil
			},
		},
		{
			nome:      "schema",
			descricao: "Mostra o CREATE TABLE de todas as tabelas do banco (com comentários), para montar consulta_sql.",
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				return consultaSQL(conn, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`, true)
			},
		},
		{
			nome: "consulta_sql",
			descricao: "Último recurso: roda um SELECT (ou WITH ... SELECT) somente leitura no SQLite, até " + strconv.Itoa(maxLinhasSQL) + " linhas. " +
				"Valores monetários são INTEGER em centavos. lancamentos.tipo é 'pagar' (despesa) ou 'receber' (receita); status 'pendente' ou 'quitado'. " +
				"Despesas divididas em grupo (grupo_id) só pesam a parte do usuário: valor / nº de pessoas do grupo, salvo recebe_pagamento = 1. " +
				"Transferências entre contas ficam em transferencias e não são gasto. Consulte schema antes.",
			params: []param{
				{nome: "sql", tipo: "string", descricao: "uma única instrução SELECT/WITH, sem ponto e vírgula", obrigatorio: true},
			},
			rodar: func(conn *sql.DB, a argumentos) (string, error) {
				q, err := validaSelect(a.texto("sql"))
				if err != nil {
					return "", err
				}
				return consultaSQL(conn, q, false)
			},
		},
	}
}

const maxLinhasSQL = 200

// validaSelect aceita só uma instrução que começa com SELECT ou WITH. É uma
// barreira de conveniência; a garantia de leitura é a conexão query_only.
func validaSelect(q string) (string, error) {
	q = strings.TrimSuffix(strings.TrimSpace(q), ";")
	if q == "" {
		return "", errArg("sql", "vazio")
	}
	if strings.Contains(q, ";") {
		return "", errArg("sql", "só uma instrução, sem ponto e vírgula")
	}
	inicio := strings.ToUpper(strings.Fields(q)[0])
	if inicio != "SELECT" && inicio != "WITH" {
		return "", errArg("sql", "só SELECT ou WITH (somente leitura)")
	}
	return q, nil
}

// consultaSQL roda q e devolve as linhas em colunas alinhadas. bruto devolve
// só a primeira coluna, uma célula por bloco (para o schema).
func consultaSQL(conn *sql.DB, q string, bruto bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	if !bruto {
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
	}
	n, cortado := 0, false
	for rows.Next() {
		if n == maxLinhasSQL {
			cortado = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		if bruto {
			fmt.Fprintf(&b, "%s;\n\n", celula(vals[0]))
		} else {
			celulas := make([]string, len(vals))
			for i, v := range vals {
				celulas[i] = strings.ReplaceAll(celula(v), "\t", " ")
			}
			fmt.Fprintln(tw, strings.Join(celulas, "\t"))
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if !bruto {
		tw.Flush()
		if cortado {
			fmt.Fprintf(&b, "\n[mais de %d linhas: resultado cortado, agregue ou filtre]\n", maxLinhasSQL)
		} else {
			fmt.Fprintf(&b, "\n(%d linhas)\n", n)
		}
	}
	return b.String(), nil
}

func celula(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprint(x)
	}
}
