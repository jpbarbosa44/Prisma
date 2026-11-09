package app

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"prisma/internal/money"
)

// diasInicio é a janela de "esta semana" da tela Início.
const diasInicio = 7

// FaturaInicio é a próxima fatura a pagar de um cartão: a mais antiga com
// compras pendentes. Fechada = o ciclo já fechou (o valor não muda mais).
type FaturaInicio struct {
	CartaoID   int64  `json:"cartaoId"`
	Nome       string `json:"nome"`
	Ref        string `json:"ref"`
	Vencimento string `json:"vencimento"`
	Aberto     int64  `json:"aberto"`
	Limite     int64  `json:"limite"`
	Fechada    bool   `json:"fechada"`
}

// PainelInicio reúne o que a tela Início mostra: a posição de hoje, o que
// vence nos próximos dias e as faturas. Vazio = banco recém-criado (sem
// contas, carteiras, cartões nem lançamentos), para a página dar as boas-vindas.
type PainelInicio struct {
	Hoje        string         `json:"hoje"`
	Mes         string         `json:"mes"` // AAAA-MM
	Saldo       int64          `json:"saldo"`
	PendPagar   int64          `json:"pendPagar"`   // pendente no mês
	PendReceber int64          `json:"pendReceber"` // pendente no mês
	Avisos      []Aviso        `json:"avisos"`
	Faturas     []FaturaInicio `json:"faturas"`
	Insights    []Insight      `json:"insights"`
	// Patrimonio é o patrimônio líquido, quando há investimentos cadastrados
	Patrimonio    int64 `json:"patrimonio"`
	TemPatrimonio bool  `json:"temPatrimonio"`
	Vazio         bool  `json:"vazio"`
}

// pendentesDoMes soma o pendente a pagar e a receber com vencimento no mês
// (AAAA-MM) — o recorte da tela Pagar/Receber e do Saldo.
func pendentesDoMes(conn *sql.DB, mes string) (pagar, receber int64, err error) {
	p, err := periodoMes(mes)
	if err != nil {
		return 0, 0, err
	}
	err = conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
		FROM lancamentos WHERE status = 'pendente' AND vencimento >= ? AND vencimento < ?`,
		p.Inicio, p.Fim).Scan(&pagar, &receber)
	return pagar, receber, err
}

// Inicio monta o painel da tela Início.
func Inicio(conn *sql.DB) (PainelInicio, error) {
	agora := time.Now()
	p := PainelInicio{Hoje: agora.Format("2006-01-02"), Mes: agora.Format("2006-01"), Avisos: []Aviso{}, Faturas: []FaturaInicio{}, Insights: []Insight{}}

	var n int
	if err := conn.QueryRow(`SELECT (SELECT COUNT(*) FROM contas) + (SELECT COUNT(*) FROM carteiras)
		+ (SELECT COUNT(*) FROM cartoes) + (SELECT COUNT(*) FROM lancamentos)`).Scan(&n); err != nil {
		return p, err
	}
	p.Vazio = n == 0
	if p.Vazio {
		return p, nil
	}

	var err error
	if p.Saldo, err = saldoTotal(conn); err != nil {
		return p, err
	}
	if p.PendPagar, p.PendReceber, err = pendentesDoMes(conn, p.Mes); err != nil {
		return p, err
	}
	avisos, err := Vencimentos(conn, agora, diasInicio)
	if err != nil {
		return p, err
	}
	p.Avisos = append(p.Avisos, avisos...)

	rows, err := conn.Query(`
		SELECT c.id, c.nome, c.limite, c.dia_fechamento, c.dia_vencimento, MIN(l.vencimento)
		FROM cartoes c JOIN lancamentos l ON l.cartao_id = c.id AND l.status = 'pendente' AND l.tipo = 'pagar'
		GROUP BY c.id ORDER BY MIN(l.vencimento), c.nome`)
	if err != nil {
		return p, err
	}
	var faturas []FaturaInicio
	for rows.Next() {
		var f FaturaInicio
		var fech, venc int
		if err := rows.Scan(&f.CartaoID, &f.Nome, &f.Limite, &fech, &venc, &f.Vencimento); err != nil {
			rows.Close()
			return p, err
		}
		f.Ref = f.Vencimento[:7]
		aberta, _ := faturaDe(fech, venc, agora)
		f.Fechada = f.Ref < aberta
		faturas = append(faturas, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, err
	}
	for _, f := range faturas {
		if err := conn.QueryRow(`SELECT COALESCE(SUM(`+valEf("lancamentos")+`), 0) FROM lancamentos
			WHERE cartao_id = ? AND status = 'pendente' AND tipo = 'pagar' AND vencimento = ?`,
			f.CartaoID, f.Vencimento).Scan(&f.Aberto); err != nil {
			return p, err
		}
		p.Faturas = append(p.Faturas, f)
	}
	if p.Insights, err = Insights(conn, agora); err != nil {
		return p, err
	}
	var nInv int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM investimentos WHERE status = 'ativo'`).Scan(&nInv); err != nil {
		return p, err
	}
	if nInv > 0 {
		pat, err := CalculaPatrimonio(conn)
		if err != nil {
			return p, err
		}
		p.Patrimonio, p.TemPatrimonio = pat.Liquido, true
	}
	return p, nil
}

// InicioTexto é a tela Início na TUI e na CLI (`prisma inicio`).
func InicioTexto(conn *sql.DB, _ []string) error {
	p, err := Inicio(conn)
	if err != nil {
		return err
	}
	if p.Vazio {
		fmt.Println("Bem-vindo ao Prisma! Para começar:")
		fmt.Println()
		fmt.Println("  1. cadastre uma conta do banco      (tela Contas, tecla a)")
		fmt.Println("  2. lance a primeira conta a pagar   (tela Pagar/Receber, tecla p)")
		fmt.Println("  3. cadastre o salário como recorrência (tela Recorrências, tecla a)")
		fmt.Println()
		fmt.Println("Ou importe o extrato do banco: prisma importar --arquivo extrato.ofx --conta 1")
		return nil
	}
	mesBR := p.Mes[5:] + "/" + p.Mes[:4]
	fmt.Printf("Saldo total:         %s\n", money.Format(p.Saldo))
	fmt.Printf("A pagar no mês:      %s   (%s)\n", money.Format(p.PendPagar), mesBR)
	fmt.Printf("A receber no mês:    %s   (%s)\n", money.Format(p.PendReceber), mesBR)

	if len(p.Insights) > 0 {
		fmt.Println()
		fmt.Println("ATENÇÃO")
		fmt.Println()
		icone := map[string]string{GravAlta: "⚠", GravMedia: "•", GravBaixa: "·"}
		for _, i := range p.Insights {
			fmt.Printf("  %s %s\n", icone[i.Gravidade], i.Texto)
		}
	}

	fmt.Println()
	if len(p.Avisos) == 0 {
		fmt.Printf("Nada vencendo nos próximos %d dias. ✓\n", diasInicio)
	} else {
		fmt.Printf("PRÓXIMOS %d DIAS\n\n", diasInicio)
		w := novaTabela()
		fmt.Fprintln(w, "ID\tVENCIMENTO\tDESCRIÇÃO\tVALOR\tSITUAÇÃO")
		for _, a := range p.Avisos {
			id := "-"
			if a.ID != 0 {
				id = fmt.Sprint(a.ID)
			}
			sit := map[string]string{SituacaoAtrasada: "⚠ atrasada", SituacaoHoje: "vence hoje", SituacaoProxima: "pendente"}[a.Situacao]
			tipo := a.Tipo
			if tipo == "fatura" {
				tipo = "pagar"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s %s\n", id, dataBR(a.Vencimento), a.Descricao, money.Format(a.Valor), tipo, sit)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if len(p.Faturas) > 0 {
		fmt.Println()
		fmt.Println("FATURAS")
		fmt.Println()
		w := novaTabela()
		for _, f := range p.Faturas {
			estado := "aberta"
			if f.Fechada {
				estado = "fechada"
			}
			fmt.Fprintf(w, "%s\t%s\tvence %s\t%s\n", f.Nome, money.Format(f.Aberto), dataBR(f.Vencimento), strings.TrimSpace(estado))
		}
		return w.Flush()
	}
	return nil
}
