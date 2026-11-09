package app

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prisma/internal/money"
)

// A carteira de investimentos (modo pessoal). Cada investimento fica numa conta
// do tipo "investimento" — o dinheiro aplicado continua no saldo, pelo valor
// aplicado — e guarda os aportes, os resgates e o valor de mercado que você
// informa de tempos em tempos ("atualizar"). O rendimento é o valor de mercado
// menos o aplicado, e entra no patrimônio líquido.

// TiposInvestimento são os tipos aceitos.
var TiposInvestimento = []string{"cdb", "tesouro", "poupanca", "acao", "fii", "fundo", "cripto", "previdencia", "outro"}

// Aplicacao é uma aplicação com a posição de hoje.
type Aplicacao struct {
	ID          int64  `json:"id"`
	Nome        string `json:"nome"`
	Tipo        string `json:"tipo"`
	Instituicao string `json:"instituicao"`
	ContaID     int64  `json:"contaId"`
	Ativo       bool   `json:"ativo"`
	Aplicado    int64  `json:"aplicado"` // aportes − resgates
	Valor       int64  `json:"valor"`    // valor de mercado hoje
	Rendimento  int64  `json:"rendimento"`
	RendMes     int64  `json:"rendMes"`    // rendimento no mês corrente
	Atualizado  string `json:"atualizado"` // data do último valor informado ("" = nunca)
}

// Pct é o rendimento sobre o aplicado, em décimos de %.
func (i Aplicacao) Pct() float64 {
	if i.Aplicado <= 0 {
		return 0
	}
	return float64(i.Rendimento) * 100 / float64(i.Aplicado)
}

type movInv struct {
	tipo  string
	valor int64
	data  string
}

func movsInvestimento(conn *sql.DB, id int64) ([]movInv, error) {
	rows, err := conn.Query(`SELECT tipo, valor, data FROM investimentos_movs WHERE investimento_id = ? ORDER BY data, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []movInv
	for rows.Next() {
		var m movInv
		if err := rows.Scan(&m.tipo, &m.valor, &m.data); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// posicaoEm calcula, até a data (inclusive), o aplicado e o valor de mercado:
// o último valor informado mais os aportes e menos os resgates depois dele (sem
// valor informado, o mercado é o próprio aplicado).
func posicaoEm(movs []movInv, data string) (aplicado, valor int64, atualizado string) {
	base, temBase := int64(0), false
	var depois int64
	for _, m := range movs {
		if m.data > data {
			break
		}
		switch m.tipo {
		case "aporte":
			aplicado += m.valor
			depois += m.valor
		case "resgate":
			aplicado -= m.valor
			depois -= m.valor
		case "valor":
			base, temBase, depois, atualizado = m.valor, true, 0, m.data
		}
	}
	if !temBase {
		return aplicado, aplicado, ""
	}
	return aplicado, base + depois, atualizado
}

// ListaInvestimentos devolve as aplicações com a posição de hoje.
func ListaInvestimentos(conn *sql.DB, todos bool) ([]Aplicacao, error) {
	q := `SELECT id, nome, tipo, instituicao, COALESCE(conta_id, 0), status = 'ativo' FROM investimentos`
	if !todos {
		q += ` WHERE status = 'ativo'`
	}
	rows, err := conn.Query(q + ` ORDER BY nome, id`)
	if err != nil {
		return nil, err
	}
	var out []Aplicacao
	for rows.Next() {
		var i Aplicacao
		if err := rows.Scan(&i.ID, &i.Nome, &i.Tipo, &i.Instituicao, &i.ContaID, &i.Ativo); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, i)
	}
	rows.Close()
	agora := relogio()
	hoje := agora.Format("2006-01-02")
	fimAnt := time.Date(agora.Year(), agora.Month(), 0, 0, 0, 0, 0, agora.Location()).Format("2006-01-02")
	for k := range out {
		movs, err := movsInvestimento(conn, out[k].ID)
		if err != nil {
			return nil, err
		}
		i := &out[k]
		i.Aplicado, i.Valor, i.Atualizado = posicaoEm(movs, hoje)
		i.Rendimento = i.Valor - i.Aplicado
		apAnt, vAnt, _ := posicaoEm(movs, fimAnt)
		i.RendMes = i.Rendimento - (vAnt - apAnt)
	}
	if out == nil {
		out = []Aplicacao{}
	}
	return out, nil
}

// rendimentoEm é a soma do rendimento de todas as aplicações numa data.
func rendimentoEm(conn *sql.DB, data string) (int64, error) {
	rows, err := conn.Query(`SELECT id FROM investimentos`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var total int64
	for _, id := range ids {
		movs, err := movsInvestimento(conn, id)
		if err != nil {
			return 0, err
		}
		ap, v, _ := posicaoEm(movs, data)
		total += v - ap
	}
	return total, nil
}

// Patrimonio é o patrimônio líquido hoje e a evolução mês a mês.
type Patrimonio struct {
	Saldo       int64       `json:"saldo"`       // contas e carteiras (aplicações pelo valor aplicado)
	Investido   int64       `json:"investido"`   // aplicado nas aplicações ativas
	ValorInvest int64       `json:"valorInvest"` // valor de mercado delas
	Rendimento  int64       `json:"rendimento"`  // ainda não resgatado
	Faturas     int64       `json:"faturas"`     // compras no cartão ainda não pagas
	Dividas     int64       `json:"dividas"`     // saldo devedor das emergências
	Liquido     int64       `json:"liquido"`
	Evolucao    []ParRotulo `json:"evolucao"` // patrimônio no fim de cada um dos 12 meses
	Aplicacoes  []Aplicacao `json:"aplicacoes"`
}

// faturasEm soma as compras no cartão feitas até a data e ainda não pagas nela.
func faturasEm(conn *sql.DB, data string) (int64, error) {
	var v int64
	err := conn.QueryRow(`SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
		WHERE tipo = 'pagar' AND cartao_id IS NOT NULL AND COALESCE(data_compra, vencimento) <= ?
		  AND (status = 'pendente' OR quitado_em > ?)`, data, data).Scan(&v)
	return v, err
}

// CalculaPatrimonio monta o patrimônio líquido: saldo + rendimento das
// aplicações − faturas em aberto − dívidas.
func CalculaPatrimonio(conn *sql.DB) (Patrimonio, error) {
	var p Patrimonio
	var err error
	if p.Saldo, err = saldoTotal(conn); err != nil {
		return p, err
	}
	if p.Aplicacoes, err = ListaInvestimentos(conn, false); err != nil {
		return p, err
	}
	for _, i := range p.Aplicacoes {
		p.Investido += i.Aplicado
		p.ValorInvest += i.Valor
	}
	hoje := relogio().Format("2006-01-02")
	if p.Rendimento, err = rendimentoEm(conn, hoje); err != nil {
		return p, err
	}
	if p.Faturas, err = faturasEm(conn, hoje); err != nil {
		return p, err
	}
	if p.Dividas, err = dividasEstimadas(conn); err != nil {
		return p, err
	}
	p.Liquido = p.Saldo + p.Rendimento - p.Faturas - p.Dividas

	saldos, err := SaldoMensal(conn, 12)
	if err != nil {
		return p, err
	}
	for _, s := range saldos {
		t, _ := time.Parse("2006-01", s.Rotulo)
		fim := t.AddDate(0, 1, -1).Format("2006-01-02")
		if fim > hoje {
			fim = hoje
		}
		r, err := rendimentoEm(conn, fim)
		if err != nil {
			return p, err
		}
		f, err := faturasEm(conn, fim)
		if err != nil {
			return p, err
		}
		p.Evolucao = append(p.Evolucao, ParRotulo{Rotulo: s.Rotulo, Valor: s.Valor + r - f - p.Dividas})
	}
	return p, nil
}

// Investir trata `prisma investimento add|listar|aportar|resgatar|atualizar|remover`
// (no modo pessoal; no modo empresa, `investimento` é o lançamento da empresa).
func Investir(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar", "novo":
		return investAdd(conn, args[1:])
	case "listar", "ls":
		return investListar(conn, args[1:])
	case "aportar", "aplicar":
		return investMov(conn, "aporte", args[1:])
	case "resgatar":
		return investMov(conn, "resgate", args[1:])
	case "atualizar", "valor":
		return investAtualizar(conn, args[1:])
	case "remover", "rm":
		return investRemover(conn, args[1:])
	case "patrimonio", "patrimônio":
		return PatrimonioCmd(conn, nil)
	}
	return fmt.Errorf("subcomando inválido %q (use: add, listar, aportar, resgatar, atualizar, remover, patrimonio)", args[0])
}

func investAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("investimento add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome (ex.: CDB Nubank 110%) (obrigatório)")
	tipo := fs.String("tipo", "outro", strings.Join(TiposInvestimento, ", "))
	inst := fs.String("instituicao", "", "banco ou corretora")
	conta := fs.Int64("conta", 0, "conta do tipo investimento onde fica (padrão: cria uma)")
	valor := fs.String("valor", "", "já aplicado hoje (opcional)")
	de := fs.String("de", "", "de onde saiu o dinheiro do valor inicial: conta:ID ou carteira:ID (opcional)")
	data := fs.String("data", "hoje", "data do valor inicial")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nome) == "" {
		return errors.New("--nome é obrigatório")
	}
	*tipo = strings.ToLower(strings.TrimSpace(*tipo))
	ok := false
	for _, t := range TiposInvestimento {
		ok = ok || t == *tipo
	}
	if !ok {
		return fmt.Errorf("tipo inválido %q (use: %s)", *tipo, strings.Join(TiposInvestimento, ", "))
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	contaID := *conta
	if contaID == 0 {
		res, err := tx.Exec(`INSERT INTO contas (nome, banco, tipo) VALUES (?, ?, 'investimento')`, *nome, *inst)
		if err != nil {
			return err
		}
		contaID, _ = res.LastInsertId()
	} else {
		var tc string
		if err := tx.QueryRow(`SELECT tipo FROM contas WHERE id = ?`, contaID).Scan(&tc); err != nil {
			return fmt.Errorf("conta #%d não encontrada", contaID)
		}
	}
	res, err := tx.Exec(`INSERT INTO investimentos (nome, tipo, instituicao, conta_id) VALUES (?,?,?,?)`, *nome, *tipo, *inst, contaID)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Investimento #%d %q criado (na conta #%d).\n", id, *nome, contaID)
	if *valor != "" {
		mov := []string{fmt.Sprint(id), "--valor", *valor}
		if *de != "" {
			mov = append(mov, "--de", *de)
		}
		mov = append(mov, "--data", *data)
		return investMov(conn, "aporte", mov)
	}
	return nil
}

// investimentoDe lê o id e devolve nome e conta.
func investimentoDe(conn *sql.DB, s string) (id int64, nome string, conta int64, err error) {
	id, err = strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, "", 0, fmt.Errorf("id inválido: %q", s)
	}
	var c sql.NullInt64
	err = conn.QueryRow(`SELECT nome, conta_id FROM investimentos WHERE id = ?`, id).Scan(&nome, &c)
	if err == sql.ErrNoRows {
		return 0, "", 0, fmt.Errorf("investimento #%d não encontrado", id)
	}
	return id, nome, c.Int64, err
}

// investMov registra um aporte ou um resgate. Com --de (aporte) ou --para
// (resgate), vira uma transferência entre a conta informada e a da aplicação;
// sem, o dinheiro entra (ou sai) da conta da aplicação como saldo inicial —
// para registrar o que já estava aplicado antes do Prisma.
func investMov(conn *sql.DB, tipo string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma investimento %s <id> --valor X [--%s conta:ID] [--data]", map[string]string{"aporte": "aportar", "resgate": "resgatar"}[tipo],
			map[string]string{"aporte": "de", "resgate": "para"}[tipo])
	}
	id, nome, conta, err := investimentoDe(conn, args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("investimento "+tipo, flag.ContinueOnError)
	valor := fs.String("valor", "", "valor (obrigatório)")
	data := fs.String("data", "hoje", "data")
	outra := fs.String(map[string]string{"aporte": "de", "resgate": "para"}[tipo], "", "conta:ID ou carteira:ID")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	v, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if v <= 0 {
		return errors.New("o valor deve ser positivo")
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	var lugar local
	if *outra != "" {
		if lugar, err = parseLocal(conn, *outra); err != nil {
			return err
		}
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO investimentos_movs (investimento_id, tipo, valor, data) VALUES (?,?,?,?)`, id, tipo, v, d); err != nil {
		return err
	}
	if conta != 0 {
		switch {
		case *outra != "" && tipo == "aporte":
			_, err = tx.Exec(`INSERT INTO transferencias (valor, data, descricao, origem_tipo, origem_id, destino_tipo, destino_id)
				VALUES (?,?,?,?,?,'conta',?)`, v, d, "Aporte: "+nome, lugar.tipo, lugar.id, conta)
		case *outra != "":
			_, err = tx.Exec(`INSERT INTO transferencias (valor, data, descricao, origem_tipo, origem_id, destino_tipo, destino_id)
				VALUES (?,?,?,'conta',?,?,?)`, v, d, "Resgate: "+nome, conta, lugar.tipo, lugar.id)
		case tipo == "aporte":
			_, err = tx.Exec(`UPDATE contas SET saldo_inicial = saldo_inicial + ? WHERE id = ?`, v, conta)
		default:
			_, err = tx.Exec(`UPDATE contas SET saldo_inicial = saldo_inicial - ? WHERE id = ?`, v, conta)
		}
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	verbo := map[string]string{"aporte": "Aporte", "resgate": "Resgate"}[tipo]
	fmt.Printf("%s de %s em %q registrado (%s).\n", verbo, money.Format(v), nome, dataBR(d))
	return nil
}

func investAtualizar(conn *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("uso: prisma investimento atualizar <id> <valor de hoje> [--data]")
	}
	id, nome, _, err := investimentoDe(conn, args[0])
	if err != nil {
		return err
	}
	v, err := money.Parse(args[1])
	if err != nil {
		return err
	}
	if v < 0 {
		return errors.New("o valor não pode ser negativo")
	}
	fs := flag.NewFlagSet("investimento atualizar", flag.ContinueOnError)
	data := fs.String("data", "hoje", "data do valor")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`INSERT INTO investimentos_movs (investimento_id, tipo, valor, data) VALUES (?, 'valor', ?, ?)`, id, v, d); err != nil {
		return err
	}
	movs, err := movsInvestimento(conn, id)
	if err != nil {
		return err
	}
	ap, val, _ := posicaoEm(movs, d)
	fmt.Printf("%q vale %s em %s: rendimento de %s sobre %s aplicados.\n", nome, money.Format(val), dataBR(d), money.Format(val-ap), money.Format(ap))
	return nil
}

func investRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("uso: prisma investimento remover <id>")
	}
	id, nome, _, err := investimentoDe(conn, args[0])
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`DELETE FROM investimentos WHERE id = ?`, id); err != nil {
		return err
	}
	fmt.Printf("Investimento #%d %q removido (a conta dele continua; remova com prisma conta remover se quiser).\n", id, nome)
	return nil
}

func investListar(conn *sql.DB, args []string) error {
	todos := len(args) > 0 && args[0] == "--todos"
	is, err := ListaInvestimentos(conn, todos)
	if err != nil {
		return err
	}
	if len(is) == 0 {
		fmt.Println(`Nenhum investimento. Cadastre com: prisma investimento add --nome "CDB Nubank" --tipo cdb --valor 5.000,00 --de conta:1`)
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tTIPO\tINSTITUIÇÃO\tAPLICADO\tVALOR HOJE\tRENDIMENTO\tNO MÊS\tATUALIZADO")
	var ap, val int64
	for _, i := range is {
		at := "nunca"
		if i.Atualizado != "" {
			at = dataBR(i.Atualizado)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s (%s)\t%s\t%s\n", i.ID, i.Nome, i.Tipo, i.Instituicao,
			money.Format(i.Aplicado), money.Format(i.Valor), money.Format(i.Rendimento), pctBR(i.Pct()), money.Format(i.RendMes), at)
		ap += i.Aplicado
		val += i.Valor
	}
	if err := w.Flush(); err != nil {
		return err
	}
	pct := 0.0
	if ap > 0 {
		pct = float64(val-ap) * 100 / float64(ap)
	}
	fmt.Printf("\nTotal: %s aplicados, valendo %s (rendimento %s, %s).\n", money.Format(ap), money.Format(val), money.Format(val-ap), pctBR(pct))
	fmt.Println("Informe o valor de hoje de vez em quando: prisma investimento atualizar <id> <valor>")
	return nil
}

func pctBR(p float64) string {
	return strings.Replace(fmt.Sprintf("%+.1f%%", p), ".", ",", 1)
}

// PatrimonioCmd trata `prisma patrimonio`.
func PatrimonioCmd(conn *sql.DB, _ []string) error {
	p, err := CalculaPatrimonio(conn)
	if err != nil {
		return err
	}
	fmt.Println("PATRIMÔNIO LÍQUIDO")
	fmt.Println()
	fmt.Printf("  Contas e carteiras:        %s\n", money.Format(p.Saldo))
	if p.Investido > 0 || p.Rendimento != 0 {
		fmt.Printf("    (inclui %s aplicados em investimentos)\n", money.Format(p.Investido))
		fmt.Printf("  Rendimento dos investimentos: %s\n", money.Format(p.Rendimento))
	}
	fmt.Printf("  Faturas em aberto:         -%s\n", money.Format(p.Faturas))
	fmt.Printf("  Dívidas (emergências):     -%s\n", money.Format(p.Dividas))
	fmt.Printf("  Patrimônio líquido:        %s\n", money.Format(p.Liquido))
	if len(p.Evolucao) > 1 {
		fmt.Println()
		fmt.Println("Evolução (fim de cada mês):")
		var maior int64 = 1
		for _, e := range p.Evolucao {
			maior = max(maior, abs64(e.Valor))
		}
		for _, e := range p.Evolucao {
			fmt.Printf("  %s  %14s  %s\n", e.Rotulo, money.Format(e.Valor), strings.Repeat("█", int(abs64(e.Valor)*30/maior)))
		}
	}
	return nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
