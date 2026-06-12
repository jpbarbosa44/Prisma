# Manual de uso do Prisma

Guia completo de todas as funcionalidades. Para instalar, veja [INSTALL.md](INSTALL.md).

## Índice

1. [Conceitos](#conceitos)
2. [Formatos aceitos](#formatos-aceitos)
3. [A interface de terminal (TUI)](#a-interface-de-terminal-tui)
4. [Comandos](#comandos)
5. [Receitas prontas](#receitas-prontas)
6. [Dados e backup](#dados-e-backup)

---

## Conceitos

| Conceito | O que é |
|---|---|
| **Conta** | Conta bancária: corrente, poupança ou investimento. |
| **Carteira** | Dinheiro fora do banco: físico, vale-refeição, cofrinho. |
| **Lançamento** | Algo *a pagar* ou *a receber*: tem valor, vencimento, categoria e status (`pendente` → `quitado`). Pode ser vinculado a uma conta ou carteira; ao quitar, o saldo dela muda. |
| **Transferência** | Dinheiro movido entre contas/carteiras. Não é receita nem despesa — não aparece nos relatórios de gasto. |
| **Recorrência** | Regra tipo "salário todo dia 1": gera os lançamentos sozinha, 3 meses à frente, sempre que o Prisma roda. |
| **Emergência** | Uma dívida cadastrada com juros e aporte mensal; o Prisma monta o plano de ação mês a mês para quitá-la. |
| **Plano** | Limite de gasto de uma categoria em uma semana ou mês ("até R$ 800 de mercado em junho"). |
| **Categoria** | Etiqueta livre dos lançamentos (`moradia`, `mercado`...). Categorias novas geram aviso, para pegar erros de digitação. |

**Saldos são sempre calculados**, nunca armazenados: saldo da conta = saldo inicial + lançamentos quitados vinculados ± transferências. Não há como "dessincronizar".

## Formatos aceitos

- **Valores:** `1234`, `1234,56`, `1.234,56`, `1234.56`, com `R$` opcional.
- **Datas:** `AAAA-MM-DD` ou `DD/MM/AAAA`; a palavra `hoje` também vale.
- **Meses:** `AAAA-MM` (ex.: `2026-06`).
- **Semanas:** `AAAA-Wnn` no padrão ISO (ex.: `2026-W24`, segunda a domingo).
- **Locais de dinheiro** (em transferências): `conta:ID` ou `carteira:ID`.

## A interface de terminal (TUI)

Digite `prisma` sem argumentos. Navegação geral:

| Tecla | Faz |
|---|---|
| `↑/↓` ou `k/j` | Move o cursor (menu ou linhas da tabela) |
| `enter` ou `1`-`9` | Abre a tela |
| `esc` | Volta (tela → menu; formulário → tela) |
| `q` | Volta / sai (no menu) |
| `pgup/pgdn` | Rola conteúdos longos |

Dentro das telas, os atalhos aparecem no rodapé. Convenções:

- `a` adiciona, `e` edita, `x` remove (com confirmação `s/n`), `l` volta à lista.
- Nas tabelas, a linha selecionada (destacada) fornece o **id automaticamente** para ações como quitar, editar e remover — o campo já vem preenchido no formulário.
- Nos formulários: `enter` avança/confirma, `tab` próximo campo, `esc` cancela. Em edições, **campos vazios mantêm o valor atual**.
- Campos de escolha (conta, carteira, tipo, período, sim/não) são **seletores**: `←/→` percorre as opções — as contas e carteiras aparecem **listadas pelo nome**, sem precisar saber o id.
- Campos marcados com `*` são **obrigatórios**: o formulário não confirma enquanto estiverem vazios.

Telas e seus atalhos específicos:

| Tela | Atalhos |
|---|---|
| 1 Saldo | `t` transferir, `z` zerar banco |
| 2 Contas | `a` `e` `t` extrato `l` `x` |
| 3 Carteiras | `a` `e` `t` extrato `l` `x` |
| 4 Pagar/Receber | `p` a pagar, `r` a receber, `u` quitar, `e` editar, `x`, `f` filtrar |
| 5 Recorrências | `a` `e` `x` |
| 6 Emergência | `a`, `p` ver plano, `e`, `l`, `u` quitar, `x` |
| 7 Planejamento | `a`, `e`, `s` status, `l`, `x` |
| 8 Relatório | `m` meses |
| 9 Previsão | `m` meses |

## Comandos

Tudo da TUI também existe na linha de comando — útil para scripts. `prisma ajuda` resume.

### conta / carteira

```sh
prisma conta add --nome "Nubank" --banco "Nubank" --tipo corrente --saldo 1.500,00
prisma conta listar
prisma conta editar 1 --nome "Nubank PJ" --saldo 2.000,00   # só altera o que for passado
prisma conta remover 1

prisma carteira add --nome "Dinheiro" --desc "físico" --saldo 200
prisma carteira editar 1 --desc "carteira física"
```

`--tipo`: `corrente` (padrão), `poupanca` ou `investimento`. Em `editar`, `--saldo` redefine o saldo **inicial**; o atual é recalculado. Remover uma conta mantém os lançamentos dela (ficam sem vínculo).

### pagar / receber

```sh
prisma pagar add --desc "Aluguel" --valor 1200 --venc 05/07/2026 --cat moradia --conta 1
prisma pagar add --desc "Energia" --valor 180 --repetir 12        # repete o valor por 12 meses
prisma pagar add --desc "Notebook" --valor 3.600,00 --parcelas 10 # divide o TOTAL em 10x
prisma pagar add --desc "Padaria" --valor 15 --quitado            # já pago (histórico)
prisma receber add --desc "Freela" --valor 800 --venc 20/07/2026 --conta 1
```

- `--repetir N` cria N cópias mensais com o mesmo valor; `--parcelas N` divide o total (última parcela absorve o resto da divisão). Não combine os dois.
- Dia 31 em mês curto vira o último dia do mês.

### lancamentos (listar, filtrar, editar, remover)

```sh
prisma lancamentos                          # tudo
prisma lancamentos --pendentes              # só o que falta pagar/receber
prisma lancamentos --tipo pagar --cat moradia
prisma lancamentos --mes 2026-07            # por mês de vencimento
prisma lancamentos --de 01/06/2026 --ate 15/06/2026   # por intervalo de datas
prisma lancamentos editar 7 --valor 1.250,00 --venc 10/07/2026
prisma lancamentos editar 7 --conta 2       # vincula à conta 2 (--conta 0 desvincula)
prisma lancamentos remover 7
```

Os filtros se combinam. A lista mostra ao final os totais pendentes a pagar e a receber.

### quitar

```sh
prisma quitar 4                 # hoje
prisma quitar 4 --data 10/06/2026
```

Ao quitar, o lançamento entra no saldo da conta/carteira vinculada e nos relatórios.

### transferir

```sh
prisma transferir --de conta:1 --para carteira:1 --valor 200
prisma transferir --de carteira:1 --para conta:2 --valor 50 --data 09/06/2026 --desc "depósito"
```

### recorrencia

```sh
prisma recorrencia add --tipo receber --desc "Salário" --valor 5000 --dia 1 --conta 1
prisma recorrencia add --tipo pagar --desc "Aluguel" --valor 1300 --dia 10 --fim 31/12/2027
prisma recorrencia listar
prisma recorrencia editar 1 --valor 5.500,00 --dia 5   # ajusta a regra E os pendentes gerados
prisma recorrencia editar 2 --fim nunca                # remove a data de término
prisma recorrencia remover 1 --limpar                  # apaga a regra e os pendentes gerados
```

A cada execução do Prisma, as regras materializam lançamentos pendentes até 3 meses à frente (sem duplicar). Editar uma regra atualiza também os pendentes já gerados — os quitados nunca são tocados.

### emergencia

```sh
prisma emergencia add --desc "Cartão de crédito" --credor "Banco X" --valor 8000 --juros 12 --aporte 1500
prisma emergencia listar          # mostra em quantos meses cada dívida é quitada
prisma emergencia plano 1         # plano de ação mês a mês (juros, pagamento, saldo devedor)
prisma emergencia editar 1 --aporte 1.800,00   # recalcula e reexibe o plano
prisma emergencia quitar 1
prisma emergencia remover 1
```

O plano simula juros compostos e mostra o total de juros pagos e a economia de acelerar o aporte em 20%. Se o aporte não cobre nem os juros do primeiro mês, o Prisma recusa e diz o mínimo necessário. Os aportes aparecem na coluna DÍVIDAS da previsão.

### plano (planejamento)

```sh
prisma plano add --cat mercado --valor 800                  # mês atual
prisma plano add --cat lazer --valor 100 --periodo semana --repetir 4
prisma plano add --cat mercado --valor 900 --ref 2026-07    # mês específico
prisma plano status                                         # uso x limite do mês atual
prisma plano status --periodo semana --ref 2026-W30
prisma plano editar 3 --valor 850,00
prisma plano remover 3
```

O gasto considera lançamentos *a pagar* da categoria: quitados dentro do período + pendentes com vencimento nele. Limite estourado gera alerta ⚠.

### relatorio / extrato

```sh
prisma relatorio --meses 6     # receitas x despesas, taxa de poupança,
                               # gastos por categoria (com barras), balanço mês a mês
prisma extrato --conta 1 --meses 3    # movimentação com saldo corrente
prisma extrato --carteira 1
```

O extrato inclui transferências (marcadas com ⇄) e mostra o saldo após cada movimento.

### previsao

```sh
prisma previsao --meses 6
```

Para cada mês futuro: lançamentos pendentes agendados; se um mês não tem nada agendado de um tipo, usa a média dos últimos 3 meses (marcado com `~`). A coluna DÍVIDAS desconta os aportes das emergências ativas. Termina com um gráfico de barras do saldo projetado e avisa se ele ficar negativo.

### saldo

```sh
prisma saldo    # contas, carteiras, total, pendências, dívidas e posição líquida
```

### exportar / importar

```sh
prisma exportar                          # prisma-lancamentos.csv no diretório atual
prisma exportar --saida jun.csv --mes 2026-06
prisma importar --arquivo extrato.ofx --conta 1
prisma importar --arquivo extrato.csv --carteira 1 --cat mercado
```

- Exporta CSV com `;` e vírgula decimal (abre direto no Excel/LibreOffice pt-BR).
- Importa OFX (formato padrão dos bancos) ou CSV com colunas data, descrição e valor (negativo = pagamento). Os movimentos entram **quitados** na conta indicada, com categoria `importado` (mude com `--cat`). Reimportar o mesmo arquivo não duplica.

### bot (Telegram)

Registra gastos e receitas mandando mensagem para um bot do Telegram — útil para anotar na hora, pelo celular. Configuração (uma vez só):

1. No Telegram, fale com o **@BotFather**, mande `/newbot` e copie o token.
2. `prisma bot --token SEU_TOKEN` — o bot conecta e fica aguardando (o token fica salvo em `telegram.json`, ao lado do banco).
3. Mande qualquer mensagem ao seu bot; ele responde com o seu *chat id*.
4. Pare o bot (Ctrl+C) e rode `prisma bot --chat SEU_CHAT_ID`. Pronto: daqui em diante basta `prisma bot`.

Por segurança, o bot só aceita lançamentos do chat autorizado — mensagens de qualquer outra pessoa são ignoradas. Ele usa *long polling*, então funciona em qualquer máquina com internet, sem IP público nem porta aberta. O processo precisa estar rodando para receber as mensagens (mensagens enviadas com o bot parado ficam na fila do Telegram por até 24h e são processadas quando ele voltar).

O formato das mensagens:

```
[+] valor [#categoria] [descrição] [marcadores]
```

| Elemento | Significado | Se omitido |
|---|---|---|
| `+` antes do valor | receita (a receber) | gasto (a pagar) |
| `25,50` | valor (formatos usuais do Prisma) | obrigatório |
| `#mercado` | categoria | `geral` |
| `@15`, `@15/07`, `@15/07/2026`, `@hoje`, `@ontem`, `@amanha` | vencimento (`@15` = dia 15 do mês atual) | hoje |
| `!` (token isolado) | já quitado | pendente |
| `3x` | divide o total em 3 parcelas mensais | à vista |
| `rep:6` | repete por 6 meses | não repete |
| `conta:2` / `cart:1` | vincula a conta ou carteira | sem vínculo |

O que não tem prefixo vira descrição; sem descrição, ela herda o nome da categoria. Os marcadores podem vir em qualquer ordem. Exemplos:

```
25,50 #mercado pão e leite !
+3500 #salario salário de junho @05/07
899,70 #eletronicos fone novo 3x
1200 #moradia aluguel @05 rep:6
12
```

Cada lançamento confirmado vem com um botão **🗑 Desfazer** que o remove na hora. Se a categoria tem um plano de gastos, a confirmação avisa quando o limite passa de 80% ou estoura. Mande `/ajuda` ao bot para ver o formato a qualquer momento.

Outras ações por mensagem:

| Mensagem | O que faz |
|---|---|
| `quitar 142` | marca o lançamento como pago/recebido |
| `corrigir 27,90` | conserta o **último** lançamento — aceita valor, `#categoria`, `@data`, `!` e nova descrição, na mesma gramática |
| `transferir 200 conta:1 cart:2 [descrição]` | move dinheiro entre conta/carteira |
| foto **com** legenda de lançamento | registra e guarda a foto como comprovante |
| foto **sem** legenda | anexa o comprovante ao último lançamento |
| `/comprovante 142` | reenvia o(s) comprovante(s) do lançamento |

O bot também responde consultas — a saída é a mesma dos comandos da CLI, em bloco monoespaçado:

| Mensagem | Equivale a |
|---|---|
| `/saldo` | `prisma saldo` |
| `/pendentes` | `prisma lancamentos --pendentes` |
| `/mes` | `prisma lancamentos --mes <mês atual>` |
| `/relatorio` | `prisma relatorio` |
| `/previsao` | `prisma previsao` |
| `/plano` | `prisma plano status` |
| `#mercado` (sozinha) | `prisma lancamentos --cat mercado --mes <mês atual>` |
| `#mercado maio` · `#mercado 3m` · `#mercado 2026-05` · `#mercado tudo` | a categoria em outros períodos |

E manda dois avisos automáticos por dia (enquanto estiver rodando):

- **9h — vencimentos**: lançamentos pendentes atrasados, de hoje e de amanhã, cada um com botão **✅ Quitar**;
- **20h — resumo do dia**: o que foi registrado e quitado no dia, mais o status dos planos de gasto.

Os comprovantes ficam armazenados no Telegram (o banco guarda só a referência), e a foto some se a conversa com o bot for apagada.

Para deixar o bot sempre ativo, rode-o como serviço do systemd (modo usuário):

```sh
systemd-run --user --unit=prisma-bot ~/.local/bin/prisma bot
```

## Receitas prontas

**Começando do zero:**

```sh
prisma conta add --nome "Banco" --saldo 2.500,00
prisma recorrencia add --tipo receber --desc "Salário" --valor 5000 --dia 5 --conta 1
prisma recorrencia add --tipo pagar --desc "Aluguel" --valor 1300 --dia 10 --conta 1
prisma plano add --cat mercado --valor 800
prisma previsao
```

**Fechando o mês:** `prisma lancamentos --pendentes --mes 2026-06`, quite o que foi pago (`prisma quitar N`), depois `prisma relatorio` e `prisma plano status`.

**Atacando uma dívida:** cadastre com `emergencia add`, siga o plano de ação pagando o aporte todo mês e acompanhe pela previsão; ao final, `emergencia quitar`.

**Conferindo com o banco:** baixe o OFX no internet banking, `prisma importar --arquivo extrato.ofx --conta 1`, depois `prisma extrato --conta 1`.

### resetar

Apaga **todos** os dados e volta o Prisma ao estado recém-instalado:

```sh
prisma resetar                 # mostra o que será perdido e exige digitar "apagar"
prisma resetar --sim           # sem confirmação (scripts — cuidado!)
prisma resetar --sem-backup    # não cria a cópia de segurança
```

Antes de zerar, uma cópia do banco é salva ao lado do original (`prisma.db.bak-AAAAMMDD-HHMMSS`) — para desfazer, basta renomeá-la de volta para `prisma.db`. Na TUI: tecla `z` na tela Saldo, digitando "apagar" e confirmando com `s`.

## Dados e backup

Um único arquivo SQLite:

| Sistema | Caminho |
|---|---|
| Linux | `~/.local/share/prisma/prisma.db` |
| macOS | `~/Library/Application Support/prisma/prisma.db` |
| Windows | `%AppData%\prisma\prisma.db` |

- **Backup automático**: a cada dia de uso, uma cópia do banco é salva em `backups/` ao lado dele (ex.: `backups/prisma-2026-06-12.db`), antes da primeira sessão do dia; as 7 mais recentes ficam guardadas. **Restaurar** = copiar a cópia de volta sobre o `prisma.db`.
- O backup protege contra erro e corrupção local, mas mora no mesmo disco — para proteção real contra perda da máquina, copie a pasta de vez em quando para um pendrive ou nuvem.
- A variável `PRISMA_DB` aponta para outro arquivo — útil para testar sem mexer nos seus dados: `PRISMA_DB=/tmp/teste.db prisma`.
- O Prisma funciona 100% offline; nada sai da sua máquina.
