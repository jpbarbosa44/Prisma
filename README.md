# Prisma

Gerenciador de finanças pessoais no terminal. Simples, rápido e offline: tudo fica em um único arquivo SQLite na sua máquina.

Digite `prisma` e a interface abre em tela cheia (como o btop), com o prisma em ASCII art e os menus logo abaixo:

```
                       /=\\
                      /===\ \
                     /=====\ ' \
                    /=======\ ' ' \
                   /=========\ ' ' '\
                  /===========\ ' ' ' \                         ██████╗ ██████╗ ██╗███████╗███╗   ███╗ █████╗
                 /=============\ ' ' ' '\                       ██╔══██╗██╔══██╗██║██╔════╝████╗ ████║██╔══██╗
                /===============\ ' ' ' ' \                     ██████╔╝██████╔╝██║███████╗██╔████╔██║███████║
───────────────/=================\ ' ' ' ' '\ ━━━━━━━━━━━━      ██╔═══╝ ██╔══██╗██║╚════██║██║╚██╔╝██║██╔══██║
              /===================\ ' ' ' ' ' \ ━━━━━━━━━━━━    ██║     ██║  ██║██║███████║██║ ╚═╝ ██║██║  ██║
             /=====================\ ' ' ' ' ' / ━━━━━━━━━━━━   ╚═╝     ╚═╝  ╚═╝╚═╝╚══════╝╚═╝     ╚═╝╚═╝  ╚═╝
            /=======================\ ' ' ' ' /
           /=========================\ ' ' ' /
          /===========================\ ' ' /
         /=============================\ ' /
        /===============================\/

 ▸ 1  Saldo          posição geral consolidada
   2  Contas         cadastro de contas bancárias
   3  Carteiras      dinheiro fora do banco
   4  Pagar/Receber  lançamentos e quitação
   5  Recorrências   salário, aluguel: todo mês, sozinho
   6  Emergência     plano de ação para quitar dívidas
   7  Planejamento   limites de gasto por semana ou mês
   8  Relatório      análise do passado, por categoria
   9  Previsão       projeção de saldo futuro

  ↑/↓ navegar · enter abrir · 1-9 atalho · q sair
```

Nas tabelas, `↑/↓` selecionam a linha e as ações usam o item selecionado (quitar, editar, remover — sem digitar o id). Remoções pedem confirmação `s/n`. Os formulários navegam com `tab`/`enter` e cancelam com `esc`.

Tudo também funciona como comandos diretos (`prisma saldo`, `prisma pagar add ...`), útil para scripts — veja os exemplos abaixo.

**Documentação:** [MANUAL.md](MANUAL.md) tem o guia de uso completo (todas as telas, comandos, filtros e receitas prontas); [INSTALL.md](INSTALL.md) cobre a instalação em cada sistema.

## Instalação

**Guia completo por sistema operacional (Linux, macOS e Windows): [INSTALL.md](INSTALL.md)** — inclui verificação de integridade, onde ficam os dados, atualização e desinstalação.

Resumo para Linux — o binário já compilado está em `dist/prisma-linux-amd64` (estático, sem dependências). Para usar como `prisma`:

```sh
mkdir -p ~/.local/bin
cp dist/prisma-linux-amd64 ~/.local/bin/prisma
```

### Mac e Windows

Os builds para macOS e Windows também estão em `dist/`:

| Plataforma | Arquivo |
|---|---|
| Linux x86-64 | `prisma-linux-amd64` |
| macOS Apple Silicon (M1+) | `prisma-mac-arm64` |
| macOS Intel | `prisma-mac-intel` |
| Windows x86-64 | `prisma-windows-amd64.exe` |
| Windows ARM | `prisma-windows-arm64.exe` |

No macOS, na primeira execução o Gatekeeper pode bloquear por não ser assinado — libere com `xattr -d com.apple.quarantine prisma-mac-arm64` ou em Ajustes › Privacidade e Segurança. No Windows, use o Windows Terminal ou PowerShell para a interface renderizar corretamente.

### Compilando do código

Requer Go 1.22+. Como o driver SQLite é Go puro (sem CGO), a compilação cruzada funciona de qualquer sistema:

```sh
make linux        # dist/prisma-linux-amd64
make mac          # dist/prisma-mac-{arm64,intel}
make windows      # dist/prisma-windows-{amd64,arm64}.exe
make release      # todos
# ou, sem make:
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/prisma-mac-arm64 ./cmd/prisma
```

## Conceitos

- **Conta** — conta bancária (corrente, poupança ou investimento).
- **Carteira** — dinheiro fora do banco (físico, vale, etc.).
- **Lançamento** — algo a **pagar** ou a **receber**, com vencimento, categoria e status (`pendente` → `quitado`). Pode ser vinculado a uma conta ou carteira; ao quitar, o saldo dela é atualizado.
- **Emergência** — uma dívida com plano de ação mês a mês para quitá-la.
- **Plano** — limite de gasto por categoria, por semana ou por mês.
- **Previsão** — projeção do saldo nos próximos meses.

Valores aceitam os formatos `1234`, `1234,56` e `1.234,56`; datas aceitam `AAAA-MM-DD` e `DD/MM/AAAA`.

## Uso

### Contas e carteiras

```sh
prisma conta add --nome "Nubank" --banco "Nubank" --tipo corrente --saldo 1.500,00
prisma conta listar
prisma carteira add --nome "Dinheiro" --saldo 200
prisma saldo                       # posição geral consolidada
```

### Pagar / Receber

```sh
prisma pagar add --desc "Aluguel" --valor 1200 --venc 05/07/2026 --cat moradia --conta 1 --repetir 12
prisma pagar add --desc "Notebook" --valor 3.600,00 --parcelas 10   # divide o TOTAL em 10x
prisma receber add --desc "Freela" --valor 800 --venc 01/07/2026 --conta 1
prisma lancamentos --pendentes     # filtros: --tipo, --mes, --de, --ate, --cat
prisma quitar 4                    # marca como pago/recebido (aceita --data)
prisma lancamentos editar 7 --valor 1.250,00 --cat moradia   # altera só o que for passado
prisma lancamentos remover 7
```

`--repetir N` repete o mesmo valor por N meses; `--parcelas N` divide o valor total em N parcelas; `--quitado` registra algo já pago. Categorias nunca usadas geram um aviso (proteção contra erros de digitação).

### Recorrências

Regras que geram lançamentos sozinhas, 3 meses à frente, a cada execução do Prisma:

```sh
prisma recorrencia add --tipo receber --desc "Salário" --valor 5000 --dia 1 --conta 1
prisma recorrencia add --tipo pagar --desc "Aluguel" --valor 1300 --dia 10 --cat moradia --fim 31/12/2027
prisma recorrencia listar
prisma recorrencia remover 1 --limpar   # apaga a regra e os pendentes gerados
```

### Transferências

Mover dinheiro entre contas e carteiras sem virar receita/despesa:

```sh
prisma transferir --de conta:1 --para carteira:1 --valor 200
```

### Modo de emergência (quitar dívidas)

```sh
prisma emergencia add --desc "Cartão de crédito" --credor "Banco X" --valor 8000 --juros 12 --aporte 1500
prisma emergencia plano 1          # reexibe o plano de ação mês a mês
prisma emergencia listar
prisma emergencia quitar 1
```

O plano simula juros compostos sobre o saldo devedor e mostra em quantos meses a dívida é quitada, o total de juros pagos e quanto você economiza acelerando o aporte. Se o aporte não cobre nem os juros, o Prisma avisa qual é o mínimo necessário.

### Planejamento (semanal ou mensal)

```sh
prisma plano add --cat mercado --valor 800 --periodo mes              # mês atual
prisma plano add --cat lazer --valor 100 --periodo semana --repetir 4 # próximas 4 semanas
prisma plano status                                                   # uso x limite do mês atual
prisma plano status --periodo semana --ref 2026-W30
```

O gasto considera lançamentos *a pagar* da categoria: quitados dentro do período + pendentes com vencimento no período.

### Relatório e extrato

```sh
prisma relatorio --meses 6         # gastos por categoria (com barras), balanço mês a mês, taxa de poupança
prisma extrato --conta 1 --meses 3 # movimentação com saldo corrente, incluindo transferências
```

### Previsão

```sh
prisma previsao --meses 6
```

Para cada mês futuro, usa os lançamentos pendentes agendados; quando um mês não tem nada agendado, estima pela média dos últimos 3 meses (marcado com `~`). Os aportes das emergências ativas entram como saída na coluna DÍVIDAS. Mostra um gráfico de barras do saldo projetado e avisa se ele ficar negativo.

### Exportar e importar

```sh
prisma exportar --saida lancamentos.csv          # CSV com ';' e vírgula decimal (Excel pt-BR)
prisma importar --arquivo extrato.ofx --conta 1  # extrato do banco (OFX ou CSV)
```

A importação cria os movimentos como quitados na conta indicada (negativo = pagar, positivo = receber), ignora duplicados e usa a categoria `importado`. CSV esperado: colunas data, descrição e valor.

### Bot de Telegram

```sh
prisma bot --token SEU_TOKEN   # primeira vez (token do @BotFather)
prisma bot                     # depois, é só rodar
```

Anote gastos pelo celular mandando mensagem ao seu bot: `25,50 #mercado pão e leite !` registra um gasto quitado de hoje; `+3500 #salario @05/07` registra uma receita. Dá para quitar (`quitar 142`), corrigir o último lançamento (`corrigir 27,90`), transferir entre conta e carteira, e guardar comprovante mandando a foto. Também responde consultas (`/saldo`, `/pendentes`, `/relatorio`, `/previsao`, `#categoria maio`...) e avisa sozinho: vencimentos às 9h (com botão de quitar) e resumo do dia às 20h. Só o chat autorizado tem acesso. Detalhes no [MANUAL.md](MANUAL.md#bot-telegram).

## Dados

O banco fica no diretório de dados padrão de cada sistema:

- Linux: `~/.local/share/prisma/prisma.db`
- macOS: `~/Library/Application Support/prisma/prisma.db`
- Windows: `%AppData%\prisma\prisma.db`

Para usar outro arquivo (ou fazer backup), use a variável `PRISMA_DB`:

```sh
PRISMA_DB=/tmp/teste.db prisma saldo
cp ~/.local/share/prisma/prisma.db backup-$(date +%F).db
```

## Arquitetura

```
cmd/prisma/        ponto de entrada: sem argumentos abre a TUI, com
                   argumentos roteia para os comandos
internal/db/       abertura do SQLite e schema (migrações idempotentes)
internal/money/    dinheiro em centavos (int64) — parse e formatação pt-BR
internal/app/      um arquivo por funcionalidade: conta, carteira,
                   lancamento, emergencia, plano, previsao
internal/tui/      interface de terminal (Bubble Tea): cabeçalho em ASCII
                   art, menu, telas e formulários — as telas capturam a
                   saída dos comandos da CLI, reaproveitando toda a lógica
internal/bot/      bot de Telegram (long polling, só stdlib): traduz
                   mensagens de texto em lançamentos

```

Decisões:

- **Go + SQLite puro-Go**: binário estático único, sem runtime nem dependências de sistema.
- **Centavos em `int64`**: nada de ponto flutuante para dinheiro.
- **Saldos calculados, não armazenados**: o saldo de uma conta é `saldo_inicial + lançamentos quitados vinculados ± transferências`, sempre consistente.
- **Transferência não é despesa**: tabela própria, não distorce relatórios.
- **Recorrências materializadas na abertura**: cada execução gera os lançamentos pendentes dos próximos 3 meses, de forma idempotente.
- **Somente biblioteca padrão** além do driver SQLite e do Bubble Tea (TUI).

Testes: `go test ./...` cobre dinheiro (parse/format), períodos ISO, simulação de dívida, parcelas, transferências, recorrências, edição e importação OFX/CSV.
