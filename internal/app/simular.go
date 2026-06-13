package app

import (
	"database/sql"
	"flag"
	"fmt"
	"math"
	"time"

	"prisma/internal/money"
)

// Simular projeta o impacto de uma compra parcelada no saldo futuro, sem
// gravar nada no banco. Responde à pergunta "se eu comprar isto, fico
// negativado? fico no aperto? ou posso comprar?": projeta o saldo mês a mês
// com e sem a compra (reusando o modelo da Previsão) e dá um veredito.
func Simular(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("simular", flag.ContinueOnError)
	desc := fs.String("desc", "", "o que vai comprar (ex.: \"Videogame\")")
	valorStr := fs.String("valor", "", "preço total da compra (obrigatório)")
	parcelas := fs.Int("parcelas", 1, "em quantas parcelas mensais (1 = à vista)")
	jurosPct := fs.Float64("juros", 0, "juros do parcelamento em % ao mês (0 = sem juros)")
	entradaStr := fs.String("entrada", "", "valor pago de entrada, à vista (opcional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *valorStr == "" {
		return fmt.Errorf("--valor é obrigatório (ex.: prisma simular --valor 4000 --parcelas 12)")
	}
	valor, err := money.Parse(*valorStr)
	if err != nil {
		return err
	}
	if valor <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	if *parcelas < 1 || *parcelas > 120 {
		return fmt.Errorf("--parcelas deve estar entre 1 e 120")
	}
	if *jurosPct < 0 || *jurosPct > 100 {
		return fmt.Errorf("--juros deve estar entre 0 e 100 (%% ao mês)")
	}
	var entrada int64
	if *entradaStr != "" {
		entrada, err = money.Parse(*entradaStr)
		if err != nil {
			return err
		}
	}
	if entrada < 0 {
		return fmt.Errorf("a entrada não pode ser negativa")
	}
	if entrada >= valor {
		return fmt.Errorf("a entrada (%s) cobre o valor todo (%s) — não há o que parcelar",
			money.Format(entrada), money.Format(valor))
	}

	financiado := valor - entrada
	parcelasVals := parcelasCompra(financiado, *jurosPct, *parcelas)
	var totalParcelado int64
	for _, p := range parcelasVals {
		totalParcelado += p
	}
	totalCompra := entrada + totalParcelado

	saldo, err := saldoTotal(conn)
	if err != nil {
		return err
	}
	mediaRec, mediaDesp, err := mediasHistoricas(conn)
	if err != nil {
		return err
	}
	aportes, err := aportesEmergencias(conn)
	if err != nil {
		return err
	}

	nome := *desc
	if nome == "" {
		nome = "Compra"
	}
	fmt.Printf("SIMULAÇÃO — %s\n", nome)
	if *parcelas == 1 {
		fmt.Printf("Valor: %s à vista\n", money.Format(valor))
	} else {
		fmt.Printf("Valor: %s em %dx de %s", money.Format(valor), *parcelas, money.Format(parcelasVals[0]))
		if entrada > 0 {
			fmt.Printf(" + entrada de %s", money.Format(entrada))
		}
		fmt.Println()
	}
	if *jurosPct > 0 {
		fmt.Printf("Juros: %.2f%% a.m. — total pago %s (%s só de juros)\n",
			*jurosPct, money.Format(totalCompra), money.Format(totalCompra-valor))
	}
	fmt.Printf("Saldo atual: %s\n", money.Format(saldo))
	fmt.Printf("Média mensal: receitas %s, despesas %s\n\n",
		money.Format(mediaRec), money.Format(mediaDesp))

	// projeção mês a mês: SEM a compra (linha de base) e COM a compra
	agora := time.Now()
	projBase := saldo
	projCompra := saldo - entrada // a entrada sai agora, à vista
	minCompra := projCompra
	var mesNegativo string
	var negativoCompra int64

	w := novaTabela()
	fmt.Fprintln(w, "MÊS\tPARCELA\tSEM A COMPRA\tCOM A COMPRA")
	for i := 1; i <= *parcelas; i++ {
		ref := agora.AddDate(0, i, 0).Format("2006-01")
		p, err := periodoMes(ref)
		if err != nil {
			return err
		}
		rec, _, err := previstoMes(conn, "receber", p, mediaRec)
		if err != nil {
			return err
		}
		desp, _, err := previstoMes(conn, "pagar", p, mediaDesp)
		if err != nil {
			return err
		}
		fluxo := rec - desp - aportes[i]
		parcela := parcelasVals[i-1]
		projBase += fluxo
		projCompra += fluxo - parcela
		if projCompra < minCompra {
			minCompra = projCompra
		}
		if projCompra < 0 && mesNegativo == "" {
			mesNegativo = dataBR(p.Inicio)[3:]
			negativoCompra = projCompra
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			dataBR(p.Inicio)[3:], money.Format(parcela), money.Format(projBase), money.Format(projCompra))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	// veredito: uma folga saudável é ter ainda ~1 mês de despesas de reserva
	fmt.Println()
	colchao := mediaDesp
	switch {
	case mesNegativo != "":
		fmt.Printf("🔴 NÃO recomendado: com essa compra seu saldo fica NEGATIVO em %s (chega a %s).\n",
			mesNegativo, money.Format(negativoCompra))
		fmt.Println("Considere mais parcelas, dar uma entrada maior, ou adiar a compra.")
	case colchao > 0 && minCompra < colchao:
		fmt.Printf("⚠ Arriscado: dá pra comprar, mas sua folga cai para %s — menos de um mês de despesas (%s).\n",
			money.Format(minCompra), money.Format(colchao))
		fmt.Println("Você ficaria sem reserva para imprevistos durante o parcelamento.")
	default:
		fmt.Printf("🟢 Pode comprar: mesmo com a compra, seu saldo nunca cai abaixo de %s.\n",
			money.Format(minCompra))
	}
	return nil
}

// parcelasCompra devolve o valor de cada uma das n parcelas. Sem juros, divide
// o financiado igualmente e a última parcela absorve o resto (como nos
// lançamentos parcelados). Com juros, usa a Tabela Price (parcela fixa).
func parcelasCompra(financiado int64, jurosPct float64, n int) []int64 {
	parcelas := make([]int64, n)
	if jurosPct <= 0 {
		base := financiado / int64(n)
		for i := range parcelas {
			parcelas[i] = base
		}
		parcelas[n-1] = financiado - base*int64(n-1)
		return parcelas
	}
	i := jurosPct / 100
	pmt := float64(financiado) * i / (1 - math.Pow(1+i, -float64(n)))
	valor := int64(math.Round(pmt))
	for k := range parcelas {
		parcelas[k] = valor
	}
	return parcelas
}
