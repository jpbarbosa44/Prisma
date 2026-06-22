// Prisma — gerenciador de finanças pessoais.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"prisma/internal/app"
	"prisma/internal/bot"
	"prisma/internal/db"
	"prisma/internal/remote"
	"prisma/internal/tui"
	"prisma/internal/update"
)

const ajuda = `prisma — gerenciador de finanças pessoais

USO
  prisma                                  abre a interface no terminal
  prisma --web [--porta N]                abre a interface no navegador
  prisma --empresa [...]                  mesmos comandos, banco separado da empresa
  prisma --analytics [--web]              módulo de análise financeira (somente leitura)
  prisma <comando> [subcomando] [opções]  modo linha de comando

COMANDOS
  conta        Contas bancárias            add | listar | editar | remover
  carteira     Carteiras (dinheiro etc.)   add | listar | editar | remover
  grupo        Pessoas que dividem gastos  add | listar | editar | remover
  categoria    Catálogo de categorias      add | listar | editar | remover
  cartao       Cartões de crédito          add | listar | editar | remover
  fatura       Fatura do cartão            --cartao N [--ref AAAA-MM] | pagar --cartao N
  socio        Sócios da empresa           add | listar | editar | remover  (com --empresa)
  capital      Capital social              aportar --socio N --valor V --conta N | listar
  imposto      Impostos da empresa         add [--recorrente --dia N] | listar
  investimento Investimentos da empresa    add | listar
  lucro        Lucro da empresa            calcular | distribuir --valor V | listar
  pagar        Contas a pagar              add | listar
  receber      Valores a receber           add | listar
  lancamentos  Lista tudo                  [--pendentes] [--tipo] [--mes] [--de] [--ate] [--cat] | editar | remover
  quitar       Marca como pago/recebido    quitar <id> [--data]
  transferir   Move entre conta/carteira   --de conta:1 --para carteira:2 --valor 100
  recorrencia  Regras automáticas          add | listar | editar | remover <id> [--limpar]
  assinatura   Assinaturas (recorrência)   listar | add | editar | remover
  emergencia   Plano de ação p/ dívidas    add | listar | plano | editar | quitar | remover
  plano        Planejamento de gastos      add | listar | status | editar | remover  (semana ou mês)
  relatorio    Análise do passado          [--meses N]  (categorias, mês a mês)
  estatisticas Análises estatísticas        [--meses N]  (tendência, top gastos, saúde financeira)
  graficos     Gráficos em ASCII           [--meses N]  (categorias, saldo, receitas×despesas, grupos)
  extrato      Movimentação com saldo      --conta 1 | --carteira 1  [--meses N]
  previsao     Projeção de saldo futuro    [--meses N]
  simular      E se eu comprar isto?       --valor 4000 --parcelas 12 [--juros N] [--entrada N]
  saldo        Posição geral consolidada
  exportar     Lançamentos em CSV          [--saida arq.csv] [--mes AAAA-MM]
  importar     Extrato bancário OFX/CSV    --arquivo extrato.ofx --conta 1
  bot          Bot de Telegram             [--token X] [--chat N] [--instalar-servico]  registra lançamentos por mensagem
  servidor     Compartilha o banco na rede  --token X [--porta N]  (outro Prisma conecta como cliente)
  config       Modo de operação             cliente --host X --token Y | local | (sem args mostra o atual)
  atualizar    Baixa e instala a versão nova (do GitHub, com conferência de SHA256)
  versao       Mostra a versão instalada
  verificar    Integridade do banco         confere o banco em uso e os backups
  restaurar    Volta a um backup            lista as cópias e restaura (guarda o estado atual antes)
  resetar      Apaga TODOS os dados        pede confirmação e faz backup antes
  ajuda        Mostra esta mensagem

EXEMPLOS
  prisma conta add --nome "Nubank" --tipo corrente --saldo 1.500,00
  prisma grupo add --nome "Eu e a Maria" --pessoas "Eu, Maria"
  prisma pagar add --desc "Mercado" --valor 300 --grupo 1   (conta só a sua parte: 150,00)
  prisma cartao add --nome "Nubank" --fechamento 20 --vencimento 27 --conta 1 --fatura-atual 1.200,00
  prisma pagar add --desc "Tênis" --valor 400 --parcelas 4 --cartao 1   (4x na fatura)
  prisma fatura --cartao 1                  (ver a fatura aberta)
  prisma fatura pagar --cartao 1            (paga a fatura, debita a conta)
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
  prisma simular --desc "Videogame" --valor 4.000,00 --parcelas 12

EMPRESA (prisma --empresa ...)
  prisma --empresa socio add --nome "Você" --participacao 60
  prisma --empresa socio add --nome "Sócio" --participacao 40
  prisma --empresa capital aportar --socio 1 --valor 5.000,00 --conta 1
  prisma --empresa imposto add --desc "DAS" --valor 250 --recorrente --dia 20
  prisma --empresa investimento add --desc "Notebook" --valor 4.500,00
  prisma --empresa lucro calcular
  prisma --empresa lucro distribuir --valor 2.000,00

DOCUMENTAÇÃO
  Manual completo de uso: MANUAL.md (no repositório do projeto).

DADOS
  Banco SQLite local (Linux: ~/.local/share/prisma/prisma.db; mude com PRISMA_DB).
  Empresa (--empresa): banco separado (Linux: ~/.local/share/prisma/empresa.db; mude com PRISMA_EMPRESA_DB).

COMPARTILHAMENTO (casal/família na mesma rede)
  Numa máquina:   prisma servidor --token SEGREDO   (mantém o banco e fica no ar)
  Na outra:       prisma config cliente --host <ip> --token SEGREDO --fingerprint <X>
  Voltar ao normal: prisma config local
  (o comando "prisma servidor" mostra o host, o token e o fingerprint prontos)
`

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "ajuda", "help", "-h", "--help":
			fmt.Print(ajuda)
			return
		case "versao", "version", "--version", "-v":
			fmt.Printf("prisma %s\n", update.Versao)
			return
		case "atualizar", "update", "autoupdate":
			if err := update.Atualizar(); err != nil {
				fmt.Fprintf(os.Stderr, "erro: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// `prisma --empresa ...` troca pro banco separado da empresa (outro
	// arquivo, $PRISMA_EMPRESA_DB ou empresa.db) e ignora o modo
	// cliente/servidor da config pessoal — a empresa é sempre local. O resto
	// do fluxo (TUI sem args, --web, switch de comandos) roda igual, só com
	// os.Args já sem a flag.
	modoEmpresa := false
	if len(os.Args) >= 2 && os.Args[1] == "--empresa" {
		modoEmpresa = true
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	// `prisma --analytics` abre o módulo de análise (somente leitura) sobre o
	// banco pessoal e sobe a TUI exclusiva do Analytics (ou a interface web com
	// --web) — sem CRUD, sem modo cliente/servidor, sem materializar
	// recorrências (nada escreve).
	if len(os.Args) >= 2 && os.Args[1] == "--analytics" {
		if modoEmpresa {
			fmt.Fprintln(os.Stderr, "erro: --analytics não pode ser combinado com --empresa")
			os.Exit(2)
		}
		// ao abrir, oferece atualizar se há versão nova (pergunta antes)
		if atualizou, _ := update.OfereceAtualizar(os.Stdin, os.Stdout); atualizou {
			return
		}
		conn, err := db.AbrirAnalytics()
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()
		// `prisma --analytics --web [--porta N]` abre o Analytics no navegador.
		restante := os.Args[2:]
		if len(restante) >= 1 && (restante[0] == "--web" || restante[0] == "web") {
			if err := tui.RunWebAnalytics(conn, restante[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "erro: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := tui.RunAnalytics(conn); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// descobre o papel desta instância: banco local (padrão), servidor ou
	// cliente de outro Prisma na rede.
	cfg, err := remote.Carrega()
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro de configuração: %v\n", err)
		os.Exit(1)
	}

	// compartilhamento na rede (servidor/cliente) é só do banco pessoal por
	// enquanto; a empresa não tem esse modo ainda.
	if modoEmpresa && len(os.Args) >= 2 && (os.Args[1] == "servidor" || os.Args[1] == "config" || os.Args[1] == "configurar") {
		fmt.Fprintf(os.Stderr, "erro: %q não tem suporte em --empresa ainda (só o banco pessoal pode ser compartilhado na rede)\n", os.Args[1])
		os.Exit(2)
	}

	// `prisma servidor` libera o banco local na rede e fica em primeiro plano.
	if len(os.Args) >= 2 && os.Args[1] == "servidor" {
		if err := rodarServidor(cfg, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `prisma config` mexe no modo (cliente/local) sem precisar do banco — roda
	// antes de qualquer conexão, senão um config de cliente quebrado impediria
	// até de consertar o config.
	if len(os.Args) >= 2 && (os.Args[1] == "config" || os.Args[1] == "configurar") {
		if err := configurar(cfg, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `prisma restaurar` troca o banco por uma cópia de backup — roda antes de
	// abrir qualquer conexão, para não substituir o arquivo com o banco em uso.
	if len(os.Args) >= 2 && (os.Args[1] == "restaurar" || os.Args[1] == "restore") {
		if err := app.Restaurar(os.Args[2:], modoEmpresa); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// checa por uma versão nova em segundo plano (no máximo 1x/dia, em silêncio)
	go update.AtualizaCache()

	var conn *sql.DB
	if modoEmpresa {
		conn, err = db.OpenEmpresa()
	} else {
		conn, err = db.Abrir(cfg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// materializa as recorrências só quando somos donos do banco; no modo
	// cliente quem faz isso é o servidor (a empresa é sempre dona do seu banco).
	if modoEmpresa || cfg.Modo != remote.ModoCliente {
		if _, err := app.GerarRecorrencias(conn); err != nil {
			fmt.Fprintf(os.Stderr, "erro nas recorrências: %v\n", err)
			os.Exit(1)
		}
		// quita automaticamente os pendentes marcados que já venceram
		if _, err := app.QuitarVencidos(conn); err != nil {
			fmt.Fprintf(os.Stderr, "erro ao quitar vencidos: %v\n", err)
			os.Exit(1)
		}
	}

	// sem argumentos: abre a interface de terminal (TUI)
	if len(os.Args) < 2 {
		// ao abrir, oferece atualizar se há versão nova (pergunta antes de baixar)
		if atualizou, _ := update.OfereceAtualizar(os.Stdin, os.Stdout); atualizou {
			return
		}
		if err := tui.Run(conn, modoEmpresa); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	err = despacha(conn, modoEmpresa, cmd, args)
	if errors.Is(err, errComandoDesconhecido) {
		fmt.Fprintf(os.Stderr, "comando desconhecido: %q\n\n", cmd)
		fmt.Print(ajuda)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
	// aviso discreto de versão nova, no stderr para não sujar saída de scripts
	if aviso, _ := update.Aviso(); aviso != "" {
		fmt.Fprintf(os.Stderr, "\n↑ %s\n", aviso)
	}
}

// errComandoDesconhecido sinaliza um comando não reconhecido — o main imprime a
// ajuda e sai com código 2. É um sentinela para despacha ficar testável sem
// chamar os.Exit nem depender do texto da mensagem.
var errComandoDesconhecido = errors.New("comando desconhecido")

// despacha roteia um comando de linha (já sem as flags globais como --empresa)
// para a função correspondente do app/tui/bot, operando sobre conn. Devolve erro
// em vez de chamar os.Exit, para ser exercível por testes.
func despacha(conn *sql.DB, modoEmpresa bool, cmd string, args []string) error {
	switch cmd {
	case "--web", "web":
		return tui.RunWeb(conn, args, modoEmpresa)
	case "conta":
		return app.Conta(conn, args)
	case "carteira":
		return app.Carteira(conn, args)
	case "grupo", "grupos":
		return app.Grupo(conn, args)
	case "categoria", "categorias":
		return app.Categoria(conn, args)
	case "socio", "socios":
		return app.Socio(conn, args)
	case "capital":
		return app.Capital(conn, args)
	case "imposto", "impostos":
		return app.Imposto(conn, args)
	case "investimento", "investimentos":
		return app.Investimento(conn, args)
	case "lucro":
		return app.Lucro(conn, args)
	case "cartao", "cartoes", "cartão", "cartões":
		return app.Cartao(conn, args)
	case "fatura", "faturas":
		return app.Fatura(conn, args)
	case "pagar":
		return app.NovoLancamento(conn, "pagar", args)
	case "receber":
		return app.NovoLancamento(conn, "receber", args)
	case "lancamentos", "lancamento":
		return app.Lancamentos(conn, args)
	case "quitar":
		return app.Quitar(conn, args)
	case "transferir":
		return app.Transferir(conn, args)
	case "recorrencia", "recorrencias":
		return app.Recorrencia(conn, args)
	case "assinatura", "assinaturas":
		return app.Assinaturas(conn, args)
	case "emergencia":
		return app.Emergencia(conn, args)
	case "plano", "planejamento":
		return app.Plano(conn, args)
	case "relatorio":
		return app.Relatorio(conn, args)
	case "estatisticas", "estatistica", "estatísticas":
		return app.Estatisticas(conn, args)
	case "graficos", "grafico", "gráficos", "gráfico":
		return app.Graficos(conn, args)
	case "extrato":
		return app.Extrato(conn, args)
	case "previsao":
		return app.Previsao(conn, args)
	case "simular", "simulacao", "simulação":
		return app.Simular(conn, args)
	case "saldo":
		return app.Saldo(conn, args)
	case "exportar":
		return app.Exportar(conn, args)
	case "importar":
		return app.Importar(conn, args)
	case "bot":
		return bot.Run(conn, args)
	case "resetar":
		return app.Resetar(conn, args)
	case "verificar", "verify":
		return app.Verificar(conn, modoEmpresa)
	default:
		return fmt.Errorf("%w: %q", errComandoDesconhecido, cmd)
	}
}
