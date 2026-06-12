package app

import (
	"database/sql"
	"flag"
	"fmt"
	"math"
	"time"

	"prisma/internal/money"
)

// Emergencia trata `prisma emergencia add|listar|plano|quitar|remover`.
// O modo de emergência cadastra uma dívida e monta um plano de ação
// mês a mês para quitá-la, considerando juros e o aporte mensal possível.
func Emergencia(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar":
		return emergenciaAdd(conn, args[1:])
	case "listar", "ls":
		return emergenciaListar(conn)
	case "plano":
		return emergenciaPlano(conn, args[1:])
	case "editar":
		return emergenciaEditar(conn, args[1:])
	case "quitar":
		return emergenciaQuitar(conn, args[1:])
	case "remover", "rm":
		return emergenciaRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: add, listar, plano, editar, quitar, remover)", args[0])
	}
}

// emergenciaEditar altera a dívida e revalida o plano (a combinação nova de
// valor, juros e aporte precisa continuar quitável):
// `prisma emergencia editar <id> [--desc] [--credor] [--valor] [--juros] [--aporte]`.
func emergenciaEditar(conn *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: prisma emergencia editar <id> [--desc] [--credor] [--valor] [--juros] [--aporte]")
	}
	id := args[0]
	fs := flag.NewFlagSet("emergencia editar", flag.ContinueOnError)
	desc := fs.String("desc", "", "nova descrição")
	credor := fs.String("credor", "", "novo credor")
	valor := fs.String("valor", "", "novo valor total")
	juros := fs.Float64("juros", -1, "novos juros em % ao mês")
	aporte := fs.String("aporte", "", "novo aporte mensal")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })

	// carrega o estado atual e aplica as mudanças por cima
	var atual struct {
		desc, credor string
		valor, ap    int64
		juros        float64
	}
	err := conn.QueryRow(
		`SELECT descricao, credor, valor_total, juros_mes, aporte_mensal FROM emergencias WHERE id = ?`, id,
	).Scan(&atual.desc, &atual.credor, &atual.valor, &atual.juros, &atual.ap)
	if err == sql.ErrNoRows {
		return fmt.Errorf("emergência #%s não encontrada", id)
	}
	if err != nil {
		return err
	}

	if informado["desc"] {
		if *desc == "" {
			return fmt.Errorf("a descrição não pode ficar vazia")
		}
		atual.desc = *desc
	}
	if informado["credor"] {
		atual.credor = *credor
	}
	if informado["valor"] {
		v, err := money.Parse(*valor)
		if err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("o valor deve ser positivo")
		}
		atual.valor = v
	}
	if informado["juros"] {
		if *juros < 0 || *juros > 100 {
			return fmt.Errorf("--juros deve estar entre 0 e 100 (%% ao mês)")
		}
		atual.juros = *juros
	}
	if informado["aporte"] {
		v, err := money.Parse(*aporte)
		if err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("o aporte deve ser positivo")
		}
		atual.ap = v
	}
	if len(informado) == 0 {
		return fmt.Errorf("nada para alterar: informe ao menos um campo")
	}

	jurosMes1 := int64(math.Round(float64(atual.valor) * atual.juros / 100))
	if atual.ap <= jurosMes1 {
		return fmt.Errorf(
			"com aporte de %s a dívida nunca seria quitada: só os juros do primeiro mês já são %s",
			money.Format(atual.ap), money.Format(jurosMes1))
	}

	_, err = conn.Exec(`
		UPDATE emergencias SET descricao = ?, credor = ?, valor_total = ?, juros_mes = ?, aporte_mensal = ?
		WHERE id = ?`,
		atual.desc, atual.credor, atual.valor, atual.juros, atual.ap, id,
	)
	if err != nil {
		return err
	}
	fmt.Printf("Emergência #%s atualizada.\n\n", id)
	return imprimePlano(atual.desc, atual.credor, atual.valor, atual.juros, atual.ap)
}

func emergenciaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("emergencia add", flag.ContinueOnError)
	desc := fs.String("desc", "", "descrição da dívida (obrigatório)")
	credor := fs.String("credor", "", "a quem se deve")
	valor := fs.String("valor", "", "valor total da dívida (obrigatório)")
	juros := fs.Float64("juros", 0, "juros em % ao mês (ex.: 12 para cartão de crédito)")
	aporte := fs.String("aporte", "", "quanto consegue pagar por mês (obrigatório)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *desc == "" || *valor == "" || *aporte == "" {
		return fmt.Errorf("--desc, --valor e --aporte são obrigatórios")
	}
	if *juros < 0 || *juros > 100 {
		return fmt.Errorf("--juros deve estar entre 0 e 100 (%% ao mês)")
	}
	vTotal, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	vAporte, err := money.Parse(*aporte)
	if err != nil {
		return err
	}
	if vTotal <= 0 || vAporte <= 0 {
		return fmt.Errorf("valor e aporte devem ser positivos")
	}

	// O aporte precisa superar os juros do primeiro mês, senão a dívida só cresce.
	jurosMes1 := int64(math.Round(float64(vTotal) * *juros / 100))
	if vAporte <= jurosMes1 {
		return fmt.Errorf(
			"com aporte de %s a dívida nunca será quitada: só os juros do primeiro mês já são %s.\n"+
				"Aumente o aporte para mais de %s ou negocie juros menores",
			money.Format(vAporte), money.Format(jurosMes1), money.Format(jurosMes1))
	}

	res, err := conn.Exec(
		`INSERT INTO emergencias (descricao, credor, valor_total, juros_mes, aporte_mensal) VALUES (?,?,?,?,?)`,
		*desc, *credor, vTotal, *juros, vAporte,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("Emergência #%d %q registrada.\n\n", id, *desc)
	return imprimePlano(*desc, *credor, vTotal, *juros, vAporte)
}

type parcela struct {
	mes              int
	juros, pago      int64
	saldoFinal       int64
}

// simulaPlano projeta a quitação mês a mês: aplica os juros sobre o saldo
// devedor e abate o aporte, até zerar (ou desistir após 50 anos). Se a
// dívida diverge (aporte menor que os juros), interrompe antes de estourar
// o int64 — o saldo final positivo sinaliza que ela nunca seria quitada.
func simulaPlano(valor int64, jurosPct float64, aporte int64) []parcela {
	const teto = int64(1) << 50 // ~R$ 11 tri: dívida divergente, não há por que seguir
	var plano []parcela
	saldo := valor
	for mes := 1; saldo > 0 && saldo < teto && mes <= 600; mes++ {
		juros := int64(math.Round(float64(saldo) * jurosPct / 100))
		saldo += juros
		pago := aporte
		if pago > saldo {
			pago = saldo
		}
		saldo -= pago
		plano = append(plano, parcela{mes, juros, pago, saldo})
	}
	return plano
}

func imprimePlano(desc, credor string, valor int64, jurosPct float64, aporte int64) error {
	plano := simulaPlano(valor, jurosPct, aporte)
	if len(plano) == 0 {
		return fmt.Errorf("não foi possível montar o plano")
	}
	ultimo := plano[len(plano)-1]
	if ultimo.saldoFinal > 0 {
		return fmt.Errorf("com esse aporte a dívida não é quitada nem em 50 anos; aumente o aporte")
	}

	fmt.Printf("PLANO DE AÇÃO — %s", desc)
	if credor != "" {
		fmt.Printf(" (credor: %s)", credor)
	}
	fmt.Printf("\nDívida: %s | Juros: %.2f%% a.m. | Aporte mensal: %s\n\n",
		money.Format(valor), jurosPct, money.Format(aporte))

	var totJuros, totPago int64
	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tQUANDO\tJUROS\tPAGAMENTO\tSALDO DEVEDOR")
	hoje := time.Now()
	for _, p := range plano {
		quando := hoje.AddDate(0, p.mes, 0).Format("01/2006")
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			p.mes, quando, money.Format(p.juros), money.Format(p.pago), money.Format(p.saldoFinal))
		totJuros += p.juros
		totPago += p.pago
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\nQuitação em %d meses. Total pago: %s (sendo %s de juros).\n",
		len(plano), money.Format(totPago), money.Format(totJuros))
	if jurosPct > 0 {
		// mostra o ganho de acelerar o pagamento em 20%
		acelerado := simulaPlano(valor, jurosPct, aporte+aporte/5)
		if len(acelerado) > 0 && acelerado[len(acelerado)-1].saldoFinal == 0 {
			var totAcel int64
			for _, p := range acelerado {
				totAcel += p.pago
			}
			fmt.Printf("Dica: aumentando o aporte em 20%% (%s/mês), você quita em %d meses e economiza %s em juros.\n",
				money.Format(aporte+aporte/5), len(acelerado), money.Format(totPago-totAcel))
		}
	}
	return nil
}

func emergenciaListar(conn *sql.DB) error {
	rows, err := conn.Query(
		`SELECT id, descricao, credor, valor_total, juros_mes, aporte_mensal, status FROM emergencias ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tDESCRIÇÃO\tCREDOR\tDÍVIDA\tJUROS a.m.\tAPORTE/MÊS\tQUITA EM\tSTATUS")
	achou := false
	for rows.Next() {
		achou = true
		var id, valor, aporte int64
		var desc, credor, status string
		var juros float64
		if err := rows.Scan(&id, &desc, &credor, &valor, &juros, &aporte, &status); err != nil {
			return err
		}
		meses := "-"
		if status == "ativa" {
			plano := simulaPlano(valor, juros, aporte)
			if len(plano) > 0 && plano[len(plano)-1].saldoFinal == 0 {
				meses = fmt.Sprintf("%d meses", len(plano))
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f%%\t%s\t%s\t%s\n",
			id, desc, credor, money.Format(valor), juros, money.Format(aporte), meses, status)
	}
	if !achou {
		fmt.Println("Nenhuma emergência registrada. Use: prisma emergencia add --desc \"...\" --valor ... --aporte ...")
		return nil
	}
	return w.Flush()
}

func emergenciaPlano(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma emergencia plano <id>")
	}
	var desc, credor string
	var valor, aporte int64
	var juros float64
	err := conn.QueryRow(
		`SELECT descricao, credor, valor_total, juros_mes, aporte_mensal FROM emergencias WHERE id = ?`,
		args[0],
	).Scan(&desc, &credor, &valor, &juros, &aporte)
	if err == sql.ErrNoRows {
		return fmt.Errorf("emergência #%s não encontrada", args[0])
	}
	if err != nil {
		return err
	}
	return imprimePlano(desc, credor, valor, juros, aporte)
}

func emergenciaQuitar(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma emergencia quitar <id>")
	}
	res, err := conn.Exec(`UPDATE emergencias SET status = 'quitada' WHERE id = ? AND status = 'ativa'`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("emergência #%s não encontrada ou já quitada", args[0])
	}
	fmt.Printf("Parabéns! Emergência #%s marcada como quitada. 🎉\n", args[0])
	return nil
}

func emergenciaRemover(conn *sql.DB, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: prisma emergencia remover <id>")
	}
	res, err := conn.Exec(`DELETE FROM emergencias WHERE id = ?`, args[0])
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("emergência #%s não encontrada", args[0])
	}
	fmt.Printf("Emergência #%s removida.\n", args[0])
	return nil
}
