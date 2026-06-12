// Prisma — gerenciador de finanças pessoais.
package main

import (
	"fmt"
	"os"

	"prisma/internal/app"
	"prisma/internal/bot"
	"prisma/internal/db"
	"prisma/internal/tui"
)

const ajuda = `prisma — gerenciador de finanças pessoais

USO
  prisma                                  abre a interface no terminal
  prisma --web [--porta N]                abre a interface no navegador
  prisma <comando> [subcomando] [opções]  modo linha de comando

COMANDOS
  conta        Contas bancárias            add | listar | editar | remover
  carteira     Carteiras (dinheiro etc.)   add | listar | editar | remover
  pagar        Contas a pagar              add | listar
  receber      Valores a receber           add | listar
  lancamentos  Lista tudo                  [--pendentes] [--tipo] [--mes] [--de] [--ate] [--cat] | editar | remover
  quitar       Marca como pago/recebido    quitar <id> [--data]
  transferir   Move entre conta/carteira   --de conta:1 --para carteira:2 --valor 100
  recorrencia  Regras automáticas          add | listar | editar | remover <id> [--limpar]
  emergencia   Plano de ação p/ dívidas    add | listar | plano | editar | quitar | remover
  plano        Planejamento de gastos      add | listar | status | editar | remover  (semana ou mês)
  relatorio    Análise do passado          [--meses N]  (categorias, mês a mês)
  extrato      Movimentação com saldo      --conta 1 | --carteira 1  [--meses N]
  previsao     Projeção de saldo futuro    [--meses N]
  saldo        Posição geral consolidada
  exportar     Lançamentos em CSV          [--saida arq.csv] [--mes AAAA-MM]
  importar     Extrato bancário OFX/CSV    --arquivo extrato.ofx --conta 1
  bot          Bot de Telegram             [--token X] [--chat N]  registra lançamentos por mensagem
  resetar      Apaga TODOS os dados        pede confirmação e faz backup antes
  ajuda        Mostra esta mensagem

EXEMPLOS
  prisma conta add --nome "Nubank" --tipo corrente --saldo 1.500,00
  prisma pagar add --desc "Aluguel" --valor 1200 --venc 05/07/2026 --cat moradia --repetir 12
  prisma pagar add --desc "Notebook" --valor 3.600,00 --parcelas 10
  prisma recorrencia add --tipo receber --desc "Salário" --valor 5000 --dia 1 --conta 1
  prisma transferir --de conta:1 --para carteira:1 --valor 200
  prisma lancamentos editar 7 --valor 1.250,00
  prisma emergencia add --desc "Cartão" --valor 8000 --juros 12 --aporte 1500
  prisma plano add --cat mercado --valor 800 --periodo mes
  prisma lancamentos --de 01/06/2026 --ate 15/06/2026 --cat mercado
  prisma relatorio --meses 6
  prisma previsao --meses 6

DOCUMENTAÇÃO
  Manual completo de uso: MANUAL.md (no repositório do projeto).

DADOS
  Banco SQLite local (Linux: ~/.local/share/prisma/prisma.db; mude com PRISMA_DB).
`

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "ajuda", "help", "-h", "--help":
			fmt.Print(ajuda)
			return
		}
	}

	conn, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// materializa lançamentos das recorrências antes de qualquer coisa
	if _, err := app.GerarRecorrencias(conn); err != nil {
		fmt.Fprintf(os.Stderr, "erro nas recorrências: %v\n", err)
		os.Exit(1)
	}

	// sem argumentos: abre a interface de terminal (TUI)
	if len(os.Args) < 2 {
		if err := tui.Run(conn); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "--web", "web":
		err = tui.RunWeb(conn, args)
	case "conta":
		err = app.Conta(conn, args)
	case "carteira":
		err = app.Carteira(conn, args)
	case "pagar":
		err = app.NovoLancamento(conn, "pagar", args)
	case "receber":
		err = app.NovoLancamento(conn, "receber", args)
	case "lancamentos", "lancamento":
		err = app.Lancamentos(conn, args)
	case "quitar":
		err = app.Quitar(conn, args)
	case "transferir":
		err = app.Transferir(conn, args)
	case "recorrencia", "recorrencias":
		err = app.Recorrencia(conn, args)
	case "emergencia":
		err = app.Emergencia(conn, args)
	case "plano", "planejamento":
		err = app.Plano(conn, args)
	case "relatorio":
		err = app.Relatorio(conn, args)
	case "extrato":
		err = app.Extrato(conn, args)
	case "previsao":
		err = app.Previsao(conn, args)
	case "saldo":
		err = app.Saldo(conn, args)
	case "exportar":
		err = app.Exportar(conn, args)
	case "importar":
		err = app.Importar(conn, args)
	case "bot":
		err = bot.Run(conn, args)
	case "resetar":
		err = app.Resetar(conn, args)
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %q\n\n", cmd)
		fmt.Print(ajuda)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
}
