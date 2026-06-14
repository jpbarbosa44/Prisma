package tui

// Textos das telas informativas (Simulação vazia e Como usar). Ficam aqui,
// fora de telas.go, por serem só conteúdo — sem lógica nem comandos.

// textoSimulacaoVazio é o que a tela Simulação mostra antes de informar uma
// compra: explica o que a ferramenta faz e como acioná-la.
const textoSimulacaoVazio = `SIMULAÇÃO DE COMPRA

Antes de comprar parcelado, veja como a compra afetaria seu saldo.

Tecle "s" (ou clique em simular) e informe:
  - valor    o preço total da compra            (obrigatório)
  - parcelas em quantas vezes (vazio = à vista)
  - juros    % ao mês do parcelamento, se houver
  - entrada  valor que você daria à vista, se houver

O Prisma projeta seu saldo mês a mês, COM e SEM a compra, usando suas
recorrências, contas a pagar/receber e a média dos últimos meses. No fim,
diz se você:
  - pode comprar tranquilo,
  - ficaria no aperto (sem reserva para imprevistos), ou
  - ficaria negativado.

Nada é gravado: é só uma projeção.`

// textoComoUsar é a documentação embutida, exibida na tela "Como usar".
const textoComoUsar = `COMO USAR O PRISMA

O Prisma é um gerenciador de finanças pessoais que roda inteiro na sua
máquina. Ele responde a três perguntas: quanto eu tenho hoje, para onde
meu dinheiro vai, e como eu fico daqui pra frente.

Esta é a mesma interface da TUI (terminal) e da web (--web). O menu à
esquerda lista as telas: tecle o número (1-9) ou clique. Dentro de cada
tela, as teclas mostradas no rodapé disparam ações (a adicionar, e editar,
x remover, etc.). Nas tabelas, ↑/↓ ou um clique selecionam uma linha — o
id dela já entra preenchido nos formulários de quitar, editar e remover.


COMO O PRISMA PENSA (modelo mental)

  Onde o dinheiro está   → CONTAS e CARTEIRAS, cada uma com um saldo.
  O que move o dinheiro  → LANÇAMENTOS: contas a pagar (gastos) e a
                           receber (receitas), com vencimento e categoria.
  O que você divide      → GRUPOS: uma despesa vinculada a um grupo conta só
                           pela sua parte (o valor dividido entre as pessoas).
  O que se repete        → RECORRÊNCIAS viram lançamentos sozinhas todo
                           mês, então você não relança salário nem aluguel.
  Onde isso aparece      → SALDO soma tudo o que já aconteceu; RELATÓRIO
                           explica o passado; PREVISÃO e SIMULAÇÃO projetam
                           o futuro a partir dos mesmos dados.

  Um lançamento nasce "pendente" e vira "quitado" quando você o paga ou
  recebe. O saldo realizado conta só os quitados; o que está pendente
  aparece à parte (a pagar / a receber) e alimenta as projeções.


PRIMEIROS PASSOS
  1. Em Contas, cadastre suas contas bancárias com o saldo atual de cada.
  2. Em Carteiras, cadastre o dinheiro fora do banco (espécie, vale etc.).
  3. Em Recorrências, cadastre o que entra e sai todo mês (salário, aluguel,
     assinaturas) — isso é o que dá inteligência à previsão.
  4. No dia a dia, use Pagar/Receber para registrar gastos e receitas, e
     quite-os quando acontecerem.
  5. Acompanhe em Saldo, Relatório, Previsão e Simulação.


AS TELAS

Saldo
  A posição geral consolidada: o saldo de cada conta e carteira, o total
  pendente a pagar e a receber, dívidas em emergência e a posição líquida
  (o que sobraria se tudo que está pendente fosse liquidado). Daqui você
  também transfere dinheiro entre conta e carteira (tecla t) — uma
  transferência não é gasto nem receita, só muda o dinheiro de lugar.

Contas
  Suas contas bancárias (corrente, poupança, investimento). Adicione, edite,
  remova e veja o extrato de cada uma: a movimentação em ordem, com o saldo
  recalculado após cada lançamento. O saldo da conta = saldo inicial + tudo
  que foi quitado nela.

Carteiras
  Dinheiro fora do banco: espécie, vale-refeição, a poupança da gaveta.
  Funciona igual às contas, com extrato próprio. Útil para não esquecer do
  dinheiro vivo na hora de fechar as contas.

Grupos
  Para gastos divididos com outras pessoas (ex.: "Eu e a Maria"). Cadastre um
  grupo com as pessoas que o compõem e, ao lançar uma despesa, vincule-a ao
  grupo: o Prisma passa a contar só a SUA parte — o valor cheio dividido pelo
  número de pessoas. Essa parte é o que aparece em todo o resto (saldo,
  extrato, relatório, planejamento e previsão); uma compra de R$ 300 num grupo
  de 2 pesa R$ 150 no seu bolso. A lista de Grupos mostra o total cheio das
  despesas vinculadas e quanto disso é seu. Remover o grupo faz as despesas
  voltarem a contar pelo valor cheio (os lançamentos não são apagados).

Pagar/Receber
  O coração do dia a dia. Cada lançamento é uma conta a pagar (gasto) ou a
  receber (receita), com descrição, valor, vencimento, categoria e, se
  quiser, vínculo a uma conta ou carteira.
    - a pagar / a receber: cria o lançamento. Dois jeitos de repetir:
        parcelas N — divide o VALOR TOTAL em N parcelas mensais (ex.: uma
                     compra de 1.200 em 3x vira 3 lançamentos de 400);
        repetir  N — repete o MESMO valor por N meses (ex.: 12 mensalidades
                     de 180 cada). Use um ou outro, não os dois.
    - quitar: marca como pago/recebido na data informada (ou hoje). Só o
      quitado entra no saldo realizado.
    - editar / remover: ajusta ou apaga um lançamento (campos vazios na
      edição mantêm o valor atual).
    - filtrar: limita a lista por tipo, mês, intervalo de datas ou categoria,
      para achar o que procura.

Recorrências
  Regras para o que se repete sem você relançar: salário todo dia 1, aluguel
  dia 5, assinatura dia 20. Você define tipo, valor, dia do mês, início e
  (opcional) fim. Quando a data chega, o Prisma cria o lançamento pendente
  automaticamente — inclusive os meses que ficaram para trás desde a última
  vez que você abriu o programa. Editar uma recorrência ajusta também os
  pendentes futuros que ela já tinha gerado.

Emergência
  Plano de ação para sair de uma dívida (cartão, cheque especial). Informe o
  valor devido, os juros ao mês e o aporte (quanto consegue pagar por mês).
  O Prisma simula juros compostos mês a mês e monta a tabela de quitação: em
  quantos meses acaba, quanto vai pagar de juros, e quanto você economiza se
  acelerar o aporte em 20%. Se o aporte não cobre nem os juros do primeiro
  mês, ele recusa e diz o mínimo necessário (senão a dívida só cresce). Os
  aportes das emergências ativas entram como saída na Previsão e na Simulação.

Planejamento
  Orçamento: limites de gasto por categoria, por semana ou por mês (ex.:
  R$ 800 de mercado no mês). O status mostra quanto de cada limite já foi
  usado, com barra de progresso, para você frear antes de estourar. No bot
  do Telegram, um gasto que encosta em 80% ou passa do limite já dispara
  um aviso na hora.

Relatório
  A análise do passado. Para os últimos N meses: gastos e receitas somados
  por categoria (onde seu dinheiro realmente vai) e a evolução mês a mês,
  para enxergar tendências.

Gráficos
  A mesma leitura do Relatório, em forma visual: gastos por categoria,
  receitas × despesas por mês, evolução do saldo e despesa por grupo (sua
  parte sobre o total cheio). No terminal saem em ASCII; na web (prisma --web)
  viram gráficos coloridos em SVG.

Previsão
  A projeção do futuro. Para cada um dos próximos meses, soma as receitas e
  despesas já agendadas (recorrências e pendentes); quando um mês não tem
  nada agendado de um tipo, usa a média dos últimos 3 meses (marcada com ~).
  Desconta os aportes das emergências. Mostra o saldo projetado mês a mês,
  um gráfico de barras e avisa a partir de quando o saldo ficaria negativo.

Simulação  — "e se eu comprar isto?"
  Veja o impacto de uma compra ANTES de fazê-la, sem gravar nada. Detalhada
  na seção abaixo.

Como usar
  Esta tela.


A SIMULAÇÃO EM DETALHE

  A pergunta que ela responde: "quero comprar um videogame de R$ 4.000 em
  12x — isso me deixa negativado, me deixa no aperto, ou posso comprar?".

  O que informar (tecla s, ou o botão simular):
    valor    — o preço total da compra (obrigatório).
    parcelas — em quantas vezes; vazio ou 1 = à vista.
    juros    — % ao mês do parcelamento, se houver. Com juros, a parcela é
               calculada pela Tabela Price (parcela fixa) e a simulação
               mostra o total pago e quanto disso é só juros.
    entrada  — um valor pago à vista agora, que reduz o que é parcelado.

  Como ela calcula: parte do seu saldo atual e projeta os próximos meses
  usando o MESMO motor da Previsão (suas recorrências, contas a pagar/
  receber e a média dos últimos meses). Monta duas trajetórias lado a lado:
  SEM A COMPRA (sua vida como está) e COM A COMPRA (descontando cada parcela
  no mês em que ela cai). Assim dá pra ver exatamente quanto a compra pesa.

  O veredito, no fim, é um de três:
    🟢 Pode comprar    — mesmo com a compra, seu saldo nunca cai abaixo de
                         uma folga saudável.
    ⚠ Arriscado        — dá pra comprar, mas sua folga cai abaixo de um mês
                         de despesas: você ficaria sem reserva para um
                         imprevisto durante o parcelamento.
    🔴 Não recomendado — em algum mês do parcelamento seu saldo fica
                         NEGATIVO. Ele diz em qual mês e quão fundo, e sugere
                         mais parcelas, uma entrada maior, ou adiar a compra.

  Quanto melhores seus dados (recorrências e histórico), mais fiel a
  projeção. Nada é salvo: simule quantas compras quiser, é só um cenário.


VALORES, DATAS E CATEGORIAS
  - Valores aceitam 1500, 1.500,00 ou 1500.00 — com ou sem "R$".
  - Datas aceitam DD/MM/AAAA ou AAAA-MM-DD. Campo de data vazio = hoje.
  - Dia 31 num mês curto vira o último dia do mês.
  - Categorias são livres (mercado, moradia, lazer...). Cuidado com a grafia:
    "mercado" e "mercados" viram categorias diferentes — o Prisma avisa na
    primeira vez que vê uma categoria nova, para pegar erros de digitação.
  - Campos marcados com * são obrigatórios.


OUTRAS FORMAS DE USAR
  - Terminal: "prisma <comando>" para a linha de comando (bom para scripts);
    "prisma ajuda" lista todos os comandos. Esta interface no navegador é
    "prisma --web"; "prisma" sem nada abre a versão de terminal.
  - Telegram: "prisma bot" deixa você registrar gastos por mensagem ("25,50
    #mercado pão"), consultar (/saldo, /previsao, /simular 4000 12x) e ainda
    recebe aviso de vencimentos às 9h e um resumo do dia às 20h. Como ele fala
    de saída com o Telegram, responde de qualquer rede — basta o PC estar
    ligado com o bot rodando. Para mantê-lo sempre no ar (sem terminal aberto),
    use "prisma bot --instalar-servico" (Linux). O manual completo está no
    MANUAL.md, no repositório do projeto.


SEUS DADOS E PRIVACIDADE
  Tudo fica num banco SQLite local, só na sua máquina — nada vai para a
  nuvem, nenhum servidor externo é consultado. A interface web escuta apenas
  em 127.0.0.1, então também não fica exposta na rede. O Prisma faz um backup
  automático por dia. Em Saldo, a ação "zerar banco" apaga tudo (com backup
  antes), caso você queira recomeçar do zero.`
