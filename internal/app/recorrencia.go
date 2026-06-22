package app

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"prisma/internal/money"
)

// Recorrencia trata `prisma recorrencia add|listar|remover|gerar`.
// Uma recorrência é uma regra ("salário todo dia 1") que materializa
// lançamentos pendentes automaticamente, 3 meses à frente.
func Recorrencia(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return recorrenciaAdd(conn, args[1:])
	case "listar", "ls":
		return recorrenciaListar(conn, args[1:])
	case "editar":
		return recorrenciaEditar(conn, args[1:])
	case "remover", "rm":
		return recorrenciaRemover(conn, args[1:])
	case "gerar":
		n, err := GerarRecorrencias(conn)
		if err != nil {
			return err
		}
		fmt.Printf("%d lançamento(s) gerado(s).\n", n)
		return nil
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, remover, gerar)", args[0])
	}
}

func recorrenciaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("recorrencia add", flag.ContinueOnError)
	tipo := fs.String("tipo", "", "pagar ou receber (obrigatório)")
	desc := fs.String("desc", "", "descrição (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório)")
	dia := fs.Int("dia", 0, "dia do mês, 1 a 31 (obrigatório)")
	cat := fs.String("cat", "geral", "categoria")
	contaID := fs.Int64("conta", 0, "id da conta vinculada")
	cartID := fs.Int64("carteira", 0, "id da carteira vinculada")
	grupoID := fs.Int64("grupo", 0, "id do grupo que divide a despesa")
	cartaoID := fs.Int64("cartao", 0, "id do cartão (gera os lançamentos na fatura)")
	assinatura := fs.Bool("assinatura", false, "marca como assinatura (Netflix, Spotify...)")
	autoQuit := fs.Bool("auto-quitar", false, "quita os lançamentos gerados no vencimento")
	intervalo := fs.String("intervalo", "mensal", "frequência: mensal ou anual")
	inicio := fs.String("inicio", "hoje", "a partir de quando vale")
	fim := fs.String("fim", "", "até quando vale (vazio = sem fim)")
	passados := fs.String("passados", "", "ocorrências antes de hoje: quitar | manter (vazio pergunta)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tipo != "pagar" && *tipo != "receber" {
		return fmt.Errorf("--tipo deve ser pagar ou receber")
	}
	if *desc == "" || *valor == "" {
		return fmt.Errorf("--desc e --valor são obrigatórios")
	}
	if *dia < 1 || *dia > 31 {
		return fmt.Errorf("--dia deve estar entre 1 e 31")
	}
	*intervalo = strings.ToLower(strings.TrimSpace(*intervalo))
	if *intervalo == "" {
		*intervalo = "mensal"
	}
	if *intervalo != "mensal" && *intervalo != "anual" {
		return fmt.Errorf("--intervalo deve ser mensal ou anual")
	}
	if *contaID != 0 && *cartID != 0 {
		return fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
	}
	if *cartaoID != 0 {
		if *tipo != "pagar" {
			return fmt.Errorf("cartão só vale para despesas (--tipo pagar)")
		}
		if *contaID != 0 || *cartID != 0 {
			return fmt.Errorf("com --cartao não informe conta nem carteira (a fatura é paga pela conta do cartão)")
		}
		if err := existe(conn, "cartoes", *cartaoID); err != nil {
			return err
		}
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if centavos <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	dIni, err := parseData(*inicio)
	if err != nil {
		return err
	}
	var dFim any
	if *fim != "" {
		d, err := parseData(*fim)
		if err != nil {
			return err
		}
		if d < dIni {
			return fmt.Errorf("--fim não pode ser antes de --inicio")
		}
		dFim = d
	}
	var conta, carteira any
	if *contaID != 0 {
		if err := existe(conn, "contas", *contaID); err != nil {
			return err
		}
		conta = *contaID
	}
	if *cartID != 0 {
		if err := existe(conn, "carteiras", *cartID); err != nil {
			return err
		}
		carteira = *cartID
	}
	var cartao any
	if *cartaoID != 0 {
		cartao = *cartaoID
	}
	var grupo any
	if *grupoID != 0 {
		if err := existe(conn, "grupos", *grupoID); err != nil {
			return err
		}
		grupo = *grupoID
	}
	categoriaNova := avisaCategoriaNova(conn, *cat)
	registraCategoria(conn, *cat)

	res, err := conn.Exec(`
		INSERT INTO recorrencias (tipo, descricao, valor, categoria, dia, conta_id, carteira_id, inicio, fim, cartao_id, assinatura, grupo_id, auto_quitar, intervalo)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		*tipo, *desc, centavos, strings.ToLower(*cat), *dia, conta, carteira, dIni, dFim, cartao, *assinatura, grupo, *autoQuit, *intervalo,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	rotulo := "Recorrência"
	if *assinatura {
		rotulo = "Assinatura"
	}
	quando := fmt.Sprintf("todo dia %d", *dia)
	if *intervalo == "anual" {
		quando = fmt.Sprintf("todo dia %d de %s", *dia, mesPorExtenso(dIni))
	}
	fmt.Printf("%s #%d criada: %s %q de %s %s.\n",
		rotulo, id, *tipo, *desc, money.Format(centavos), quando)
	if categoriaNova {
		fmt.Printf("Aviso: primeira vez usando a categoria %q — confira se não é um erro de digitação.\n",
			strings.ToLower(*cat))
	}
	n, err := GerarRecorrencias(conn)
	if err != nil {
		return err
	}
	if n > 0 {
		fmt.Printf("%d lançamento(s) gerado(s).\n", n)
	}
	return quitarPassadosRec(conn, id, *passados)
}

// quitarPassadosRec trata as ocorrências da recorrência com vencimento antes de
// hoje: conforme `modo` (quitar | manter | vazio=pergunta), marca-as como
// quitadas (na própria data de vencimento) ou as deixa pendentes.
func quitarPassadosRec(conn *sql.DB, id int64, modo string) error {
	hoje, _ := parseData("hoje")
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente' AND vencimento < ?`,
		id, hoje).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	quitar := false
	switch strings.ToLower(strings.TrimSpace(modo)) {
	case "quitar", "s", "sim":
		quitar = true
	case "manter", "n", "nao", "não":
		quitar = false
	default:
		quitar = perguntaSN(fmt.Sprintf("Há %d ocorrência(s) anterior(es) a hoje. Marcar como quitadas?", n))
	}
	if !quitar {
		fmt.Printf("As %d ocorrência(s) anterior(es) ficaram como pendentes.\n", n)
		return nil
	}
	res, err := conn.Exec(`
		UPDATE lancamentos SET status = 'quitado', quitado_em = vencimento
		WHERE recorrencia_id = ? AND status = 'pendente' AND vencimento < ?`, id, hoje)
	if err != nil {
		return err
	}
	m, _ := res.RowsAffected()
	fmt.Printf("%d ocorrência(s) anterior(es) a hoje marcada(s) como quitada(s).\n", m)
	return nil
}

// perguntaSN faz uma pergunta sim/não no terminal; em entrada não-interativa
// (sem TTY, EOF) responde não.
func perguntaSN(pergunta string) bool {
	fmt.Print(pergunta + " (s/n): ")
	linha, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	r := strings.ToLower(strings.TrimSpace(linha))
	return r == "s" || r == "sim" || r == "y"
}

// recorrenciaEditar altera a regra E os lançamentos pendentes já gerados por
// ela (os quitados ficam intactos, são histórico):
// `prisma recorrencia editar <id> [--desc] [--valor] [--dia] [--cat] [--conta] [--carteira] [--fim]`.
// Use --fim nunca para remover a data de término; --conta/--carteira 0 desvincula.
func recorrenciaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma recorrencia editar <id> [--desc] [--valor] [--dia] [--cat] [--conta] [--carteira] [--fim]")
	}
	id := args[0]
	fs := flag.NewFlagSet("recorrencia editar", flag.ContinueOnError)
	desc := fs.String("desc", "", "nova descrição")
	valor := fs.String("valor", "", "novo valor")
	dia := fs.Int("dia", 0, "novo dia do mês")
	cat := fs.String("cat", "", "nova categoria")
	contaID := fs.Int64("conta", -1, "vincular à conta (0 desvincula)")
	cartID := fs.Int64("carteira", -1, "vincular à carteira (0 desvincula)")
	grupoID := fs.Int64("grupo", -1, "vincular ao grupo que divide (0 desvincula)")
	cartaoID := fs.Int64("cartao", -1, "vincular ao cartão, na fatura (0 desvincula)")
	assinatura := fs.String("assinatura", "", "marca como assinatura: sim | nao")
	autoQuit := fs.String("auto-quitar", "", "quita os gerados no vencimento: sim | nao")
	fim := fs.String("fim", "", "nova data de término (ou \"nunca\" para remover)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	if len(informado) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}

	// carrega a regra atual e aplica as mudanças por cima
	var r struct {
		tipo, desc, cat, inicio string
		valor                   int64
		dia                     int
		conta, carteira         sql.NullInt64
		fim                     sql.NullString
		cartao, grupo           sql.NullInt64
		assinatura, autoQuitar  bool
	}
	err := conn.QueryRow(`
		SELECT tipo, descricao, valor, categoria, dia, conta_id, carteira_id, inicio, fim, cartao_id, assinatura, grupo_id, auto_quitar
		FROM recorrencias WHERE id = ?`, id,
	).Scan(&r.tipo, &r.desc, &r.valor, &r.cat, &r.dia, &r.conta, &r.carteira, &r.inicio, &r.fim, &r.cartao, &r.assinatura, &r.grupo, &r.autoQuitar)
	if err == sql.ErrNoRows {
		return fmt.Errorf("recorrência #%s não encontrada", id)
	}
	if err != nil {
		return err
	}

	if informado["desc"] {
		if *desc == "" {
			return fmt.Errorf("a descrição não pode ficar vazia")
		}
		r.desc = *desc
	}
	if informado["valor"] {
		v, err := money.Parse(*valor)
		if err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("o valor deve ser positivo")
		}
		r.valor = v
	}
	if informado["dia"] {
		if *dia < 1 || *dia > 31 {
			return fmt.Errorf("--dia deve estar entre 1 e 31")
		}
		r.dia = *dia
	}
	if informado["cat"] {
		if *cat == "" {
			return fmt.Errorf("a categoria não pode ficar vazia")
		}
		r.cat = strings.ToLower(*cat)
		registraCategoria(conn, r.cat)
	}
	if informado["conta"] {
		if *contaID > 0 {
			if err := existe(conn, "contas", *contaID); err != nil {
				return err
			}
			r.conta = sql.NullInt64{Int64: *contaID, Valid: true}
			r.carteira = sql.NullInt64{}
			r.cartao = sql.NullInt64{} // conta avulsa tira o cartão
		} else {
			r.conta = sql.NullInt64{}
		}
	}
	if informado["carteira"] {
		if *cartID > 0 {
			if informado["conta"] && *contaID > 0 {
				return fmt.Errorf("vincule a uma conta OU a uma carteira, não ambas")
			}
			if err := existe(conn, "carteiras", *cartID); err != nil {
				return err
			}
			r.carteira = sql.NullInt64{Int64: *cartID, Valid: true}
			r.conta = sql.NullInt64{}
			r.cartao = sql.NullInt64{}
		} else {
			r.carteira = sql.NullInt64{}
		}
	}
	if informado["cartao"] {
		if *cartaoID > 0 {
			if r.tipo != "pagar" {
				return fmt.Errorf("cartão só vale para despesas (tipo pagar)")
			}
			if err := existe(conn, "cartoes", *cartaoID); err != nil {
				return err
			}
			// o cartão paga pela própria conta; tira conta/carteira avulsas
			r.cartao = sql.NullInt64{Int64: *cartaoID, Valid: true}
			r.conta = sql.NullInt64{}
			r.carteira = sql.NullInt64{}
		} else {
			r.cartao = sql.NullInt64{}
		}
	}
	if informado["grupo"] {
		if *grupoID > 0 {
			if err := existe(conn, "grupos", *grupoID); err != nil {
				return err
			}
			r.grupo = sql.NullInt64{Int64: *grupoID, Valid: true}
		} else {
			r.grupo = sql.NullInt64{}
		}
	}
	if informado["assinatura"] {
		switch strings.ToLower(strings.TrimSpace(*assinatura)) {
		case "sim", "s", "true", "1":
			r.assinatura = true
		case "nao", "não", "n", "false", "0":
			r.assinatura = false
		default:
			return fmt.Errorf("--assinatura deve ser sim ou nao")
		}
	}
	if informado["auto-quitar"] {
		switch strings.ToLower(strings.TrimSpace(*autoQuit)) {
		case "sim", "s", "true", "1":
			r.autoQuitar = true
		case "nao", "não", "n", "false", "0":
			r.autoQuitar = false
		default:
			return fmt.Errorf("--auto-quitar deve ser sim ou nao")
		}
	}
	if informado["fim"] {
		if strings.ToLower(*fim) == "nunca" {
			r.fim = sql.NullString{}
		} else {
			d, err := parseData(*fim)
			if err != nil {
				return err
			}
			if d < r.inicio {
				return fmt.Errorf("--fim não pode ser antes do início (%s)", dataBR(r.inicio))
			}
			r.fim = sql.NullString{String: d, Valid: true}
		}
	}

	var conta, carteira, cartao, grupo, dFim any
	if r.conta.Valid {
		conta = r.conta.Int64
	}
	if r.carteira.Valid {
		carteira = r.carteira.Int64
	}
	if r.cartao.Valid {
		cartao = r.cartao.Int64
	}
	if r.grupo.Valid {
		grupo = r.grupo.Int64
	}
	if r.fim.Valid {
		dFim = r.fim.String
	}
	_, err = conn.Exec(`
		UPDATE recorrencias SET descricao = ?, valor = ?, categoria = ?, dia = ?,
		       conta_id = ?, carteira_id = ?, fim = ?, cartao_id = ?, assinatura = ?, grupo_id = ?, auto_quitar = ? WHERE id = ?`,
		r.desc, r.valor, r.cat, r.dia, conta, carteira, dFim, cartao, r.assinatura, grupo, r.autoQuitar, id,
	)
	if err != nil {
		return err
	}

	// desc/valor/categoria/grupo/auto_quitar sempre propagam aos pendentes já gerados
	res, err := conn.Exec(`
		UPDATE lancamentos SET descricao = ?, valor = ?, categoria = ?, grupo_id = ?, auto_quitar = ?
		WHERE recorrencia_id = ? AND status = 'pendente'`,
		r.desc, r.valor, r.cat, grupo, r.autoQuitar, id,
	)
	if err != nil {
		return err
	}
	atualizados, _ := res.RowsAffected()

	// conta/carteira/cartão/vencimento dependem do destino (avulso x fatura) e do
	// dia: recalcula cada pendente a partir da data da compra
	if err := repropagaPendentes(conn, id, r.cartao, conta, carteira, r.dia); err != nil {
		return err
	}

	removidos := int64(0)
	if r.fim.Valid {
		// para cartão, o que importa para o término é a data da compra, não o
		// vencimento da fatura (que cai num mês seguinte)
		res, err := conn.Exec(`
			DELETE FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'
			       AND COALESCE(data_compra, vencimento) > ?`,
			id, r.fim.String)
		if err != nil {
			return err
		}
		removidos, _ = res.RowsAffected()
	}

	fmt.Printf("Recorrência #%s atualizada", id)
	if atualizados > 0 {
		fmt.Printf("; %d lançamento(s) pendente(s) ajustado(s)", atualizados)
	}
	if removidos > 0 {
		fmt.Printf("; %d removido(s) por ficarem após o término", removidos)
	}
	fmt.Println(".")
	return nil
}

// repropagaPendentes recalcula conta/carteira/cartão/vencimento/data_compra dos
// lançamentos pendentes de uma recorrência depois de uma edição. A data da
// compra (data_compra, ou o próprio vencimento quando avulso) é a âncora: o dia
// é reposicionado para `dia` e, havendo cartão, o vencimento vira o da fatura.
func repropagaPendentes(conn *sql.DB, recID any, cartao sql.NullInt64, conta, carteira any, dia int) error {
	var fech, vencDia int
	var contaCartao sql.NullInt64
	if cartao.Valid {
		var err error
		_, fech, vencDia, contaCartao, err = dadosCartao(conn, cartao.Int64)
		if err != nil {
			return err
		}
	}
	rows, err := conn.Query(
		`SELECT id, vencimento, COALESCE(data_compra, '') FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'`, recID)
	if err != nil {
		return err
	}
	type lin struct {
		id           int64
		venc, compra string
	}
	var lins []lin
	for rows.Next() {
		var l lin
		if err := rows.Scan(&l.id, &l.venc, &l.compra); err != nil {
			rows.Close()
			return err
		}
		lins = append(lins, l)
	}
	rows.Close()
	for _, l := range lins {
		base := l.compra // compra anterior; se avulso, ancora no vencimento
		if base == "" {
			base = l.venc
		}
		compra := diaNoMes(base[:7], dia)
		if cartao.Valid {
			compraT, _ := parseDataT(compra)
			_, vencFat := faturaDe(fech, vencDia, compraT)
			var c any
			if contaCartao.Valid {
				c = contaCartao.Int64
			}
			if _, err := conn.Exec(
				`UPDATE lancamentos SET cartao_id = ?, data_compra = ?, vencimento = ?, conta_id = ?, carteira_id = NULL WHERE id = ?`,
				cartao.Int64, compra, vencFat, c, l.id); err != nil {
				return err
			}
		} else {
			if _, err := conn.Exec(
				`UPDATE lancamentos SET cartao_id = NULL, data_compra = NULL, vencimento = ?, conta_id = ?, carteira_id = ? WHERE id = ?`,
				compra, conta, carteira, l.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// GerarRecorrencias materializa os lançamentos pendentes de todas as regras
// até 3 meses à frente. É chamada a cada execução do Prisma; idempotente.
func GerarRecorrencias(conn *sql.DB) (int, error) {
	rows, err := conn.Query(`
		SELECT r.id, r.tipo, r.descricao, r.valor, r.categoria, r.dia, r.conta_id, r.carteira_id,
		       r.inicio, COALESCE(r.fim, ''), r.ultima_ref,
		       r.cartao_id, c.dia_fechamento, c.dia_vencimento, c.conta_id, r.grupo_id, r.auto_quitar, r.intervalo
		FROM recorrencias r LEFT JOIN cartoes c ON c.id = r.cartao_id`)
	if err != nil {
		return 0, err
	}
	type regra struct {
		id, valor                    int64
		dia                          int
		tipo, desc, cat              string
		conta, carteira              sql.NullInt64
		inicio, fim, refUl           string
		cartao, cFech, cVenc, cConta sql.NullInt64
		grupo                        sql.NullInt64
		autoQuitar                   bool
		intervalo                    string
	}
	var regras []regra
	for rows.Next() {
		var r regra
		if err := rows.Scan(&r.id, &r.tipo, &r.desc, &r.valor, &r.cat, &r.dia,
			&r.conta, &r.carteira, &r.inicio, &r.fim, &r.refUl,
			&r.cartao, &r.cFech, &r.cVenc, &r.cConta, &r.grupo, &r.autoQuitar, &r.intervalo); err != nil {
			rows.Close()
			return 0, err
		}
		regras = append(regras, r)
	}
	rows.Close()

	horizonte := time.Now().AddDate(0, 3, 0).Format("2006-01")
	total := 0
	for _, r := range regras {
		ref := r.inicio[:7] // AAAA-MM do início
		if r.refUl != "" && r.refUl >= ref {
			ref = proximoMes(r.refUl)
		}
		for ref <= horizonte {
			// anual: só materializa no mês de aniversário (o mês do início)
			if r.intervalo == "anual" && ref[5:7] != r.inicio[5:7] {
				ref = proximoMes(ref)
				continue
			}
			venc := diaNoMes(ref, r.dia)
			// a vigência (início/fim) é medida pela data da ocorrência (a compra),
			// mesmo quando o vencimento depois vira o da fatura do cartão
			if venc >= r.inicio && (r.fim == "" || venc <= r.fim) {
				var conta, carteira, cartao, dataCompra, grupo any
				if r.conta.Valid {
					conta = r.conta.Int64
				}
				if r.carteira.Valid {
					carteira = r.carteira.Int64
				}
				if r.grupo.Valid {
					grupo = r.grupo.Int64
				}
				// cartão: a ocorrência cai numa fatura — a data vira a da compra,
				// o vencimento passa a ser o da fatura e quem paga é a conta do cartão
				if r.cartao.Valid {
					cartao = r.cartao.Int64
					compraT, _ := parseDataT(venc)
					_, vencFat := faturaDe(int(r.cFech.Int64), int(r.cVenc.Int64), compraT)
					dataCompra = venc
					venc = vencFat
					carteira = nil
					if r.cConta.Valid {
						conta = r.cConta.Int64
					} else {
						conta = nil
					}
				}
				_, err := conn.Exec(`
					INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, conta_id, carteira_id, recorrencia_id, cartao_id, data_compra, grupo_id, auto_quitar)
					VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
					r.tipo, r.desc, r.valor, r.cat, venc, conta, carteira, r.id, cartao, dataCompra, grupo, r.autoQuitar,
				)
				if err != nil {
					return total, err
				}
				total++
			}
			ref = proximoMes(ref)
		}
		if _, err := conn.Exec(`UPDATE recorrencias SET ultima_ref = ? WHERE id = ?`, horizonte, r.id); err != nil {
			return total, err
		}
	}
	return total, nil
}

// recorrenciasNoMes soma o valor efetivo das recorrências de um tipo
// (pagar/receber) que têm ocorrência no mês ref (AAAA-MM), respeitando o
// intervalo (mensal/anual) e a vigência início/fim. O valor já vem dividido
// pelo tamanho do grupo (a minha parte), igual ao que valEf faz nos
// lançamentos. É a base das projeções de longo prazo, que não podem depender da
// materialização (limitada a 3 meses à frente por GerarRecorrencias). Devolve
// também quantas recorrências entraram na conta.
func recorrenciasNoMes(conn *sql.DB, tipo, ref string) (int64, int, error) {
	rows, err := conn.Query(`
		SELECT r.valor, r.dia, r.inicio, COALESCE(r.fim, ''), r.intervalo,
		       max(1, COALESCE((SELECT COUNT(*) FROM grupo_pessoas gp WHERE gp.grupo_id = r.grupo_id), 1))
		FROM recorrencias r WHERE r.tipo = ?`, tipo)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var soma int64
	var qtd int
	for rows.Next() {
		var valor int64
		var dia, pessoas int
		var inicio, fim, intervalo string
		if err := rows.Scan(&valor, &dia, &inicio, &fim, &intervalo, &pessoas); err != nil {
			return 0, 0, err
		}
		// anual: só conta no mês de aniversário (o mês do início)
		if intervalo == "anual" && ref[5:7] != inicio[5:7] {
			continue
		}
		venc := diaNoMes(ref, dia)
		if venc >= inicio && (fim == "" || venc <= fim) {
			soma += valor / int64(pessoas)
			qtd++
		}
	}
	return soma, qtd, rows.Err()
}

// ocorrenciasRestantes conta quantas cobranças ainda faltam de uma recorrência
// com término definido: as ocorrências (dia de cada mês, de início a fim) que
// caem de hoje em diante. Retorna 0 quando já encerrou.
func ocorrenciasRestantes(inicio, fim string, dia int, intervalo string) int {
	if fim == "" {
		return -1 // sem fim
	}
	hoje, _ := parseData("hoje")
	ref := inicio[:7]
	if hoje[:7] > ref {
		ref = hoje[:7]
	}
	cont := 0
	for ref <= fim[:7] {
		// anual: só conta os meses de aniversário (o mês do início)
		if intervalo == "anual" && ref[5:7] != inicio[5:7] {
			ref = proximoMes(ref)
			continue
		}
		venc := diaNoMes(ref, dia)
		if venc >= inicio && venc <= fim && venc >= hoje {
			cont++
		}
		ref = proximoMes(ref)
	}
	return cont
}

// proximoMes avança uma referência AAAA-MM em um mês.
func proximoMes(ref string) string {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return ref
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// diaNoMes monta a data AAAA-MM-DD travando o dia no fim do mês (31 → 28/30).
func diaNoMes(ref string, dia int) string {
	t, err := time.Parse("2006-01", ref)
	if err != nil {
		return ref + "-01"
	}
	ultimo := t.AddDate(0, 1, -1).Day()
	if dia > ultimo {
		dia = ultimo
	}
	return time.Date(t.Year(), t.Month(), dia, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// recorrenciaListar aceita filtros: --tipo pagar|receber, --vigentes (só as que
// ainda valem hoje, escondendo as encerradas) e --assinaturas (só assinaturas).
func recorrenciaListar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("recorrencia listar", flag.ContinueOnError)
	tipo := fs.String("tipo", "", "filtra por tipo: pagar ou receber")
	vigentes := fs.Bool("vigentes", false, "esconde as recorrências já encerradas")
	soAssin := fs.Bool("assinaturas", false, "mostra só as assinaturas")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := `
		SELECT r.id, r.tipo, r.descricao, r.valor, r.categoria, r.dia, r.inicio, COALESCE(r.fim, ''),
		       COALESCE(c.nome, ''), r.assinatura, COALESCE(g.nome, ''), r.auto_quitar,
		       (SELECT COUNT(*) FROM grupo_pessoas gp WHERE gp.grupo_id = r.grupo_id), r.intervalo
		FROM recorrencias r
		LEFT JOIN cartoes c ON c.id = r.cartao_id
		LEFT JOIN grupos g ON g.id = r.grupo_id WHERE 1=1`
	var params []any
	if *tipo != "" {
		query += ` AND r.tipo = ?`
		params = append(params, *tipo)
	}
	if *soAssin {
		query += ` AND r.assinatura = 1`
	}
	if *vigentes {
		hoje, _ := parseData("hoje")
		query += ` AND (r.fim IS NULL OR r.fim >= ?)`
		params = append(params, hoje)
	}
	query += ` ORDER BY r.id`
	rows, err := conn.Query(query, params...)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tTIPO\tDESCRIÇÃO\tCATEGORIA\tVALOR\tDIA\tCARTÃO\tGRUPO\tVIGÊNCIA\tRESTANTES")
	achou := false
	for rows.Next() {
		achou = true
		var id, valor int64
		var dia, assin, autoQuit, pessoas int
		var tipo, desc, cat, ini, fim, cartao, grupo, intervalo string
		if err := rows.Scan(&id, &tipo, &desc, &valor, &cat, &dia, &ini, &fim, &cartao, &assin, &grupo, &autoQuit, &pessoas, &intervalo); err != nil {
			return err
		}
		if assin == 1 {
			desc += " (assinatura)"
		}
		if intervalo == "anual" {
			desc += " (anual)"
		}
		if autoQuit == 1 {
			desc += " ⏱"
		}
		// com grupo, o valor exibido é a sua parte (valor ÷ pessoas), como em pagar/receber
		grupoCol := ouTraco(grupo)
		if grupo != "" && pessoas > 0 {
			valor /= int64(pessoas)
			grupoCol = fmt.Sprintf("%s ÷%d", grupo, pessoas)
		}
		vig := "desde " + dataBR(ini)
		rest := "-"
		if fim != "" {
			vig = dataBR(ini) + " a " + dataBR(fim)
			rest = fmt.Sprintf("%d ocorrência(s)", ocorrenciasRestantes(ini, fim, dia, intervalo))
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			id, tipo, desc, cat, money.Format(valor), dia, ouTraco(cartao), grupoCol, vig, rest)
	}
	if !achou {
		fmt.Println("Nenhuma recorrência. Use: prisma recorrencia add --tipo receber --desc \"Salário\" --valor 5000 --dia 1")
		return nil
	}
	return w.Flush()
}

// recorrenciaRemover apaga a regra; com --limpar, apaga também os lançamentos
// pendentes que ela gerou (os quitados ficam, são histórico).
func recorrenciaRemover(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma recorrencia remover <id> [--limpar]")
	}
	id := args[0]
	fs := flag.NewFlagSet("recorrencia remover", flag.ContinueOnError)
	limpar := fs.Bool("limpar", false, "remove também os lançamentos pendentes gerados")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *limpar {
		res, err := conn.Exec(
			`DELETE FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			fmt.Printf("%d lançamento(s) pendente(s) removido(s).\n", n)
		}
	}
	res, err := conn.Exec(`DELETE FROM recorrencias WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("recorrência #%s não encontrada", id)
	}
	fmt.Printf("Recorrência #%s removida.\n", id)
	return nil
}
