package mcp

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"prisma/internal/app"
	"prisma/internal/db"
	"prisma/internal/money"
)

// Ferramentas que leem extratos e cadastram dados. As de gravação só existem
// com `prisma mcp --escrita` (ferramenta.escreve). Todas montam os argumentos
// das funções do app a partir de parâmetros validados, como as de leitura.

const (
	maxExtrato     = 2 << 20 // 2 MB
	maxLancamentos = 2000    // por chamada de importar_lancamentos
	maxSnapshots   = 10      // snapshots "mcp-antes-*" guardados
)

var reLote = regexp.MustCompile(`^mcp-\d{8}-\d{6}$`)

// marcaLote é o sufixo da observação que identifica o lote de importação.
func marcaLote(lote string) string { return "[lote " + lote + "]" }

// ferramentasExtrato são ler_extrato e as de gravação; entram no fim da tabela
// (ferramentas.go), que as filtra por sessão.
func ferramentasExtrato() []ferramenta {
	return []ferramenta{
		{
			nome: "ler_extrato",
			descricao: "Lê um extrato ou fatura do disco (.csv, .ofx, .txt ou .tsv, até 2 MB): devolve as linhas cruas " +
				"(convertendo de Latin-1 quando preciso) e uma interpretação automática (data, descrição, valor) para conferência. " +
				"Interprete você as colunas — bancos variam muito — e use o resultado em importar_lancamentos.",
			params: []param{
				{nome: "caminho", tipo: "string", descricao: "caminho do arquivo (absoluto, ou ~/...)", obrigatorio: true},
				{nome: "desde_linha", tipo: "integer", descricao: "primeira linha a mostrar (padrão 1)"},
				{nome: "linhas", tipo: "integer", descricao: "quantas linhas mostrar, até 500 (padrão 200)"},
			},
			rodar: lerExtrato,
		},
		{
			nome:    "importar_lancamentos",
			escreve: true,
			descricao: "Cadastra lançamentos (despesas = pagar, receitas = receber). Sem confirmar=true, só mostra a PRÉVIA: " +
				"o que entraria, o que já existe (duplicado: mesmo tipo, descrição, valor e data — é ignorado), categorias novas e totais. " +
				"Com confirmar=true grava tudo num lote (marcado na observação), após um backup do banco. " +
				"Compras no cartão: informe cartao e a data da COMPRA; o vencimento vira o da fatura e, sem quitado, ficam pendentes até pagar a fatura. " +
				"Movimentos de conta: informe conta (ou carteira) e quitado=true (padrão fora do cartão). " +
				"Não importe pagamento de fatura nem transferência entre contas próprias como despesa: isso duplicaria os gastos.",
			params: []param{
				{nome: "lancamentos", tipo: "array", obrigatorio: true, descricao: "até 2000 por chamada",
					itens: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"data":       map[string]any{"type": "string", "description": "AAAA-MM-DD (no cartão, a data da compra)"},
							"descricao":  map[string]any{"type": "string"},
							"valor":      map[string]any{"type": []string{"string", "number"}, "description": "positivo, em reais: \"1.234,56\" ou 1234.56"},
							"tipo":       map[string]any{"type": "string", "enum": []string{"pagar", "receber"}},
							"categoria":  map[string]any{"type": "string", "description": "minúsculas; padrão geral"},
							"conta":      map[string]any{"type": "integer"},
							"carteira":   map[string]any{"type": "integer"},
							"cartao":     map[string]any{"type": "integer"},
							"quitado":    map[string]any{"type": "boolean"},
							"observacao": map[string]any{"type": "string"},
						},
						"required":             []string{"data", "descricao", "valor", "tipo"},
						"additionalProperties": false,
					}},
				{nome: "confirmar", tipo: "boolean", descricao: "true grava; ausente ou false só mostra a prévia"},
			},
			rodar: importarLancamentos,
		},
		{
			nome:    "desfazer_importacao",
			escreve: true,
			descricao: "Apaga todos os lançamentos de um lote criado por importar_lancamentos. " +
				"Sem lote, lista os lotes existentes com a quantidade e o total de cada um.",
			params: []param{
				{nome: "lote", tipo: "string", descricao: "id do lote, ex.: mcp-20261013-195501"},
			},
			rodar: desfazerImportacao,
		},
		{
			nome:    "definir_categoria",
			escreve: true,
			descricao: "Cria a categoria se não existir e, opcionalmente, define a classe que o Analytics usa: " +
				"necessidade, desejo, utilidade (conta da casa), basica (custo de vida básico) ou auto (palavra-chave no nome).",
			params: []param{
				{nome: "nome", tipo: "string", descricao: "nome curto, em minúsculas", obrigatorio: true},
				{nome: "classe", tipo: "string", enum: []string{"necessidade", "desejo", "utilidade", "basica", "auto"}, descricao: "classe no Analytics"},
			},
			rodar: definirCategoria,
		},
		{
			nome:      "criar_conta",
			escreve:   true,
			descricao: "Cria uma conta bancária (use quando o extrato for de um banco ainda não cadastrado; veja as existentes em saldo).",
			params: []param{
				{nome: "nome", tipo: "string", obrigatorio: true, descricao: "ex.: Nubank"},
				{nome: "banco", tipo: "string", descricao: "instituição"},
				{nome: "tipo", tipo: "string", enum: []string{"corrente", "poupanca", "investimento"}, descricao: "padrão corrente"},
				{nome: "saldo_inicial", tipo: "dinheiro", descricao: "saldo antes do primeiro lançamento importado (padrão 0)"},
			},
			rodar: criarConta,
		},
		{
			nome:    "criar_recorrencia",
			escreve: true,
			descricao: "Cria uma recorrência (conta fixa, salário) ou assinatura (assinatura=true: Netflix, Spotify...), " +
				"que passa a gerar os lançamentos futuros sozinha. Começa hoje ou depois — o histórico já vem do extrato. " +
				"vincular_lancamentos liga lançamentos já importados (ids) a ela, para não aparecerem como assinatura oculta.",
			params: []param{
				{nome: "tipo", tipo: "string", enum: []string{"pagar", "receber"}, obrigatorio: true},
				{nome: "descricao", tipo: "string", obrigatorio: true},
				{nome: "valor", tipo: "dinheiro", obrigatorio: true},
				{nome: "dia", tipo: "integer", obrigatorio: true, descricao: "dia do mês do vencimento, 1 a 31"},
				{nome: "categoria", tipo: "string"},
				{nome: "conta", tipo: "integer"},
				{nome: "cartao", tipo: "integer", descricao: "cobrada no cartão (vai para a fatura)"},
				{nome: "assinatura", tipo: "boolean"},
				{nome: "intervalo", tipo: "string", enum: []string{"mensal", "anual"}, descricao: "padrão mensal"},
				{nome: "inicio", tipo: "string", descricao: "AAAA-MM-DD, hoje ou depois (padrão hoje)"},
				{nome: "vincular_lancamentos", tipo: "array", itens: map[string]any{"type": "integer"}, descricao: "ids de lançamentos já quitados desta cobrança"},
			},
			rodar: criarRecorrencia,
		},
	}
}

// --- ler_extrato ---

func lerExtrato(_ *Sessao, a argumentos) (string, error) {
	caminho := a.texto("caminho")
	if strings.HasPrefix(caminho, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			caminho = filepath.Join(home, caminho[2:])
		}
	}
	switch strings.ToLower(filepath.Ext(caminho)) {
	case ".csv", ".ofx", ".txt", ".tsv":
	default:
		return "", errArg("caminho", "só .csv, .ofx, .txt ou .tsv")
	}
	info, err := os.Stat(caminho)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Size() > maxExtrato {
		return "", errArg("caminho", "precisa ser um arquivo de até 2 MB")
	}
	bruto, err := os.ReadFile(caminho)
	if err != nil {
		return "", err
	}
	texto, codif := decodifica(bruto)
	desde, err := a.inteiro("desde_linha", 1, 1, math.MaxInt32)
	if err != nil {
		return "", err
	}
	qtd, err := a.inteiro("linhas", 200, 1, 500)
	if err != nil {
		return "", err
	}

	linhas := strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n")
	for len(linhas) > 0 && strings.TrimSpace(linhas[len(linhas)-1]) == "" {
		linhas = linhas[:len(linhas)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Arquivo: %s (%d linhas, %s)\n", caminho, len(linhas), codif)
	if len(linhas) > 0 {
		cab := linhas[0]
		sep := ","
		switch {
		case strings.Count(cab, ";") > strings.Count(cab, ","):
			sep = ";"
		case strings.Count(cab, "\t") > strings.Count(cab, ","):
			sep = "tab"
		}
		fmt.Fprintf(&b, "Separador provável: %s\n", sep)
	}
	fim := desde - 1 + qtd
	if fim > len(linhas) {
		fim = len(linhas)
	}
	fmt.Fprintf(&b, "\n--- linhas %d a %d ---\n", desde, fim)
	for i := desde - 1; i < fim; i++ {
		fmt.Fprintf(&b, "%4d| %s\n", i+1, linhas[i])
	}
	if fim < len(linhas) {
		fmt.Fprintf(&b, "... mais %d linhas (use desde_linha=%d)\n", len(linhas)-fim, fim+1)
	}

	movs, err := app.InterpretaExtrato(caminho, texto)
	fmt.Fprintln(&b, "\n--- interpretação automática (heurística: confira as colunas) ---")
	switch {
	case err != nil:
		fmt.Fprintf(&b, "não reconhecida: %v\n", err)
	case len(movs) == 0:
		fmt.Fprintln(&b, "nenhum movimento reconhecido automaticamente; interprete as linhas acima")
	default:
		var ent, sai int64
		for _, m := range movs {
			if m.Valor >= 0 {
				ent += m.Valor
			} else {
				sai -= m.Valor
			}
		}
		fmt.Fprintf(&b, "%d movimento(s): entradas %s, saídas %s\n", len(movs), money.Format(ent), money.Format(sai))
		for i, m := range movs {
			if i == 10 {
				fmt.Fprintf(&b, "...\n")
				break
			}
			fmt.Fprintf(&b, "  %s  %-40s %s\n", m.Data, truncar(m.Descricao, 40), money.Format(m.Valor))
		}
	}
	return b.String(), nil
}

// decodifica devolve o texto em UTF-8: arquivos de banco brasileiros muitas
// vezes vêm em Latin-1/Windows-1252, que não é UTF-8 válido.
func decodifica(b []byte) (string, string) {
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b), "UTF-8"
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r), "Latin-1 convertido para UTF-8"
}

func truncar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// --- importar_lancamentos ---

// item é um lançamento já validado, pronto para a prévia ou para gravar.
type item struct {
	app.LancamentoParams
	destino   string // rótulo do destino, para a prévia
	duplicado bool
	categNova bool
}

func importarLancamentos(s *Sessao, a argumentos) (string, error) {
	brutos, _ := a["lancamentos"].([]any)
	if len(brutos) == 0 {
		return "", errArg("lancamentos", "lista vazia")
	}
	if len(brutos) > maxLancamentos {
		return "", errArg("lancamentos", fmt.Sprintf("no máximo %d por chamada; divida em partes", maxLancamentos))
	}
	conn := s.escrita
	cats := map[string]bool{}
	for _, c := range app.ListaCategorias(conn) {
		cats[c] = true
	}
	destinos := map[string]string{} // "conta:1" -> "conta #1 Nubank"

	itens := make([]item, 0, len(brutos))
	for i, br := range brutos {
		obj, ok := br.(map[string]any)
		if !ok {
			return "", fmt.Errorf("lançamento %d: esperava um objeto", i+1)
		}
		it, err := validaItem(conn, argumentos(obj), destinos)
		if err != nil {
			return "", fmt.Errorf("lançamento %d (%v): %w", i+1, obj["descricao"], err)
		}
		it.categNova = !cats[it.Cat] && it.Cat != "geral"
		if it.duplicado, err = jaExiste(conn, it.LancamentoParams); err != nil {
			return "", err
		}
		itens = append(itens, it)
	}

	if !a.booleano("confirmar") {
		return previa(itens), nil
	}
	return grava(s, itens)
}

// validaItem confere um lançamento da lista e monta os parâmetros do app.
func validaItem(conn *sql.DB, o argumentos, destinos map[string]string) (item, error) {
	permitidos := map[string]bool{"data": true, "descricao": true, "valor": true, "tipo": true, "categoria": true,
		"conta": true, "carteira": true, "cartao": true, "quitado": true, "observacao": true}
	for k := range o {
		if !permitidos[k] {
			return item{}, fmt.Errorf("campo desconhecido %q", k)
		}
	}
	var it item
	data := o.texto("data")
	if t, err := time.Parse("2006-01-02", data); err != nil || t.Format("2006-01-02") != data {
		return item{}, errArg("data", "use AAAA-MM-DD")
	}
	it.Venc = data
	it.Desc = strings.Join(strings.Fields(o.texto("descricao")), " ")
	if it.Desc == "" || len([]rune(it.Desc)) > 200 {
		return item{}, errArg("descricao", "obrigatória, até 200 caracteres")
	}
	v, err := o.dinheiro("valor")
	if err != nil {
		return item{}, err
	}
	if it.Valor, err = money.Parse(v); err != nil || it.Valor <= 0 {
		return item{}, errArg("valor", "precisa ser positivo")
	}
	it.Tipo = o.texto("tipo")
	if it.Tipo != "pagar" && it.Tipo != "receber" {
		return item{}, errArg("tipo", "pagar ou receber")
	}
	it.Cat = strings.ToLower(strings.Join(strings.Fields(o.texto("categoria")), " "))
	if it.Cat == "" {
		it.Cat = "geral"
	}
	it.Obs = o.texto("observacao")

	ids := map[string]int64{}
	for _, campo := range []string{"conta", "carteira", "cartao"} {
		if _, ok := o[campo]; !ok {
			continue
		}
		f, ok := o[campo].(float64)
		if !ok || f != math.Trunc(f) || f < 1 {
			return item{}, errArg(campo, "id inteiro positivo")
		}
		ids[campo] = int64(f)
	}
	if len(ids) > 1 {
		return item{}, fmt.Errorf("informe só um destino: conta, carteira ou cartao")
	}
	it.ContaID, it.CartID, it.CartaoID = ids["conta"], ids["carteira"], ids["cartao"]
	it.Quitado = it.CartaoID == 0 // padrão: movimento de conta já aconteceu
	if q, ok := o["quitado"].(bool); ok {
		it.Quitado = q
	}
	for campo, id := range ids {
		chave := fmt.Sprintf("%s:%d", campo, id)
		if _, ok := destinos[chave]; !ok {
			tabela := map[string]string{"conta": "contas", "carteira": "carteiras", "cartao": "cartoes"}[campo]
			var nome string
			if err := conn.QueryRow(`SELECT nome FROM `+tabela+` WHERE id = ?`, id).Scan(&nome); err == sql.ErrNoRows {
				return item{}, fmt.Errorf("%s #%d não existe", campo, id)
			} else if err != nil {
				return item{}, err
			}
			destinos[chave] = fmt.Sprintf("%s #%d %s", map[string]string{"conta": "conta", "carteira": "carteira", "cartao": "cartão"}[campo], id, nome)
		}
		it.destino = destinos[chave]
	}
	if it.destino == "" {
		it.destino = "sem vínculo"
	}
	return it, nil
}

// jaExiste diz se um lançamento igual (tipo, descrição, valor e data — de
// vencimento ou de compra) já está no banco: reimportar um extrato que se
// sobrepõe ao anterior não duplica nada.
func jaExiste(conn *sql.DB, p app.LancamentoParams) (bool, error) {
	var n int
	err := conn.QueryRow(`
		SELECT COUNT(*) FROM lancamentos
		WHERE tipo = ? AND valor = ? AND lower(trim(descricao)) = lower(?)
		  AND (vencimento = ? OR data_compra = ?)`,
		p.Tipo, p.Valor, p.Desc, p.Venc, p.Venc).Scan(&n)
	return n > 0, err
}

// previa resume o que importar_lancamentos gravaria, sem gravar.
func previa(itens []item) string {
	var b strings.Builder
	var novos, dups []item
	for _, it := range itens {
		if it.duplicado {
			dups = append(dups, it)
		} else {
			novos = append(novos, it)
		}
	}
	var desp, rec int64
	porCat := map[string][2]int64{} // categoria -> {qtd, total de despesas}
	porDestino := map[string]int{}
	novasCats := map[string]bool{}
	for _, it := range novos {
		if it.Tipo == "pagar" {
			desp += it.Valor
			c := porCat[it.Cat]
			porCat[it.Cat] = [2]int64{c[0] + 1, c[1] + it.Valor}
		} else {
			rec += it.Valor
		}
		porDestino[it.destino]++
		if it.categNova {
			novasCats[it.Cat] = true
		}
	}

	fmt.Fprintln(&b, "PRÉVIA — nada foi gravado (repita com confirmar=true para gravar)")
	fmt.Fprintf(&b, "\n  Novos:       %d (despesas %s, receitas %s)\n", len(novos), money.Format(desp), money.Format(rec))
	fmt.Fprintf(&b, "  Duplicados:  %d (já existem; serão ignorados)\n", len(dups))
	if len(novasCats) > 0 {
		fmt.Fprintf(&b, "  Categorias novas: %s\n", strings.Join(chaves(novasCats), ", "))
	}
	var dest []string
	for d, n := range porDestino {
		dest = append(dest, fmt.Sprintf("%s (%d)", d, n))
	}
	sort.Strings(dest)
	if len(dest) > 0 {
		fmt.Fprintf(&b, "  Destino: %s\n", strings.Join(dest, ", "))
	}

	if len(porCat) > 0 {
		fmt.Fprintln(&b, "\nDespesas por categoria:")
		type par struct {
			cat      string
			qtd, tot int64
		}
		var ps []par
		for c, v := range porCat {
			ps = append(ps, par{c, v[0], v[1]})
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].tot > ps[j].tot })
		w := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
		for _, p := range ps {
			fmt.Fprintf(w, "  %s\t%d\t%s\n", p.cat, p.qtd, money.Format(p.tot))
		}
		w.Flush()
	}

	fmt.Fprintln(&b, "\nLançamentos novos:")
	w := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  DATA\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVALOR\tSTATUS\tDESTINO")
	for i, it := range novos {
		if i == 60 {
			fmt.Fprintf(w, "  ... e mais %d\n", len(novos)-60)
			break
		}
		status := "pendente"
		if it.Quitado {
			status = "quitado"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", it.Venc, it.Tipo, truncar(it.Desc, 32), it.Cat,
			money.Format(it.Valor), status, it.destino)
	}
	w.Flush()
	if len(dups) > 0 {
		fmt.Fprintln(&b, "\nDuplicados ignorados:")
		for i, it := range dups {
			if i == 15 {
				fmt.Fprintf(&b, "  ... e mais %d\n", len(dups)-15)
				break
			}
			fmt.Fprintf(&b, "  %s  %s  %s\n", it.Venc, truncar(it.Desc, 40), money.Format(it.Valor))
		}
	}
	return b.String()
}

func chaves(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// grava cria os lançamentos não duplicados num lote novo, depois do snapshot.
func grava(s *Sessao, itens []item) (string, error) {
	var novos []item
	for _, it := range itens {
		if !it.duplicado {
			novos = append(novos, it)
		}
	}
	if len(novos) == 0 {
		return "Nada a gravar: todos os lançamentos já existem.", nil
	}
	if err := s.garanteSnapshot(); err != nil {
		return "", fmt.Errorf("backup antes de gravar: %w", err)
	}
	lote := "mcp-" + time.Now().Format("20060102-150405")
	marca := marcaLote(lote)
	criados, primeiro, ultimo := 0, int64(0), int64(0)
	for _, it := range novos {
		p := it.LancamentoParams
		p.Obs = strings.TrimSpace(p.Obs + " " + marca)
		// a saída do app (confirmações, aviso de categoria nova) não interessa
		// aqui: o resumo abaixo diz o que foi feito
		var lc []app.LancamentoCriado
		_, err := captura(func() error {
			var err error
			lc, _, _, err = app.CriarLancamentos(s.escrita, p)
			return err
		})
		if err != nil {
			return fmt.Sprintf("Lote %s interrompido após %d de %d lançamentos.\nDesfaça com desfazer_importacao lote=%s, corrija e repita.",
				lote, criados, len(novos), lote), err
		}
		for _, c := range lc {
			if primeiro == 0 {
				primeiro = c.ID
			}
			ultimo = c.ID
		}
		criados++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Lote %s gravado: %d lançamento(s) criado(s) (ids %d a %d).\n", lote, criados, primeiro, ultimo)
	if dups := len(itens) - len(novos); dups > 0 {
		fmt.Fprintf(&b, "%d duplicado(s) ignorado(s).\n", dups)
	}
	if s.snapshot != "" {
		fmt.Fprintf(&b, "Backup anterior à importação: %s\n", s.snapshot)
	}
	fmt.Fprintf(&b, "Para desfazer: desfazer_importacao lote=%s\n", lote)
	return b.String(), nil
}

// garanteSnapshot copia o banco local uma vez por sessão antes da primeira
// gravação, guardando os maxSnapshots mais recentes. Com banco remoto (modo
// cliente) não há o que copiar daqui: o servidor faz o backup diário.
func (s *Sessao) garanteSnapshot() error {
	if s.snapshot != "" || s.banco == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(s.banco), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	alvo := filepath.Join(dir, "mcp-antes-"+time.Now().Format("20060102-150405")+".db")
	if err := db.Snapshot(s.banco, alvo); err != nil {
		return err
	}
	s.snapshot = alvo
	if nomes, err := filepath.Glob(filepath.Join(dir, "mcp-antes-*.db")); err == nil && len(nomes) > maxSnapshots {
		sort.Strings(nomes)
		for _, n := range nomes[:len(nomes)-maxSnapshots] {
			os.Remove(n)
		}
	}
	return nil
}

// --- desfazer_importacao ---

func desfazerImportacao(s *Sessao, a argumentos) (string, error) {
	lote := a.texto("lote")
	if lote == "" {
		return listaLotes(s.escrita)
	}
	if !reLote.MatchString(lote) {
		return "", errArg("lote", "formato mcp-AAAAMMDD-HHMMSS")
	}
	if err := s.garanteSnapshot(); err != nil {
		return "", fmt.Errorf("backup antes de apagar: %w", err)
	}
	res, err := s.escrita.Exec(`DELETE FROM lancamentos WHERE observacao LIKE ?`, "%"+marcaLote(lote))
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", fmt.Errorf("nenhum lançamento do lote %s", lote)
	}
	return fmt.Sprintf("Lote %s desfeito: %d lançamento(s) apagado(s).", lote, n), nil
}

func listaLotes(conn *sql.DB) (string, error) {
	rows, err := conn.Query(`
		SELECT substr(observacao, instr(observacao, '[lote mcp-') + 6, 19) AS lote, COUNT(*),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN valor ELSE 0 END), 0),
		       MIN(vencimento), MAX(vencimento)
		FROM lancamentos WHERE observacao LIKE '%[lote mcp-%'
		GROUP BY lote ORDER BY lote DESC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "LOTE\tLANÇAMENTOS\tDESPESAS\tPERÍODO")
	achou := false
	for rows.Next() {
		var lote, de, ate string
		var n int
		var desp int64
		if err := rows.Scan(&lote, &n, &desp, &de, &ate); err != nil {
			return "", err
		}
		achou = true
		fmt.Fprintf(w, "%s\t%d\t%s\t%s a %s\n", lote, n, money.Format(desp), de, ate)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if !achou {
		return "Nenhum lote importado pelo assistente.", nil
	}
	w.Flush()
	return b.String(), nil
}

// --- definir_categoria, criar_conta, criar_recorrencia ---

func definirCategoria(s *Sessao, a argumentos) (string, error) {
	nome := strings.ToLower(strings.Join(strings.Fields(a.texto("nome")), " "))
	if nome == "" || nome == "geral" {
		return "", errArg("nome", "informe um nome (geral já é o padrão)")
	}
	conn := s.escrita
	res, err := conn.Exec(`INSERT OR IGNORE INTO categorias (nome) VALUES (?)`, nome)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if n, _ := res.RowsAffected(); n > 0 {
		fmt.Fprintf(&b, "Categoria %q criada.\n", nome)
	} else {
		fmt.Fprintf(&b, "Categoria %q já existia.\n", nome)
	}
	if classe := a.texto("classe"); classe != "" {
		var id int64
		if err := conn.QueryRow(`SELECT id FROM categorias WHERE nome = ?`, nome).Scan(&id); err != nil {
			return b.String(), err
		}
		out, err := captura(func() error {
			return app.Categoria(conn, []string{"editar", strconv.FormatInt(id, 10), "--classe=" + classe})
		})
		b.WriteString(out)
		if err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

func criarConta(s *Sessao, a argumentos) (string, error) {
	args := []string{"add", "--nome=" + a.texto("nome")}
	if v := a.texto("banco"); v != "" {
		args = append(args, "--banco="+v)
	}
	if v := a.texto("tipo"); v != "" {
		args = append(args, "--tipo="+v)
	}
	saldo, err := a.dinheiro("saldo_inicial")
	if err != nil {
		return "", err
	}
	if saldo != "" {
		args = append(args, "--saldo="+saldo)
	}
	if err := s.garanteSnapshot(); err != nil {
		return "", fmt.Errorf("backup antes de gravar: %w", err)
	}
	return captura(func() error { return app.Conta(s.escrita, args) })
}

func criarRecorrencia(s *Sessao, a argumentos) (string, error) {
	conn := s.escrita
	valor, err := a.dinheiro("valor")
	if err != nil {
		return "", err
	}
	dia, err := a.inteiro("dia", 0, 1, 31)
	if err != nil {
		return "", err
	}
	hoje := time.Now().Format("2006-01-02")
	inicio, err := a.data("inicio")
	if err != nil {
		return "", err
	}
	if inicio == "" {
		inicio = hoje
	}
	if inicio < hoje {
		return "", errArg("inicio", "hoje ou depois: o histórico já vem dos lançamentos importados")
	}
	desc := strings.Join(strings.Fields(a.texto("descricao")), " ")
	if desc == "" {
		return "", errArg("descricao", "obrigatória")
	}
	// --passados=manter: com início hoje ou depois não há ocorrências passadas,
	// mas o flag garante que a função nunca pergunte nada pelo stdin (que aqui
	// é o canal do protocolo)
	args := []string{"add", "--tipo=" + a.texto("tipo"), "--desc=" + desc, "--valor=" + valor,
		"--dia=" + strconv.Itoa(dia), "--inicio=" + inicio, "--passados=manter"}
	if c := strings.ToLower(a.texto("categoria")); c != "" {
		args = append(args, "--cat="+c)
	}
	for _, campo := range []string{"conta", "cartao"} {
		id, err := a.inteiro(campo, 0, 1, math.MaxInt32)
		if err != nil {
			return "", err
		}
		if id > 0 {
			args = append(args, "--"+campo+"="+strconv.Itoa(id))
		}
	}
	if a.booleano("assinatura") {
		args = append(args, "--assinatura")
	}
	if v := a.texto("intervalo"); v != "" {
		args = append(args, "--intervalo="+v)
	}

	var vinc []int64
	if lista, ok := a["vincular_lancamentos"].([]any); ok {
		for _, x := range lista {
			f, ok := x.(float64)
			if !ok || f != math.Trunc(f) || f < 1 {
				return "", errArg("vincular_lancamentos", "ids inteiros positivos")
			}
			vinc = append(vinc, int64(f))
		}
	}

	if err := s.garanteSnapshot(); err != nil {
		return "", fmt.Errorf("backup antes de gravar: %w", err)
	}
	out, err := captura(func() error { return app.Recorrencia(conn, args) })
	if err != nil {
		return out, err
	}
	if len(vinc) == 0 {
		return out, nil
	}
	var recID int64
	if err := conn.QueryRow(`SELECT id FROM recorrencias WHERE descricao = ? ORDER BY id DESC LIMIT 1`, desc).Scan(&recID); err != nil {
		return out, err
	}
	ligados := 0
	for _, id := range vinc {
		res, err := conn.Exec(`UPDATE lancamentos SET recorrencia_id = ?
			WHERE id = ? AND tipo = ? AND status = 'quitado' AND recorrencia_id IS NULL`, recID, id, a.texto("tipo"))
		if err != nil {
			return out, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			ligados++
		}
	}
	out += fmt.Sprintf("%d de %d lançamento(s) passados vinculados à recorrência #%d.\n", ligados, len(vinc), recID)
	return out, nil
}
