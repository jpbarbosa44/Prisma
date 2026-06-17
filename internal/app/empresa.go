package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prisma/internal/money"
)

// Módulo empresa (prisma --empresa): sócios com participação própria,
// aportes de capital, imposto, investimento e distribuição de lucro. Roda
// sobre o mesmo motor de lançamentos/categorias do resto do Prisma — só
// acrescenta tabelas pequenas (socios, aportes_capital, distribuicoes_lucro,
// distribuicao_socios) e comandos dedicados, pra não depender do usuário
// lembrar de marcar a categoria certa na mão.

// Socio trata `prisma socio add|listar|editar|remover`.
func Socio(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return socioAdd(conn, args[1:])
	case "listar", "ls":
		return socioListar(conn)
	case "editar":
		return socioEditar(conn, args[1:])
	case "remover", "rm":
		return socioRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, editar, remover)", args[0])
	}
}

func socioAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("socio add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome do sócio (obrigatório)")
	participacao := fs.Float64("participacao", 0, "participação no lucro/capital, em % (obrigatório)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nome == "" {
		return fmt.Errorf("--nome é obrigatório")
	}
	if *participacao <= 0 || *participacao > 100 {
		return fmt.Errorf("--participacao deve estar entre 0 (exclusive) e 100")
	}
	res, err := conn.Exec(`INSERT INTO socios (nome, participacao) VALUES (?,?)`, *nome, *participacao)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Sócio #%d %q criado com %.1f%% de participação.\n", id, *nome, *participacao)
	avisaSomaParticipacao(conn)
	return nil
}

func socioEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma socio editar <id> [--nome] [--participacao]")
	}
	id := args[0]
	fs := flag.NewFlagSet("socio editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	participacao := fs.Float64("participacao", 0, "nova participação, em %")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	if !informado["nome"] && !informado["participacao"] {
		return fmt.Errorf("nada para alterar: informe --nome e/ou --participacao")
	}

	var sets []string
	var params []any
	if informado["nome"] {
		if *nome == "" {
			return fmt.Errorf("o nome não pode ficar vazio")
		}
		sets, params = append(sets, "nome = ?"), append(params, *nome)
	}
	if informado["participacao"] {
		if *participacao <= 0 || *participacao > 100 {
			return fmt.Errorf("--participacao deve estar entre 0 (exclusive) e 100")
		}
		sets, params = append(sets, "participacao = ?"), append(params, *participacao)
	}
	params = append(params, id)
	res, err := conn.Exec(`UPDATE socios SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("sócio #%s não encontrado", id)
	}
	fmt.Printf("Sócio #%s atualizado.\n", id)
	avisaSomaParticipacao(conn)
	return nil
}

func socioListar(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT id, nome, participacao FROM socios ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type linha struct {
		id           int64
		nome         string
		participacao float64
	}
	var socios []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nome, &l.participacao); err != nil {
			return err
		}
		socios = append(socios, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(socios) == 0 {
		fmt.Println("Nenhum sócio cadastrado. Use: prisma socio add --nome \"Você\" --participacao 60")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tPARTICIPAÇÃO")
	var soma float64
	for _, l := range socios {
		fmt.Fprintf(w, "%d\t%s\t%.1f%%\n", l.id, l.nome, l.participacao)
		soma += l.participacao
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if soma < 99.9 || soma > 100.1 {
		fmt.Printf("\nAviso: a soma das participações é %.1f%%, deveria ser 100%%.\n", soma)
	}
	return nil
}

// avisaSomaParticipacao avisa (sem bloquear) quando as participações dos
// sócios não somam 100%, o que deixaria distribuições de lucro injustas.
func avisaSomaParticipacao(conn *sql.DB) {
	var soma float64
	if err := conn.QueryRow(`SELECT COALESCE(SUM(participacao), 0) FROM socios`).Scan(&soma); err != nil {
		return
	}
	if soma != 0 && (soma < 99.9 || soma > 100.1) {
		fmt.Printf("Aviso: a soma das participações dos sócios é %.1f%%, deveria ser 100%%.\n", soma)
	}
}

// socioRemover apaga o sócio; falha com uma mensagem clara se houver aportes
// ou distribuições vinculados (a FK não tem ON DELETE CASCADE de propósito,
// pra não apagar histórico financeiro silenciosamente).
func socioRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma socio remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM socios WHERE id = ?`, args[0])
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return fmt.Errorf("não é possível remover: há aportes de capital ou distribuições de lucro vinculados a este sócio")
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("sócio #%s não encontrado", args[0])
	}
	fmt.Printf("Sócio #%s removido.\n", args[0])
	return nil
}

// Capital trata `prisma capital aportar|listar`.
func Capital(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "aportar":
		return capitalAportar(conn, args[1:])
	case "listar", "ls":
		return capitalListar(conn)
	default:
		return fmt.Errorf("subcomando inválido %q (use: aportar, listar)", args[0])
	}
}

// capitalAportar registra um aporte de capital social: cria a receita
// (categoria "capital", já quitada) e a linha em aportes_capital que liga o
// lançamento ao sócio, numa transação só.
func capitalAportar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("capital aportar", flag.ContinueOnError)
	socioID := fs.Int64("socio", 0, "id do sócio que aportou (obrigatório)")
	valor := fs.String("valor", "", "valor aportado (obrigatório)")
	contaID := fs.Int64("conta", 0, "id da conta que recebeu o aporte")
	data := fs.String("data", "hoje", "data do aporte")
	obs := fs.String("obs", "", "observação livre")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socioID == 0 {
		return fmt.Errorf("--socio é obrigatório")
	}
	if *valor == "" {
		return fmt.Errorf("--valor é obrigatório")
	}
	var nomeSocio string
	if err := conn.QueryRow(`SELECT nome FROM socios WHERE id = ?`, *socioID).Scan(&nomeSocio); err == sql.ErrNoRows {
		return fmt.Errorf("sócio #%d não encontrado", *socioID)
	} else if err != nil {
		return err
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if centavos <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	var conta any
	if *contaID != 0 {
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	}
	registraCategoria(conn, "capital")

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`
		INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, observacao)
		VALUES ('receber', ?, ?, 'capital', ?, 'quitado', ?, ?, ?)`,
		fmt.Sprintf("Aporte de capital: %s", nomeSocio), centavos, d, d, conta, *obs,
	)
	if err != nil {
		return err
	}
	lancID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO aportes_capital (socio_id, lancamento_id) VALUES (?,?)`, *socioID, lancID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Aporte de %s de %s registrado (lançamento #%d).\n", nomeSocio, money.Format(centavos), lancID)
	return nil
}

func capitalListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT s.id, s.nome, COALESCE(SUM(l.valor), 0), COUNT(a.id)
		FROM socios s
		LEFT JOIN aportes_capital a ON a.socio_id = s.id
		LEFT JOIN lancamentos l ON l.id = a.lancamento_id
		GROUP BY s.id, s.nome ORDER BY s.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type linha struct {
		id    int64
		nome  string
		total int64
		qtd   int
	}
	var linhas []linha
	var totalGeral int64
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.nome, &l.total, &l.qtd); err != nil {
			return err
		}
		linhas = append(linhas, l)
		totalGeral += l.total
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(linhas) == 0 {
		fmt.Println("Nenhum sócio cadastrado. Use: prisma socio add --nome \"Você\" --participacao 60")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tSÓCIO\tAPORTADO\tAPORTES\t% DO CAPITAL")
	for _, l := range linhas {
		pct := float64(0)
		if totalGeral > 0 {
			pct = float64(l.total) / float64(totalGeral) * 100
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%.1f%%\n", l.id, l.nome, money.Format(l.total), l.qtd, pct)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nCapital social total: %s\n", money.Format(totalGeral))
	return nil
}

// Imposto trata `prisma imposto add|listar` — um atalho fino sobre
// pagar/recorrencia que sempre fixa categoria "imposto", pra não depender de
// lembrar a categoria certa na hora de lançar.
func Imposto(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return impostoInvestimentoAdd(conn, "imposto", args[1:])
	case "listar", "ls":
		return Lancamentos(conn, append([]string{"--tipo", "pagar", "--cat", "imposto"}, args[1:]...))
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar)", args[0])
	}
}

// Investimento trata `prisma investimento add|listar`, mesmo esquema do
// Imposto, fixando a categoria "investimento".
func Investimento(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return impostoInvestimentoAdd(conn, "investimento", args[1:])
	case "listar", "ls":
		return Lancamentos(conn, append([]string{"--tipo", "pagar", "--cat", "investimento"}, args[1:]...))
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar)", args[0])
	}
}

// impostoInvestimentoAdd monta os args de `pagar add` (ou `recorrencia add`,
// se --recorrente) com a categoria fixa em cat, e delega pro comando já
// existente — reaproveita toda a validação de NovoLancamento/Recorrencia.
func impostoInvestimentoAdd(conn *sql.DB, cat string, args []string) error {
	fs := flag.NewFlagSet(cat+" add", flag.ContinueOnError)
	desc := fs.String("desc", "", "descrição (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório)")
	venc := fs.String("venc", "hoje", "vencimento (ou, com --recorrente, a data de início)")
	contaID := fs.Int64("conta", 0, "id da conta vinculada")
	quitado := fs.Bool("quitado", false, "já marca como pago")
	obs := fs.String("obs", "", "observação livre")
	parcelas := fs.Int("parcelas", 1, "divide o valor TOTAL em N parcelas mensais (não use com --recorrente)")
	repetir := fs.Int("repetir", 1, "repete o lançamento por N meses (não use com --recorrente)")
	recorrente := fs.Bool("recorrente", false, "lança todo mês (vira uma regra de recorrência)")
	dia := fs.Int("dia", 0, "dia do mês — obrigatório com --recorrente")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *desc == "" || *valor == "" {
		return fmt.Errorf("--desc e --valor são obrigatórios")
	}
	if *recorrente && cat != "imposto" {
		return fmt.Errorf("--recorrente só vale para imposto")
	}
	if *recorrente {
		if *dia < 1 || *dia > 31 {
			return fmt.Errorf("--dia é obrigatório (1 a 31) com --recorrente")
		}
		if *parcelas > 1 || *repetir > 1 {
			return fmt.Errorf("--parcelas/--repetir não combinam com --recorrente (a regra já se repete todo mês)")
		}
		rargs := []string{"add", "--tipo", "pagar", "--cat", cat, "--desc", *desc, "--valor", *valor, "--dia", strconv.Itoa(*dia)}
		if *venc != "" && *venc != "hoje" {
			rargs = append(rargs, "--inicio", *venc)
		}
		if *contaID != 0 {
			rargs = append(rargs, "--conta", strconv.FormatInt(*contaID, 10))
		}
		return Recorrencia(conn, rargs)
	}
	largs := []string{"add", "--cat", cat, "--desc", *desc, "--valor", *valor, "--venc", *venc}
	if *contaID != 0 {
		largs = append(largs, "--conta", strconv.FormatInt(*contaID, 10))
	}
	if *obs != "" {
		largs = append(largs, "--obs", *obs)
	}
	if *parcelas > 1 {
		largs = append(largs, "--parcelas", strconv.Itoa(*parcelas))
	}
	if *repetir > 1 {
		largs = append(largs, "--repetir", strconv.Itoa(*repetir))
	}
	if *quitado {
		largs = append(largs, "--quitado")
	}
	return NovoLancamento(conn, "pagar", largs)
}

// Lucro trata `prisma lucro calcular|distribuir|listar`.
func Lucro(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "calcular":
		return lucroCalcular(conn, args[1:])
	case "distribuir":
		return lucroDistribuir(conn, args[1:])
	case "listar", "ls":
		return lucroListar(conn)
	default:
		return fmt.Errorf("subcomando inválido %q (use: calcular, distribuir, listar)", args[0])
	}
}

// lucroCalcular soma receitas menos despesas quitadas no período, excluindo
// capital (não é receita operacional) e distribuição (já é saída de um lucro
// anterior, não despesa do período) — só informa, não grava nada.
func lucroCalcular(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("lucro calcular", flag.ContinueOnError)
	de := fs.String("de", "", "data inicial (padrão: 1º dia do mês atual)")
	ate := fs.String("ate", "", "data final (padrão: hoje)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	agora := time.Now()
	inicio := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	fim := agora.Format("2006-01-02")
	if *de != "" {
		d, err := parseData(*de)
		if err != nil {
			return err
		}
		inicio = d
	}
	if *ate != "" {
		d, err := parseData(*ate)
		if err != nil {
			return err
		}
		fim = d
	}

	var rec, desp int64
	err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
		FROM lancamentos
		WHERE status = 'quitado' AND categoria NOT IN ('capital', 'distribuicao')
		  AND COALESCE(data_compra, quitado_em) >= ? AND COALESCE(data_compra, quitado_em) <= ?`,
		inicio, fim).Scan(&rec, &desp)
	if err != nil {
		return err
	}
	fmt.Printf("LUCRO — %s a %s\n\n", dataBR(inicio), dataBR(fim))
	fmt.Printf("Receitas: %s | Despesas: %s | Lucro: %s\n",
		money.Format(rec), money.Format(desp), money.Format(rec-desp))
	fmt.Println("\n(capital aportado e distribuições de lucro não entram nesta conta)")
	return nil
}

// lucroAcumulado é o lucro de toda a vida da empresa (mesmo cálculo de
// lucroCalcular, sem filtro de data) menos o que já foi distribuído — o que
// ainda dá pra distribuir sem "comer" o capital. Usado só como aviso, não
// bloqueia nada (uma distribuição maior pode ser uma decisão válida).
func lucroAcumulado(conn *sql.DB) (int64, error) {
	var rec, desp, distribuido int64
	err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN ` + valEf("lancamentos") + ` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN ` + valEf("lancamentos") + ` ELSE 0 END), 0)
		FROM lancamentos WHERE status = 'quitado' AND categoria NOT IN ('capital', 'distribuicao')`,
	).Scan(&rec, &desp)
	if err != nil {
		return 0, err
	}
	if err := conn.QueryRow(`SELECT COALESCE(SUM(lucro_total), 0) FROM distribuicoes_lucro`).Scan(&distribuido); err != nil {
		return 0, err
	}
	return rec - desp - distribuido, nil
}

// lucroDistribuir reparte um valor entre os sócios conforme a participação de
// cada um, gravando a distribuição e um lançamento "pagar" por sócio (a saída
// de caixa da empresa), numa transação só. A soma das participações precisa
// bater 100% — senão a divisão fica injusta.
func lucroDistribuir(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("lucro distribuir", flag.ContinueOnError)
	valor := fs.String("valor", "", "valor total a distribuir (obrigatório)")
	data := fs.String("data", "hoje", "data da distribuição")
	obs := fs.String("obs", "", "observação livre")
	quitado := fs.Bool("quitado", false, "já marca as distribuições como pagas")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *valor == "" {
		return fmt.Errorf("--valor é obrigatório")
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if centavos <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}

	type socio struct {
		id           int64
		nome         string
		participacao float64
	}
	rows, err := conn.Query(`SELECT id, nome, participacao FROM socios ORDER BY id`)
	if err != nil {
		return err
	}
	var socios []socio
	for rows.Next() {
		var s socio
		if err := rows.Scan(&s.id, &s.nome, &s.participacao); err != nil {
			rows.Close()
			return err
		}
		socios = append(socios, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if len(socios) == 0 {
		return fmt.Errorf("nenhum sócio cadastrado (use: prisma socio add)")
	}
	var soma float64
	for _, s := range socios {
		soma += s.participacao
	}
	if soma < 99.9 || soma > 100.1 {
		return fmt.Errorf("a soma das participações dos sócios é %.1f%%, deveria ser 100%% (ajuste com `prisma socio editar`)", soma)
	}
	disponivel, err := lucroAcumulado(conn)
	if err != nil {
		return err
	}

	status, quitadoEm := "pendente", any(nil)
	if *quitado {
		status, quitadoEm = "quitado", d
	}
	registraCategoria(conn, "distribuicao")

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	resD, err := tx.Exec(`INSERT INTO distribuicoes_lucro (data, lucro_total, observacao) VALUES (?,?,?)`,
		d, centavos, *obs)
	if err != nil {
		return err
	}
	distID, _ := resD.LastInsertId()

	var distribuido int64
	for i, s := range socios {
		parte := int64(float64(centavos) * s.participacao / 100)
		if i == len(socios)-1 {
			parte = centavos - distribuido // a última absorve a sobra do arredondamento
		} else {
			distribuido += parte
		}
		res, err := tx.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em)
			VALUES ('pagar', ?, ?, 'distribuicao', ?, ?, ?)`,
			fmt.Sprintf("Distribuição de lucro: %s", s.nome), parte, d, status, quitadoEm,
		)
		if err != nil {
			return err
		}
		lancID, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO distribuicao_socios (distribuicao_id, socio_id, lancamento_id) VALUES (?,?,?)`,
			distID, s.id, lancID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Distribuição #%d de %s criada entre %d sócio(s).\n", distID, money.Format(centavos), len(socios))
	if disponivel < centavos {
		fmt.Printf("Aviso: o lucro acumulado disponível antes desta distribuição era %s — você distribuiu %s, %s além do que a empresa lucrou até agora.\n",
			money.Format(disponivel), money.Format(centavos), money.Format(centavos-disponivel))
	}
	return nil
}

func lucroListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT d.id, d.data, s.nome, l.valor, l.status
		FROM distribuicoes_lucro d
		JOIN distribuicao_socios ds ON ds.distribuicao_id = d.id
		JOIN socios s ON s.id = ds.socio_id
		JOIN lancamentos l ON l.id = ds.lancamento_id
		ORDER BY d.id DESC, s.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type linha struct {
		distID int64
		data   string
		nome   string
		valor  int64
		status string
	}
	var linhas []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.distID, &l.data, &l.nome, &l.valor, &l.status); err != nil {
			return err
		}
		linhas = append(linhas, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(linhas) == 0 {
		fmt.Println("Nenhuma distribuição de lucro registrada. Use: prisma lucro distribuir --valor 1000")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "DISTRIBUIÇÃO\tDATA\tSÓCIO\tVALOR\tSTATUS")
	for _, l := range linhas {
		fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\n", l.distID, dataBR(l.data), l.nome, money.Format(l.valor), l.status)
	}
	return w.Flush()
}
