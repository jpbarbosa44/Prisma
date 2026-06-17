# Manual de uso do Prisma

Guia completo de todas as funcionalidades. Para instalar, veja [INSTALL.md](INSTALL.md).

## Índice

1. [Conceitos](#conceitos)
2. [Formatos aceitos](#formatos-aceitos)
3. [A interface de terminal (TUI)](#a-interface-de-terminal-tui)
4. [A interface web (--web)](#a-interface-web---web)
5. [Comandos](#comandos)
6. [Receitas prontas](#receitas-prontas)
7. [Compartilhamento (cliente/servidor)](#compartilhamento-entre-dispositivos-clienteservidor)
8. [Dados e backup](#dados-e-backup)

---

## Conceitos

| Conceito | O que é |
|---|---|
| **Conta** | Conta bancária: corrente, poupança ou investimento. |
| **Carteira** | Dinheiro fora do banco: físico, vale-refeição, cofrinho. |
| **Lançamento** | Algo *a pagar* ou *a receber*: tem valor, vencimento, categoria, observação e status (`pendente` → `quitado`). Pode ser vinculado a uma conta ou carteira; ao quitar, o saldo dela muda. Com `--auto-quitar`, quita-se sozinho quando o vencimento chega. |
| **Transferência** | Dinheiro movido entre contas/carteiras. Não é receita nem despesa — não aparece nos relatórios de gasto. |
| **Recorrência** | Regra tipo "salário todo dia 1": gera os lançamentos sozinha, 3 meses à frente, sempre que o Prisma roda. Pode dividir por grupo e quitar sozinha no vencimento. |
| **Emergência** | Uma dívida cadastrada com juros e aporte mensal; o Prisma monta o plano de ação mês a mês para quitá-la. |
| **Plano** | Limite de gasto de uma categoria em uma semana ou mês ("até R$ 800 de mercado em junho"). |
| **Grupo** | Pessoas que dividem despesas (ex.: "Eu e a Maria"). Uma despesa vinculada a um grupo conta, em todo o sistema, só pela **sua parte**: o valor cheio dividido pelo número de pessoas. |
| **Cartão / Fatura** | Cartão de crédito: você gasta agora e paga depois. Um gasto de cartão é um lançamento cujo **vencimento é a data da fatura** (calculada do ciclo do cartão), então ele não mexe no saldo do banco até a fatura ser paga. Pagar a fatura quita os gastos do ciclo de uma vez, debitando a conta do cartão. |
| **Categoria** | Etiqueta dos lançamentos (`moradia`, `mercado`...). Há um **catálogo**: categorias usadas entram nele sozinhas e novas geram aviso (pega erros de digitação). Dá para gerenciá-las em `prisma categoria`. |
| **Estatísticas** | Análise estatística do histórico quitado: média/mediana por categoria, tendência e variação, top gastos e recorrentes, e projeção/saúde financeira. |

**Saldos são sempre calculados**, nunca armazenados: saldo da conta = saldo inicial + lançamentos quitados vinculados ± transferências. Não há como "dessincronizar".

## Formatos aceitos

- **Valores:** `1234`, `1234,56`, `1.234,56`, `1234.56`, com `R$` opcional.
- **Datas:** `AAAA-MM-DD`, `DD/MM/AAAA` ou `DD/MM` (assume o ano vigente); a palavra `hoje` também vale.
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
- Nos formulários: `enter` avança/confirma, `tab` próximo campo, `esc` cancela. **Em edições, os campos já abrem com os valores atuais do registro** (vindos do banco) — é só ajustar o que quiser; apagar um campo de texto mantém o valor anterior.
- Campos de escolha (conta, carteira, tipo, período, sim/não) são **seletores**: `←/→` percorre as opções — as contas e carteiras aparecem **listadas pelo nome**, sem precisar saber o id.
- O campo **categoria** é um combo: digite para criar uma nova ou use `←/→` para navegar pelas já existentes (do catálogo).
- A mensagem de confirmação (verde) ou de erro (vermelha) **some sozinha depois de 10 segundos**.
- Campos marcados com `*` são **obrigatórios**: o formulário não confirma enquanto estiverem vazios.
- **Repetir** e **parcelas** são mutuamente exclusivos: o formulário avisa se você preencher os dois.

Telas e seus atalhos específicos:

| Tela | Atalhos |
|---|---|
| 1 Saldo | `t` transferir, `z` zerar banco |
| 2 Contas | `a` `e` `t` extrato `l` `x` |
| 3 Carteiras | `a` `e` `t` extrato `l` `x` |
| 4 Grupos | `a` `e` `v` ver despesas, `l` `x` |
| 5 Categorias | `a` `e` renomear `x` |
| 6 Cartões | `a` `e` `t` ver fatura, `p` pagar fatura, `l` `x` |
| 7 Pagar/Receber | `p` a pagar, `r` a receber, `u` quitar, `e` editar, `x`, `f` filtrar |
| 8 Recorrências | `a` `e` `f` filtrar `x` |
| 9 Assinaturas | `a` `e` `x` |
| 10 Emergência | `a`, `p` ver plano, `e`, `l`, `u` quitar, `x` |
| 11 Planejamento | `a`, `e`, `s` status, `l`, `x` |
| 12 Relatório | `m` meses |
| 13 Estatísticas | `m` meses |
| 14 Gráficos | `m` meses |
| 15 Previsão | `m` meses |
| 16 Simulação | `s` simular |
| 17 Como usar | — (só documentação) |

## A interface web (--web)

As mesmas telas da TUI, no navegador:

```sh
prisma --web                      # sobe o servidor e abre o navegador
prisma --web --porta 8080         # outra porta (padrão: 7747)
prisma --web --sem-abrir          # só sobe o servidor, sem abrir o navegador
```

O servidor escuta **somente em `127.0.0.1`** — nada fica acessível pela rede e nenhum dado sai da sua máquina. A página inteira vai embutida no binário; não há dependências nem arquivos extras. `ctrl+c` no terminal encerra.

A navegação espelha a TUI:

- O menu lateral lista as mesmas telas (`1`-`9` trocam de tela pelo teclado; as demais por clique).
- As teclas dos botões de ação funcionam como atalhos (`a` adiciona, `e` edita, `x` remove...).
- `↑/↓` ou um clique selecionam a linha da tabela, e o id selecionado já vem preenchido nos formulários de quitar, editar e remover.
- Nos formulários, `enter` confirma e `esc` cancela; campos com `*` são obrigatórios; em edições, campos vazios mantêm o valor atual.
- Remoções e o "zerar banco" pedem confirmação, como na TUI.

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

### grupo

Grupos reúnem pessoas que dividem despesas. Uma despesa vinculada a um grupo passa a contar, em **todo** o sistema (saldo, extrato, relatório, planejamento, previsão), apenas pela **sua parte** — o valor cheio dividido pelo número de pessoas.

```sh
prisma grupo add --nome "Eu e a Maria" --pessoas "Eu, Maria"   # mínimo 2 pessoas
prisma grupo listar                                            # pessoas, total vinculado e gasto do mês atual
prisma grupo editar 1 --nome "Casa" --pessoas "Eu, Maria, João"  # --pessoas substitui a lista
prisma lancamentos --grupo 1                                   # vê os lançamentos vinculados ao grupo
prisma grupo remover 1                                         # despesas voltam ao valor cheio
```

- O número de pessoas é o divisor. Uma despesa de R$ 300 num grupo de 2 pesa **R$ 150** no seu bolso.
- Vincule a despesa com `--grupo N` (veja abaixo). Remover o grupo **não apaga** os lançamentos: eles só deixam de ser divididos.
- O `listar` mostra, além do total vinculado, a **soma do mês vigente** (a sua parte). Na TUI, a ação **`v` ver despesas** abre a lista de lançamentos do grupo.

### categoria

Catálogo de categorias. É opcional gerenciá-lo à mão: ao usar uma categoria nova num lançamento ou recorrência, ela já entra no catálogo. Serve para a interface sugerir/navegar e para organizar a lista.

```sh
prisma categoria add --nome mercado
prisma categoria listar                       # cada categoria + nº de lançamentos
prisma categoria editar 3 --nome supermercado # renomeia e atualiza lançamentos e recorrências
prisma categoria remover 5                    # tira do catálogo (os lançamentos ficam intactos)
```

- A categoria no lançamento continua sendo **texto livre**; o catálogo é só um índice. Por isso remover do catálogo não afeta os lançamentos.
- `editar` propaga o novo nome para os lançamentos e recorrências que usavam o nome antigo.

### cartao / fatura

Cartão de crédito: você gasta agora e paga depois, na fatura. Um gasto de cartão é um lançamento `pagar` cujo **vencimento é a data da fatura** (calculada do ciclo do cartão) — por isso ele **não mexe no saldo do banco até a fatura ser paga**, mas já aparece em "pendente a pagar" e na Previsão do mês da fatura.

```sh
prisma cartao add --nome "Nubank" --fechamento 20 --vencimento 27 --conta 1 --fatura-atual 1.200,00
prisma cartao listar                                # mostra ciclo, conta e a fatura em aberto
prisma pagar add --desc "Tênis" --valor 400 --parcelas 4 --cartao 1 --venc 10/06/2026
prisma fatura --cartao 1                            # ver a fatura aberta (ou --ref 2026-07)
prisma fatura pagar --cartao 1                      # quita o ciclo e debita a conta do cartão
```

- **Data da compra:** com `--cartao`, a data informada (`--venc`) é a **data da compra**; o Prisma calcula em qual fatura ela cai (compra após o fechamento vai pra próxima).
- **Competência:** o gasto conta nos relatórios e no planejamento pela **data da compra**, não pela data do pagamento da fatura.
- **Parcelas:** `--parcelas N` espalha a compra por N faturas consecutivas (uma parcela por mês).
- **Fatura inicial:** `--fatura-atual` lança o que já está em aberto hoje, pra você não precisar recadastrar o passado.
- **Pagar a fatura** quita todos os gastos do ciclo de uma vez e debita a conta do cartão (ou outra, com `--conta`). A fatura é identificada pelo mês do vencimento (`--ref AAAA-MM`); sem `--ref`, é a aberta.
- Grupos compõem com cartão: um gasto de cartão dividido conta pela **sua parte** também na fatura.
- **Remover o cartão apaga junto os lançamentos de despesa vinculados a ele** (eles não fazem sentido sem o cartão) — diferente de remover uma conta, que só desvincula.

### pagar / receber

```sh
prisma pagar add --desc "Aluguel" --valor 1200 --venc 05/07/2026 --cat moradia --conta 1
prisma pagar add --desc "Energia" --valor 180 --repetir 12        # repete o valor por 12 meses
prisma pagar add --desc "Notebook" --valor 3.600,00 --parcelas 10 # divide o TOTAL em 10x
prisma pagar add --desc "Mercado" --valor 300 --grupo 1           # divide com o grupo: conta R$ 150
prisma pagar add --desc "Tênis" --valor 400 --parcelas 4 --cartao 1  # 4x na fatura do cartão
prisma pagar add --desc "IPTU" --valor 600 --venc 10/03 --auto-quitar --obs "cota única"
prisma pagar add --desc "Padaria" --valor 15 --quitado            # já pago (histórico)
prisma receber add --desc "Freela" --valor 800 --venc 20/07/2026 --conta 1
```

- `--repetir N` cria N cópias mensais com o mesmo valor; `--parcelas N` divide o total (última parcela absorve o resto da divisão). Não combine os dois.
- `--grupo N` vincula a despesa a um grupo; o valor que pesa no sistema é o cheio dividido pelas pessoas do grupo.
- `--cartao N` lança no cartão: a data vira a da compra e o gasto vai pra fatura (veja a seção *cartao / fatura*).
- `--obs "texto"` guarda uma observação livre (aparece na coluna OBS da listagem).
- `--auto-quitar` faz o lançamento quitar-se sozinho quando o vencimento chega (a varredura roda toda vez que o Prisma ou o bot executa). Na listagem, esses itens são marcados com `⏱`.
- **Parcelas ligadas:** remover a **parcela raiz** (a 1ª) apaga todas as parcelas do grupo de uma vez; remover qualquer outra apaga só ela.
- Dia 31 em mês curto vira o último dia do mês.

### lancamentos (listar, filtrar, editar, remover)

```sh
prisma lancamentos                          # tudo
prisma lancamentos --pendentes              # só o que falta pagar/receber
prisma lancamentos --tipo pagar --cat moradia
prisma lancamentos --mes 2026-07            # por mês de vencimento
prisma lancamentos --de 01/06/2026 --ate 15/06/2026   # por intervalo de datas
prisma lancamentos --grupo 1                # só os vinculados ao grupo 1
prisma lancamentos editar 7 --valor 1.250,00 --venc 10/07/2026
prisma lancamentos editar 7 --conta 2       # vincula à conta 2 (--conta 0 desvincula)
prisma lancamentos editar 7 --grupo 1       # vincula ao grupo 1 (--grupo 0 desvincula)
prisma lancamentos editar 7 --obs "parcela final" --auto-quitar sim   # observação e auto-quitar (sim|nao; "-" limpa a obs)
prisma lancamentos remover 7                # se for a parcela raiz, remove TODAS as parcelas
```

Os filtros se combinam. A lista mostra ao final os totais pendentes a pagar e a receber, e traz as colunas OBS (observação) e STATUS (com `⏱` quando o item quita sozinho no vencimento).

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
prisma recorrencia add --tipo pagar --desc "Internet" --valor 120 --dia 12 --cartao 1   # na fatura
prisma recorrencia add --tipo pagar --desc "Faxina" --valor 200 --dia 5 --grupo 1 --auto-quitar
prisma recorrencia listar --vigentes --tipo pagar      # filtra (esconde encerradas)
prisma recorrencia editar 1 --valor 5.500,00 --dia 5   # ajusta a regra E os pendentes gerados
prisma recorrencia editar 2 --fim nunca                # remove a data de término
prisma recorrencia remover 1 --limpar                  # apaga a regra e os pendentes gerados
```

A cada execução do Prisma, as regras materializam lançamentos pendentes até 3 meses à frente (sem duplicar). Editar uma regra atualiza também os pendentes já gerados — os quitados nunca são tocados.

- **`--cartao N`** liga a recorrência a um cartão: cada ocorrência cai na fatura (o dia da regra vira a data da compra e o vencimento, o da fatura). Não combine com `--conta`/`--carteira` — quem paga é a conta do cartão.
- **`--grupo N`** divide cada ocorrência pelas pessoas do grupo: os lançamentos gerados já contam só a sua parte.
- **`--auto-quitar`** faz os lançamentos gerados quitarem-se sozinhos no vencimento.
- **`--inicio` no passado:** se a regra começa antes de hoje, o Prisma pergunta se as ocorrências anteriores entram já **quitadas**. Sem terminal interativo (bot/TUI), use `--passados quitar|manter`.
- **Listar:** com início **e** fim definidos, mostra a coluna RESTANTES (quantas ocorrências ainda faltam) e a coluna GRUPO. Filtros: `--tipo pagar|receber`, `--vigentes` (esconde as encerradas) e `--assinaturas` (só assinaturas).

### assinatura

Assinaturas (Netflix, Spotify, academia...) são recorrências de despesa marcadas como tal — em geral cobradas no cartão. A tela/comando *Assinaturas* mostra só elas e soma o **custo mensal**.

```sh
prisma assinaturas add --desc "Netflix" --valor 39,90 --dia 15 --cartao 1
prisma assinaturas add --desc "Academia" --valor 89,90 --dia 5 --conta 1
prisma assinaturas add --desc "Spotify Família" --valor 34,90 --dia 10 --conta 1 --grupo 1  # dividida
prisma assinaturas listar                              # lista + total mensal (e cobranças restantes, se tiver fim)
prisma assinaturas editar 1 --valor 44,90              # mesma engine da recorrência
prisma assinaturas remover 1 --limpar
```

- `add` já assume `--tipo pagar` e marca `--assinatura`; o resto dos campos é igual ao da recorrência (inclusive `--grupo N` para dividir).
- Como toda assinatura é uma recorrência, ela também aparece em `prisma recorrencia listar` (marcada com *(assinatura)*) e gera os lançamentos mensais normalmente.

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

### estatisticas

Análise estatística mais profunda do histórico **quitado** (competência pela data da compra/quitação):

```sh
prisma estatisticas --meses 6     # janela analisada (1 a 36; padrão 6)
```

Quatro blocos:

1. **Resumo por categoria** — para cada categoria de despesa: total, média/mês, mediana, maior e menor mês, e % do total.
2. **Tendência e variação** — despesa do mês x mês anterior, média móvel (até 3 meses) e alerta das categorias que ficaram acima da própria média histórica neste mês.
3. **Top gastos e recorrentes** — os maiores lançamentos do período e as despesas repetidas (mesma descrição/valor em 3+ meses) que ainda não são recorrência — candidatas a virar uma.
4. **Projeção e saúde financeira** — sobra (receitas − despesas), taxa de poupança, sobra média por mês, projeção do saldo em 6 meses e os "meses de fôlego" (saldo ÷ despesa média).

### graficos

```sh
prisma graficos --meses 6
```

Quatro gráficos do período: **gastos por categoria**, **receitas × despesas por mês**, **evolução do saldo** e **despesa por grupo** (sua parte sobre o total cheio). No terminal e no bot saem em ASCII; na interface web (`prisma --web`, tela *Gráficos*) viram gráficos visuais em SVG. Todos os valores já refletem a divisão por grupo.

### previsao

```sh
prisma previsao --meses 6
```

Para cada mês futuro: lançamentos pendentes agendados; se um mês não tem nada agendado de um tipo, usa a média dos últimos 3 meses (marcado com `~`). A coluna DÍVIDAS desconta os aportes das emergências ativas. Termina com um gráfico de barras do saldo projetado e avisa se ele ficar negativo.

### simular

```sh
prisma simular --desc "Videogame" --valor 4000 --parcelas 12
prisma simular --valor 3000 --parcelas 10 --juros 3        # parcelamento com juros (Tabela Price)
prisma simular --valor 4000 --parcelas 6 --entrada 800     # com entrada à vista
```

Responde **"e se eu comprar isto?"** sem gravar nada. Usa o mesmo modelo da previsão (recorrências, pendentes e média histórica) e projeta o saldo mês a mês **com e sem a compra**, pelo prazo do parcelamento. No fim, dá um veredito:

- 🟢 **Pode comprar** — o saldo nunca cai abaixo de uma folga saudável.
- ⚠ **Arriscado** — dá pra comprar, mas a folga cai abaixo de um mês de despesas (sem reserva para imprevistos).
- 🔴 **Não recomendado** — o saldo ficaria negativo em algum mês do parcelamento.

Com `--juros`, a parcela é calculada pela Tabela Price (parcela fixa) e a saída mostra o total pago e quanto é só de juros. No bot do Telegram: `/simular videogame 4000 12x 2% entrada:500`.

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

Registra gastos e receitas mandando mensagem para um bot do Telegram — útil para anotar na hora, pelo celular.

**Configuração inicial (recomendada — já deixa o bot sempre no ar):**

1. No Telegram, fale com o **@BotFather**, mande `/newbot` e copie o token.
2. `prisma bot --token SEU_TOKEN --instalar-servico` — salva o token (em `telegram.json`, ao lado do banco) e sobe o serviço já rodando.
3. Mande qualquer mensagem ao seu bot; ele responde com o seu *chat id*.
4. `prisma bot --chat SEU_CHAT_ID` — pareia o chat. Como o serviço está ativo, isso **salva e reinicia o serviço sozinho** (não abre um segundo bot). Pronto.

**Alternativa sem serviço (roda só enquanto o terminal estiver aberto):**

1–2. `prisma bot --token SEU_TOKEN` — conecta e fica aguardando no terminal.
3. Mande uma mensagem ao bot; ele responde o *chat id*.
4. `Ctrl+C`, depois `prisma bot --chat SEU_CHAT_ID`. Daí em diante, `prisma bot` roda o bot no terminal.

Por segurança, o bot só aceita lançamentos do chat autorizado — mensagens de qualquer outra pessoa são ignoradas. Ele usa *long polling*: conecta **de saída** no Telegram, então funciona de **qualquer rede**, sem IP público nem porta aberta — basta o processo estar de pé (mensagens enviadas com o bot parado ficam na fila do Telegram por até 24h). Como o Telegram só admite **um** poller por bot, rodar `prisma bot` no terminal enquanto o serviço está ativo é recusado (e mudar token/chat pelo terminal reinicia o serviço em vez de duplicar).

**O serviço** `--instalar-servico` (Linux/systemd) instala um serviço de usuário que sobe o bot junto com o computador, reinicia se cair e roda mesmo sem ninguém logado (habilita *linger*). A partir daí o bot responde de qualquer rede enquanto o PC estiver ligado e com internet. Útil:

```sh
systemctl --user status prisma-bot     # estado
journalctl --user -u prisma-bot -f     # logs ao vivo
prisma bot --remover-servico           # desinstala o serviço
```

> Se o bot "só responde quando estou em casa", quase sempre é porque o processo só estava de pé enquanto o terminal estava aberto. O serviço acima resolve isso. (Se o PC estiver **desligado**, nada roda — aí seria preciso hospedar o bot num servidor sempre ligado.)

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
| `grupo:1` | divide a despesa entre o grupo (veja `/grupos`) | sem grupo |
| `cartao:1` | lança no cartão; a data vira a da compra e vai pra fatura (veja `/cartoes`) | sem cartão |

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
| `/simular 4000 12x` | `prisma simular --valor 4000 --parcelas 12` (aceita `2%` de juros e `entrada:500`) |
| `/plano` | `prisma plano status` |
| `/grupos` | `prisma grupo listar` (ids para usar em `grupo:N`) |
| `/cartoes` | `prisma cartao listar` (ids para usar em `cartao:N`) |
| `/fatura 1` · `/fatura 1 2026-07` | `prisma fatura --cartao 1 [--ref ...]` |
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

### servidor

Disponibiliza o banco desta máquina na rede local, para outro Prisma usar como cliente.

```sh
prisma servidor --token UMSEGREDO            # fica no ar (Ctrl-C para parar)
prisma servidor --token X --porta 9000       # outra porta (padrão 8456)
prisma servidor --token X --sem-tls          # sem criptografia (só rede confiável)
```

Ao subir, imprime o comando `prisma config cliente ...` já preenchido (host, token e _fingerprint_) para você copiar e colar no outro computador. A máquina servidor continua usando o banco normalmente em modo local. Veja [Compartilhamento](#compartilhamento-entre-dispositivos-clienteservidor).

### config

Mostra ou troca o modo de operação (banco local **ou** cliente de um servidor).

```sh
prisma config                                # mostra o modo atual e o arquivo de config
prisma config cliente --host IP --token X --fingerprint Y   # conecta a um servidor e testa
prisma config local                          # volta ao banco local desta máquina
```

`config cliente` grava a configuração e já testa a conexão. `config local` desfaz (volta ao normal). As mesmas chaves podem ser definidas por variáveis de ambiente (`PRISMA_MODO`, `PRISMA_HOST`, `PRISMA_TOKEN`, `PRISMA_FINGERPRINT`), que têm prioridade sobre o arquivo.

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

## Compartilhamento entre dispositivos (cliente/servidor)

Permite que mais de uma pessoa (ou mais de um computador seu) use o **mesmo banco** — por exemplo, um casal lançando despesas em máquinas diferentes na rede de casa. Uma máquina é o **servidor**, dona do arquivo, e as outras são **clientes** que falam com ela pela rede local. Para o cliente, tudo funciona igual: TUI, CLI e bot não percebem diferença.

### Quem é o quê

- **Servidor**: a máquina que guarda o `prisma.db`. Ela roda o daemon e **também continua usando o banco normalmente** (modo local) — não precisa se configurar como cliente.
- **Cliente**: qualquer outra máquina, que não tem banco próprio e opera sobre o do servidor.

```
SERVIDOR (dono do banco)                       CLIENTE
  ├─ prisma servidor   ───────── rede ────────►  prisma  (modo cliente)
  │   (daemon, fica no ar)                         lê/escreve no banco do servidor
  └─ prisma            ← mesmo arquivo .db →       (não tem banco próprio)
      (uso local normal)
```

### Passo a passo

**1. No servidor**, suba o daemon com um token à sua escolha:

```sh
prisma servidor --token UMSEGREDO
```

Ele imprime os endereços da máquina e um comando pronto, por exemplo:

```
prisma config cliente --host 192.168.0.71 --token UMSEGREDO --fingerprint 97e3...
```

**2. No cliente**, cole esse comando. Ele grava a configuração e já testa a conexão:

```sh
prisma config cliente --host 192.168.0.71 --token UMSEGREDO --fingerprint 97e3...
# Modo cliente configurado. Testando conexão... ok!
prisma saldo        # agora vem do banco do servidor
```

**3. Para voltar** ao banco local desta máquina, a qualquer momento:

```sh
prisma config local
```

### Segurança

- **Criptografia (TLS) ligada por padrão.** O servidor gera um certificado autoassinado na primeira vez e mostra o seu _fingerprint_ (SHA-256); o cliente só confia no servidor cujo certificado bate com o _fingerprint_ fixado. Isso dá sigilo e protege contra interceptação, sem precisar de uma autoridade certificadora.
- **Token compartilhado.** Toda conexão exige o token combinado entre os dois lados; sem ele o servidor recusa.
- Pensado para a **rede local (LAN)** de uma casa. Para usar pela internet seria preciso liberar a porta no roteador — não recomendado sem um cuidado extra de segurança.
- Para testes em rede confiável, `--sem-tls` (no servidor) e `--sem-tls` (no `config cliente`) desligam a criptografia. Não use com dados reais fora de um ambiente controlado.

### Coisas a saber

- **São dois bancos diferentes.** Em modo local você vê o `prisma.db` desta máquina; em modo cliente, o do servidor. Trocar de modo só muda qual banco você enxerga — nada é copiado nem misturado, e o banco local fica intacto enquanto você usa como cliente.
- **O servidor precisa estar no ar** na hora que o cliente for usar. Se o daemon estiver parado, o cliente avisa que não conseguiu conectar (o uso local do servidor continua normal). Para deixá-lo sempre ativo, rode como serviço do systemd, igual ao bot: `systemd-run --user --unit=prisma-servidor ~/.local/bin/prisma servidor --token UMSEGREDO`.
- **Uso simultâneo é seguro.** O banco usa WAL com espera por bloqueio, então o servidor e o cliente podem mexer ao mesmo tempo sem corromper nada.
- O arquivo de configuração do cliente fica em `~/.config/prisma/config` (Linux) ou `~/Library/Application Support/prisma/config` (macOS). O certificado do servidor fica ao lado do banco (`servidor-cert.pem` / `servidor-key.pem`).

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
- O Prisma funciona offline por padrão; nada sai da sua máquina — a menos que você ative o [compartilhamento](#compartilhamento-entre-dispositivos-clienteservidor), e mesmo aí os dados só trafegam na sua rede local, criptografados.
