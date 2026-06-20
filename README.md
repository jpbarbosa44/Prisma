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
   4  Grupos         pessoas que dividem despesas
   5  Categorias     catálogo de categorias
   6  Cartões        cartões de crédito e faturas
   7  Pagar/Receber  lançamentos e quitação
   8  Recorrências   salário, aluguel: todo mês, sozinho
   9  Assinaturas    serviços recorrentes (Netflix, Spotify...)
  10  Emergência     plano de ação para quitar dívidas
  11  Planejamento   limites de gasto por semana ou mês
  12  Relatório      análise do passado, por categoria
  13  Estatísticas   tendência, top gastos, saúde financeira
  14  Gráficos       categorias, saldo, receitas × despesas
  15  Previsão       projeção de saldo futuro
  16  Simulação      e se eu comprar isto?

  ↑/↓ navegar · enter abrir · 1-17 atalho · q sair
```

Nas tabelas, `↑/↓` selecionam a linha e as ações usam o item selecionado (quitar, editar, remover — sem digitar o id). Remoções pedem confirmação `s/n`. Os formulários navegam com `tab`/`enter` e cancelam com `esc`.

Tudo também funciona como comandos diretos (`prisma saldo`, `prisma pagar add ...`), útil para scripts — veja os exemplos abaixo.

Prefere o navegador? `prisma --web` sobe um servidor local (só em `127.0.0.1`, nada sai da sua máquina) e abre as mesmas telas no browser, com os mesmos atalhos de teclado. Opções: `--porta N` muda a porta (padrão 7747) e `--sem-abrir` não chama o navegador.

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
- **Lançamento** — algo a **pagar** ou a **receber**, com vencimento, categoria, observação e status (`pendente` → `quitado`). Pode ser vinculado a uma conta ou carteira; ao quitar, o saldo dela é atualizado. Pode ainda **quitar sozinho no vencimento** (`--auto-quitar`).
- **Categoria** — etiqueta dos lançamentos (mercado, moradia, salário...). O Prisma mantém um **catálogo**: ao usar uma categoria nova, ela é cadastrada automaticamente; na interface, o campo sugere as existentes enquanto você digita.
- **Grupo** — pessoas que dividem despesas (ex.: "eu e a Maria"). Uma despesa vinculada a um grupo passa a contar, no sistema inteiro, só pela **sua parte** (valor cheio ÷ nº de pessoas).
- **Cartão** — cartão de crédito, com dias de fechamento e vencimento. Uma compra no cartão é um lançamento que cai na **fatura** do ciclo e só mexe no saldo do banco quando a fatura é paga.
- **Assinatura** — serviço recorrente (Netflix, Spotify, academia...), em geral pago no cartão. É uma recorrência de despesa com visão e total mensal próprios.
- **Emergência** — uma dívida com plano de ação mês a mês para quitá-la.
- **Plano** — limite de gasto por categoria, por semana ou por mês.
- **Relatório** — análise do passado: gastos por categoria e balanço mês a mês.
- **Estatísticas** — análise estatística mais profunda: média/mediana por categoria, tendência e variação, top gastos e recorrentes, projeção e saúde financeira.
- **Previsão** — projeção do saldo nos próximos meses.
- **Simulação** — projeta o impacto de uma compra parcelada no saldo futuro, sem gravar nada.

Valores aceitam os formatos `1234`, `1234,56` e `1.234,56`; datas aceitam `AAAA-MM-DD`, `DD/MM/AAAA` e `DD/MM` (assume o ano vigente).

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
prisma pagar add --desc "Mercado" --valor 300 --grupo 1            # conta só a sua parte
prisma pagar add --desc "Tênis" --valor 400 --parcelas 4 --cartao 1 # 4x na fatura do cartão
prisma pagar add --desc "IPTU" --valor 600 --venc 10/03 --auto-quitar --obs "cota única"
prisma receber add --desc "Freela" --valor 800 --venc 01/07/2026 --conta 1
prisma lancamentos --pendentes     # filtros: --tipo, --mes, --de, --ate, --cat, --grupo
prisma quitar 4                    # marca como pago/recebido (aceita --data)
prisma lancamentos editar 7 --valor 1.250,00 --cat moradia   # altera só o que for passado
prisma lancamentos remover 7       # se for a parcela raiz, remove TODAS as parcelas
```

`--repetir N` repete o mesmo valor por N meses; `--parcelas N` divide o valor total em N parcelas (não use os dois juntos); `--quitado` registra algo já pago. `--grupo N` divide a despesa (só a sua parte conta); `--cartao N` joga a compra na fatura do cartão. `--obs` guarda uma observação; `--auto-quitar` faz o lançamento quitar sozinho quando o vencimento chega. Remover a **parcela raiz** (a 1ª) apaga todas as parcelas do grupo. Categorias nunca usadas geram um aviso (proteção contra erros de digitação) e entram no catálogo.

### Recorrências

Regras que geram lançamentos sozinhas, 3 meses à frente, a cada execução do Prisma:

```sh
prisma recorrencia add --tipo receber --desc "Salário" --valor 5000 --dia 1 --conta 1
prisma recorrencia add --tipo pagar --desc "Aluguel" --valor 1300 --dia 10 --cat moradia --fim 31/12/2027
prisma recorrencia add --tipo pagar --desc "Internet" --valor 100 --dia 15 --cartao 1   # gera na fatura
prisma recorrencia add --tipo pagar --desc "Faxina" --valor 200 --dia 5 --grupo 1 --auto-quitar
prisma recorrencia listar          # com fim definido, mostra quantas ocorrências faltam
prisma recorrencia remover 1 --limpar   # apaga a regra e os pendentes gerados
```

A recorrência pode vincular a uma conta, carteira ou cartão (`--cartao N`, cai na fatura) e a um grupo (`--grupo N`, divide a sua parte). `--auto-quitar` faz os lançamentos gerados quitarem sozinhos no vencimento. Quando a regra tem início **e** fim, a listagem mostra quantas ocorrências ainda faltam. As assinaturas são recorrências de despesa com visão própria — veja abaixo.

### Transferências

Mover dinheiro entre contas e carteiras sem virar receita/despesa:

```sh
prisma transferir --de conta:1 --para carteira:1 --valor 200
```

### Grupos (dividir despesas)

Para gastos divididos com outras pessoas: vincule a despesa a um grupo e o Prisma conta só a parte que cabe a você (valor cheio ÷ nº de pessoas) em todo o sistema — saldo, relatórios, previsão e gráficos.

```sh
prisma grupo add --nome "Eu e a Maria" --pessoas "Eu, Maria"   # 2 pessoas: divide por 2
prisma pagar add --desc "Mercado" --valor 300 --grupo 1        # pesa só 150,00 no seu bolso
prisma grupo listar                                            # total cheio, sua parte e gasto do mês atual
prisma lancamentos --grupo 1                                   # vê os gastos vinculados ao grupo
prisma grupo editar 1 --pessoas "Eu, Maria, João"             # passa a dividir por 3
```

A listagem de grupos mostra também a **soma do mês vigente** (a sua parte). Na interface, a tela Grupos tem a ação **ver despesas**, que lista os lançamentos vinculados.

### Categorias

O Prisma mantém um catálogo de categorias. Categorias usadas num lançamento ou recorrência entram nele automaticamente; você também pode gerenciá-las à mão:

```sh
prisma categoria add --nome mercado
prisma categoria listar              # cada categoria + nº de lançamentos
prisma categoria editar 3 --nome supermercado   # renomeia e atualiza os lançamentos
prisma categoria remover 5           # tira do catálogo (os lançamentos ficam)
```

Na interface, o campo de categoria sugere as existentes enquanto você digita (←/→ navega nelas) e cria a nova se você digitar um nome inédito.

### Cartões de crédito e faturas

Uma compra no cartão entra na fatura do ciclo (calculado pelos dias de fechamento e vencimento) e só debita a conta quando você paga a fatura — não antes.

```sh
prisma cartao add --nome "Nubank" --fechamento 20 --vencimento 27 --conta 1 --fatura-atual 1.200,00
prisma pagar add --desc "Tênis" --valor 400 --parcelas 4 --cartao 1   # 4x, cada uma numa fatura
prisma cartao listar               # limite e fatura aberta de cada cartão
prisma fatura --cartao 1           # detalhe da fatura em aberto (ou --ref AAAA-MM)
prisma fatura pagar --cartao 1     # quita a fatura em bloco e debita a conta do cartão
```

`--fatura-atual` lança o saldo já em aberto ao cadastrar o cartão, sem precisar relançar o passado. A divisão por grupo continua valendo: a fatura reflete a sua parte. **Remover um cartão apaga junto os lançamentos de despesa vinculados a ele.**

### Assinaturas

Serviços recorrentes (Netflix, Spotify, academia...). É uma recorrência de despesa com visão própria e total mensal somado.

```sh
prisma assinaturas add --desc "Netflix" --valor 39,90 --dia 20 --cartao 1
prisma assinaturas add --desc "Spotify Família" --valor 34,90 --dia 10 --conta 1 --grupo 1  # dividida
prisma assinaturas listar          # cada assinatura + total mensal (e quantas cobranças faltam, se tiver fim)
prisma assinaturas remover 3
```

`--grupo N` divide a assinatura entre as pessoas do grupo (só a sua parte conta).

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

### Estatísticas

Análise estatística mais profunda do histórico quitado:

```sh
prisma estatisticas --meses 6
```

Quatro blocos: **resumo por categoria** (média, mediana, maior/menor mês e % do total), **tendência e variação** (mês a mês, média móvel e alerta de categorias acima da própria média), **top gastos e recorrentes** (maiores lançamentos e despesas repetidas, candidatas a virar recorrência) e **projeção e saúde financeira** (sobra, taxa de poupança, projeção do saldo e meses de fôlego).

### Gráficos

```sh
prisma graficos --meses 6
```

Gráficos de barras em ASCII: gastos por categoria, receitas × despesas mês a mês, evolução do saldo e despesa por grupo (a sua parte sobre o total cheio). Na interface web (`--web`) os mesmos dados viram gráficos em SVG.

### Previsão

```sh
prisma previsao --meses 6
```

Para cada mês futuro, usa os lançamentos pendentes agendados; quando um mês não tem nada agendado, estima pela média dos últimos 3 meses (marcado com `~`). Os aportes das emergências ativas entram como saída na coluna DÍVIDAS. Mostra um gráfico de barras do saldo projetado e avisa se ele ficar negativo.

### Simulação ("e se eu comprar isto?")

```sh
prisma simular --desc "Videogame" --valor 4.000,00 --parcelas 12
prisma simular --valor 6000 --parcelas 10 --juros 2,5 --entrada 1000
```

Projeta o saldo mês a mês com e sem a compra — usando o mesmo motor da Previsão (suas recorrências, contas a pagar/receber e aportes de emergência) — e dá um veredito: 🟢 pode comprar, ⚠ arriscado (folga abaixo de um mês de despesas) ou 🔴 não recomendado (saldo fica negativo). `--juros` usa a Tabela Price; nada é gravado no banco.

### Exportar e importar

```sh
prisma exportar --saida lancamentos.csv          # CSV com ';' e vírgula decimal (Excel pt-BR)
prisma importar --arquivo extrato.ofx --conta 1  # extrato do banco (OFX ou CSV)
```

A importação cria os movimentos como quitados na conta indicada (negativo = pagar, positivo = receber), ignora duplicados e usa a categoria `importado`. CSV esperado: colunas data, descrição e valor.

### Bot de Telegram

```sh
prisma bot --token SEU_TOKEN              # primeira vez (token do @BotFather)
prisma bot                                # depois, é só rodar
prisma bot --token X --instalar-servico   # roda em segundo plano (systemd)
```

Anote gastos pelo celular mandando mensagem ao seu bot: `25,50 #mercado pão e leite !` registra um gasto quitado de hoje; `+3500 #salario @05/07` registra uma receita; `300 #mercado feira grupo:1` divide a despesa com o grupo (só a sua parte conta). Dá para quitar (`quitar 142`), corrigir o último lançamento (`corrigir 27,90`), transferir entre conta e carteira, e guardar comprovante mandando a foto. Também responde consultas (`/saldo`, `/pendentes`, `/relatorio`, `/previsao`, `/grupos`, `#categoria maio`...) e avisa sozinho: vencimentos às 9h (com botão de quitar) e resumo do dia às 20h. Só o chat autorizado tem acesso. Detalhes no [MANUAL.md](MANUAL.md#bot-telegram).

## Empresa (`prisma --empresa`)

Pra controlar o financeiro de uma empresa (sócios, capital social, imposto, investimento, lucro) sem misturar com as suas finanças pessoais, use `prisma --empresa` antes de qualquer comando — troca pra um banco totalmente separado (`empresa.db`, ou `PRISMA_EMPRESA_DB`) e, na TUI, mostra o selo **CORP** ao lado do logo. Todo o resto do Prisma (contas, cartões, relatório, gráficos...) funciona normalmente dentro desse banco.

```sh
prisma --empresa                                              # TUI no banco da empresa
prisma socio add --nome "Você" --participacao 60               # participação por sócio (não precisa ser 50/50)
prisma socio add --nome "Sócio" --participacao 40
prisma capital aportar --socio 1 --valor 6.000,00 --conta 1    # aporte de capital social
prisma imposto add --desc "DAS" --valor 250 --recorrente --dia 20
prisma investimento add --desc "Notebook" --valor 4.500,00 --parcelas 12
prisma lucro calcular                                          # receitas - despesas (exclui capital/distribuição)
prisma lucro distribuir --valor 2.000,00                       # divide pela participação de cada sócio
```

Detalhes (incluindo os avisos de participação/lucro acumulado) no [MANUAL.md](MANUAL.md#empresa-prisma---empresa).

## Analytics (`prisma --analytics`)

Para uma camada de **análise financeira** sobre os seus dados pessoais — sem risco de mexer em nada — use `prisma --analytics`. Ele abre uma TUI exclusiva (com o selo **ANALYTICS** ao lado do logo) que **só lê** o banco: a conexão é aberta em modo somente-leitura, então nenhuma inserção, edição ou exclusão é possível, e não há formulários de lançamento. O módulo só interpreta o histórico que você já registrou no Prisma normal.

```sh
prisma --analytics                    # abre o painel de análise (somente leitura)
```

As telas (navegue com `↑/↓` e `enter`, volte com `esc`):

| Tela | O que mostra |
|---|---|
| **Health Score** | índice 0–100 de saúde financeira (poupança + fundo de emergência + constância do fluxo) |
| **Modo Economia** | categorias com gasto atípico no mês (desvio padrão e médias do histórico) |
| **Sazonalidade** | meses do calendário historicamente mais caros e avisos dos próximos |
| **Runway** | projeção de saldo em 30/90/180 dias, *burn rate* e meses até zerar |
| **Metas** | viabilidade de uma meta (valor + prazo) frente ao superávit; sugere cortes |
| **Assinaturas Ocultas** | despesas repetidas que parecem assinaturas, com impacto anual |
| **Simulador** | *what-if*: perda de renda / nova despesa recalculando fluxo e runway (em memória) |
| **Inflação Pessoal** | quanto o seu custo de vida básico subiu, ano a ano |
| **Regra 50/30/20** | necessidades × desejos × poupança frente ao padrão ideal |
| **Patrimônio Líquido** | ativos − dívidas e a evolução mês a mês |
| **Eficiência** | consumo das contas de utilidade (luz, água...) e picos |

Em **Metas** (tecla `m`) e **Simulador** (tecla `s`) você informa os parâmetros por um formulário — a análise roda só em memória, nada é gravado. O Analytics lê o **banco pessoal** (não combina com `--empresa`). Detalhes no [MANUAL.md](MANUAL.md#analytics-prisma---analytics).

## Compartilhamento (vários dispositivos)

Um casal ou família pode usar o **mesmo banco** a partir de máquinas diferentes na rede de casa. Uma máquina vira **servidor** (dona do arquivo) e disponibiliza o banco na rede local; as outras conectam como **cliente** e operam sem perceber que o banco está em outro lugar — TUI, CLI e bot funcionam igual.

Na máquina que guarda o banco:

```sh
prisma servidor --token UMSEGREDO   # fica no ar e imprime o comando de pareamento
```

Ela continua usando o banco normalmente (modo local); o `servidor` só é um processo a mais. Na outra máquina, cole o comando que o servidor mostrou:

```sh
prisma config cliente --host 192.168.0.71 --token UMSEGREDO --fingerprint 97e3...
prisma saldo                        # agora lê e escreve no banco do servidor
prisma config local                 # volta a usar o banco local desta máquina
```

A conexão é **criptografada por padrão** (TLS com certificado autoassinado e verificação por _fingerprint_) e protegida por um **token** combinado entre os dois lados. É pensado para a rede de casa (LAN). Detalhes no [MANUAL.md](MANUAL.md#compartilhamento-entre-dispositivos-clienteservidor).

## Dados

O banco fica no diretório de dados padrão de cada sistema:

- Linux: `~/.local/share/prisma/prisma.db`
- macOS: `~/Library/Application Support/prisma/prisma.db`
- Windows: `%AppData%\prisma\prisma.db`

A cada dia de uso, o Prisma guarda sozinho uma cópia do banco em `backups/` ao lado dele (as 7 mais recentes). Para usar outro arquivo, use a variável `PRISMA_DB`:

```sh
PRISMA_DB=/tmp/teste.db prisma saldo
```

## Arquitetura

```
cmd/prisma/        ponto de entrada: sem argumentos abre a TUI, com
                   argumentos roteia para os comandos
internal/db/       abertura do SQLite e schema (migrações idempotentes)
internal/money/    dinheiro em centavos (int64) — parse e formatação pt-BR
internal/app/      um arquivo por funcionalidade: conta, carteira, grupo,
                   cartao, lancamento, recorrencia, assinatura, emergencia,
                   plano, relatorio, graficos, previsao, simular, transferir
internal/tui/      interface de terminal (Bubble Tea): cabeçalho em ASCII
                   art, menu, telas e formulários — as telas capturam a
                   saída dos comandos da CLI, reaproveitando toda a lógica;
                   a interface web (--web) serve essas mesmas telas no
                   navegador via API JSON + página única embutida no binário
                   (os gráficos viram SVG)
internal/bot/      bot de Telegram (long polling, só stdlib): traduz
                   mensagens de texto em lançamentos; roda como serviço
internal/remote/   modo cliente/servidor: driver database/sql remoto e
                   daemon HTTP que compartilham o banco na rede local
                   (TLS por fingerprint + token, só stdlib)
internal/update/   autoatualização do GitHub com conferência de SHA256

```

Decisões:

- **Go + SQLite puro-Go**: binário estático único, sem runtime nem dependências de sistema.
- **Centavos em `int64`**: nada de ponto flutuante para dinheiro.
- **Saldos calculados, não armazenados**: o saldo de uma conta é `saldo_inicial + lançamentos quitados vinculados ± transferências`, sempre consistente.
- **Transferência não é despesa**: tabela própria, não distorce relatórios.
- **Divisão por grupo no banco, não na exibição**: a "sua parte" é calculada na própria consulta (`valEf`), então saldo, relatórios, previsão e gráficos já concordam entre si.
- **Compra no cartão é um lançamento com vencimento na fatura**: não mexe no saldo do banco até a fatura ser paga; pagar a fatura quita o ciclo em bloco.
- **Recorrências materializadas na abertura**: cada execução gera os lançamentos pendentes dos próximos 3 meses, de forma idempotente. Assinaturas são recorrências marcadas.
- **Cliente/servidor no nível do `database/sql`**: como tudo recebe um `*sql.DB`, o modo cliente apenas troca o driver por um que fala com o servidor pela rede — o `app/`, a TUI e o bot não mudam. Cada conexão do cliente segura uma `*sql.Conn` dedicada no servidor, preservando a semântica de transação.
- **Somente biblioteca padrão** além do driver SQLite e do Bubble Tea (TUI).

Testes: `go test ./...` cobre dinheiro (parse/format), períodos ISO, simulação de dívida, parcelas, transferências, recorrências, divisão por grupo, ciclo de fatura do cartão, edição e importação OFX/CSV.

## Licença

© 2026 João Pedro Barbosa. Distribuído sob a licença [GPL-3.0](LICENSE): use, estude e modifique à vontade; quem distribuir uma versão modificada deve mantê-la sob a mesma licença, com o código aberto.
