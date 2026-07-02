package app

import (
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strings"

	"prisma/internal/money"
)

// Exportar grava os lançamentos em CSV (separador ';', decimal com vírgula —
// abre direto no Excel/LibreOffice pt-BR): `prisma exportar [--saida arq.csv] [--mes AAAA-MM]`.
func Exportar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("exportar", flag.ContinueOnError)
	saida := fs.String("saida", "prisma-lancamentos.csv", "arquivo de destino")
	mes := fs.String("mes", "", "exporta só um mês (AAAA-MM)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := `
		SELECT l.id, l.tipo, l.descricao, l.valor, l.categoria, l.vencimento, l.status,
		       COALESCE(l.quitado_em, ''), COALESCE(c.nome, ''), COALESCE(ca.nome, '')
		FROM lancamentos l
		LEFT JOIN contas c ON c.id = l.conta_id
		LEFT JOIN carteiras ca ON ca.id = l.carteira_id
		WHERE 1=1`
	var params []any
	if *mes != "" {
		p, err := periodoMes(*mes)
		if err != nil {
			return err
		}
		query += ` AND l.vencimento >= ? AND l.vencimento < ?`
		params = append(params, p.Inicio, p.Fim)
	}
	query += ` ORDER BY l.vencimento, l.id`

	rows, err := conn.Query(query, params...)
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(*saida)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Comma = ';'
	if err := w.Write([]string{
		"id", "tipo", "descricao", "valor", "categoria", "vencimento", "status", "quitado_em", "conta", "carteira",
	}); err != nil {
		return err
	}
	n := 0
	for rows.Next() {
		var id, valor int64
		var tipo, desc, cat, venc, status, quitadoEm, conta, carteira string
		if err := rows.Scan(&id, &tipo, &desc, &valor, &cat, &venc, &status, &quitadoEm, &conta, &carteira); err != nil {
			return err
		}
		if err := w.Write([]string{
			fmt.Sprint(id), tipo, desc, valorCSV(valor), cat, venc, status, quitadoEm, conta, carteira,
		}); err != nil {
			return err
		}
		n++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	fmt.Printf("%d lançamento(s) exportado(s) para %s.\n", n, *saida)
	return nil
}

// valorCSV formata centavos como "1234,56" (decimal com vírgula, sem milhar).
func valorCSV(c int64) string {
	sinal := ""
	if c < 0 {
		sinal, c = "-", -c
	}
	return fmt.Sprintf("%s%d,%02d", sinal, c/100, c%100)
}

// movimento é uma linha de extrato bancário importada.
type movimento struct {
	data  string // AAAA-MM-DD
	desc  string
	valor int64 // centavos, com sinal (negativo = saída)
}

// Importar lê um extrato bancário (.ofx ou .csv) e cria os lançamentos como
// quitados, vinculados à conta/carteira:
// `prisma importar --arquivo extrato.ofx --conta 1`.
// CSV esperado: colunas data, descrição e valor (negativo = pagamento).
func Importar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("importar", flag.ContinueOnError)
	arquivo := fs.String("arquivo", "", "extrato .ofx ou .csv (obrigatório)")
	contaID := fs.Int64("conta", 0, "id da conta de destino")
	cartID := fs.Int64("carteira", 0, "id da carteira de destino")
	cat := fs.String("cat", "importado", "categoria dos lançamentos criados")
	colValor := fs.Int("coluna-valor", 0, "coluna do valor no CSV, contando de 1 (padrão: o último campo monetário — atenção a extratos com coluna de saldo)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *arquivo == "" {
		return fmt.Errorf("--arquivo é obrigatório")
	}
	if (*contaID == 0) == (*cartID == 0) {
		return fmt.Errorf("informe --conta OU --carteira de destino")
	}
	var conta, carteira any
	if *contaID != 0 {
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	} else {
		if err := existe(conn, "carteiras", *cartID); err != nil {
			return err
		}
		carteira = *cartID
	}

	bruto, err := os.ReadFile(*arquivo)
	if err != nil {
		return err
	}
	var movs []movimento
	if strings.HasSuffix(strings.ToLower(*arquivo), ".ofx") {
		movs = parseOFX(string(bruto))
	} else {
		movs, err = parseCSVExtrato(string(bruto), *colValor)
		if err != nil {
			return err
		}
	}
	if len(movs) == 0 {
		return fmt.Errorf("nenhum movimento reconhecido em %s", *arquivo)
	}

	// importação inteira numa transação: ou todos os movimentos entram, ou
	// nenhum — um extrato não pode ficar pela metade se falhar no meio. O dedupe
	// roda no mesmo tx para enxergar um estado consistente.
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	criados, duplicados := 0, 0
	for _, m := range movs {
		tipo, valor := "receber", m.valor
		if m.valor < 0 {
			tipo, valor = "pagar", -m.valor
		}
		if valor == 0 {
			continue
		}
		// dedupe: mesmo dia, mesma descrição e mesmo valor já importados
		var n int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM lancamentos
			WHERE tipo = ? AND descricao = ? AND valor = ? AND vencimento = ? AND status = 'quitado'`,
			tipo, m.desc, valor, m.data).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			duplicados++
			continue
		}
		_, err := tx.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, carteira_id)
			VALUES (?,?,?,?,?,'quitado',?,?,?)`,
			tipo, m.desc, valor, strings.ToLower(*cat), m.data, m.data, conta, carteira,
		)
		if err != nil {
			return err
		}
		criados++
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("%d movimento(s) importado(s)", criados)
	if duplicados > 0 {
		fmt.Printf(", %d ignorado(s) como duplicado(s)", duplicados)
	}
	fmt.Println(".")
	return nil
}

// parseOFX extrai as transações (<STMTTRN>) de um extrato OFX. O formato é
// SGML: as tags de campo podem não ter fechamento, então lê linha a linha.
func parseOFX(s string) []movimento {
	var movs []movimento
	var atual *movimento
	campo := func(linha, tag string) (string, bool) {
		i := strings.Index(strings.ToUpper(linha), "<"+tag+">")
		if i < 0 {
			return "", false
		}
		v := linha[i+len(tag)+2:]
		if j := strings.Index(v, "<"); j >= 0 {
			v = v[:j]
		}
		return strings.TrimSpace(v), true
	}
	for _, linha := range strings.Split(s, "\n") {
		up := strings.ToUpper(linha)
		switch {
		case strings.Contains(up, "<STMTTRN>"):
			atual = &movimento{}
		case strings.Contains(up, "</STMTTRN>"):
			if atual != nil && atual.data != "" && atual.valor != 0 {
				if atual.desc == "" {
					atual.desc = "Movimento importado"
				}
				movs = append(movs, *atual)
			}
			atual = nil
		case atual != nil:
			if v, ok := campo(linha, "DTPOSTED"); ok && len(v) >= 8 {
				atual.data = v[:4] + "-" + v[4:6] + "-" + v[6:8]
			}
			if v, ok := campo(linha, "TRNAMT"); ok {
				if c, err := parseValorOFX(v); err == nil {
					atual.valor = c
				}
			}
			if v, ok := campo(linha, "MEMO"); ok && v != "" {
				atual.desc = v
			} else if v, ok := campo(linha, "NAME"); ok && atual.desc == "" {
				atual.desc = v
			}
		}
	}
	return movs
}

// parseValorOFX interpreta o TRNAMT de um extrato OFX: o separador que aparecer
// (ponto ou vírgula) é SEMPRE decimal. Não dá para reusar o money.Parse aqui —
// nele "1.200" é milhar pt-BR (R$ 1.200,00), mas num OFX "1.200" quer dizer
// 1 real e 20 centavos.
func parseValorOFX(s string) (int64, error) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	if s == "" {
		return 0, fmt.Errorf("valor OFX vazio")
	}
	inteiro, decimal := s, ""
	if i := strings.IndexAny(s, ".,"); i >= 0 {
		inteiro, decimal = s[:i], s[i+1:]
	}
	if inteiro == "" {
		inteiro = "0"
	}
	// normaliza para 2 casas: completa com zeros ou corta o excedente
	// (arredondando pela 3ª casa)
	arredonda := false
	switch {
	case len(decimal) > 2:
		arredonda = decimal[2] >= '5'
		decimal = decimal[:2]
	default:
		for len(decimal) < 2 {
			decimal += "0"
		}
	}
	var c int64
	for _, r := range inteiro + decimal {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("valor OFX inválido: %q", s)
		}
		c = c*10 + int64(r-'0')
		if c > 1_000_000_000_000 { // mesmo teto do money.Parse (10 bi de reais)
			return 0, fmt.Errorf("valor OFX %q grande demais", s)
		}
	}
	if arredonda {
		c++
	}
	if neg {
		c = -c
	}
	return c, nil
}

// parseCSVExtrato lê um CSV de banco: detecta o separador (';' ou ','), acha a
// coluna de data e usa como valor a coluna colValor (contada de 1) ou, sem ela,
// o último campo monetário; o resto vira descrição. Linhas sem data
// (cabeçalhos) são ignoradas.
func parseCSVExtrato(s string, colValor int) ([]movimento, error) {
	sep := ','
	if i := strings.IndexByte(s, '\n'); i > 0 && strings.Count(s[:i], ";") > 0 {
		sep = ';'
	}
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = sep
	r.FieldsPerRecord = -1
	registros, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV inválido: %w", err)
	}
	var movs []movimento
	for _, campos := range registros {
		var m movimento
		idxData := -1
		for i, c := range campos {
			if d, err := parseData(strings.TrimSpace(c)); err == nil && strings.TrimSpace(c) != "" {
				m.data, idxData = d, i
				break
			}
		}
		if idxData < 0 {
			continue // sem data: cabeçalho ou linha inválida
		}
		idxValor := -1
		if colValor > 0 {
			// coluna fixada pelo usuário (--coluna-valor): não adivinha
			if colValor > len(campos) {
				continue
			}
			v, err := money.Parse(campos[colValor-1])
			if err != nil {
				continue // linha sem valor na coluna indicada (ex.: cabeçalho)
			}
			m.valor, idxValor = v, colValor-1
		} else {
			for i := len(campos) - 1; i >= 0; i-- {
				if i == idxData {
					continue
				}
				if v, err := money.Parse(campos[i]); err == nil {
					m.valor, idxValor = v, i
					break
				}
			}
		}
		if idxValor < 0 {
			continue
		}
		var partes []string
		for i, c := range campos {
			if i != idxData && i != idxValor && strings.TrimSpace(c) != "" {
				partes = append(partes, strings.TrimSpace(c))
			}
		}
		m.desc = strings.Join(partes, " - ")
		if m.desc == "" {
			m.desc = "Movimento importado"
		}
		movs = append(movs, m)
	}
	return movs, nil
}
