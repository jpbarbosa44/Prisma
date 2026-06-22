# Changelog

Todas as mudanças relevantes do Prisma são registradas aqui.

O formato segue o [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/)
e o projeto adota o [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Não lançado]

## [1.0.2] - 2026-06-22

### Corrigido
- **Cor dos gráficos do Analytics no navegador.** O 1.0.1 consertou os gráficos
  quebrados removendo os códigos ANSI, mas isso deixava as barras e linhas sem
  cor. Agora a página **converte** as sequências ANSI nas mesmas classes de cor
  do tema (verde/vermelho/amarelo/ciano/magenta/cinza), preservando a cor dos
  gráficos. As mensagens curtas de resultado continuam sem ANSI (são texto puro).

## [1.0.1] - 2026-06-22

### Corrigido
- **Gráficos do Analytics no navegador (`prisma --analytics --web`)** apareciam
  quebrados: a saída capturada da CLI trazia códigos de cor ANSI (do asciigraph e
  dos gráficos de `viz.go`), que o navegador exibia como lixo dentro do `<pre>`.
  O conteúdo servido à página agora tem os códigos ANSI removidos (a página já
  recolore por conta própria); os caracteres de bloco/desenho são preservados.

## [1.0.0] - 2026-06-22

Primeira versão estável. Consolida a base de integridade de dados, qualidade
(CI, testes) e acabamento (documentação, índices) sobre o conjunto de funções já
em uso desde a série 0.10.

### Adicionado
- **CI mais rígido:** além de gofmt/vet/testes, agora roda `staticcheck`,
  `govulncheck` (vulnerabilidades), os testes com `-race` (detector de corrida) e
  valida o build nas cinco combinações de plataforma/arquitetura.
- **Testes de regressão** das projeções cientes de recorrências (previsão além do
  horizonte de materialização) e do plano de quitação de emergências com juros.
- **Testes do despacho de comandos** da CLI (`prisma <cmd>`) e do bot (`/saldo`,
  `/previsao`, `/fatura`, comando desconhecido…), com um servidor de Telegram
  falso — pegam regressões de roteamento sem tocar na rede.

- **Índices de banco** nas colunas mais filtradas dos lançamentos
  (`recorrencia_id`, `cartao_id`, `parcela_grupo`, `grupo_id`, `conta_id`,
  `carteira_id`, `reembolso_de`), com teste de migração de bancos antigos.
- **Documentação de projeto:** `CONTRIBUTING.md` (build/testes/padrões) e
  `SECURITY.md` (como reportar falhas), referenciados no README.

### Alterado
- **Servidor web local endurecido:** `http.Server` com timeouts de leitura/escrita
  e teto de cabeçalho, mais limite de 1 MiB no corpo das requisições POST. Continua
  escutando só em `127.0.0.1`.

### Corrigido
- **Integridade transacional nos caminhos de dinheiro.** Operações que gravam
  mais de uma linha passam a ser atômicas (tudo ou nada), eliminando estados
  parciais em caso de falha no meio:
  - criação de lançamentos parcelados/com reembolso de grupo (`CriarLancamentos`)
    — antes uma falha podia deixar parcelas órfãs ou um reembolso sem a despesa;
  - materialização de recorrências (`GerarRecorrencias`) — os inserts do mês e a
    marca `ultima_ref` agora são gravados juntos, fechando uma janela em que um
    crash entre os dois duplicaria lançamentos na execução seguinte;
  - importação de extrato OFX/CSV (`Importar`) — o extrato inteiro entra de uma
    vez ou nenhum movimento entra.
- Testes de regressão cobrindo a atomicidade do parcelado e a idempotência da
  materialização de recorrências.

## [0.10.3] - 2026-06-22

### Alterado
- **Visual da interface web (`prisma --web`) refinado**, mantendo a identidade da
  TUI (monoespaçado e a mesma paleta). O layout passa a ocupar a largura toda da
  tela (antes ficava num bloco central estreito), com margens laterais fluidas.
  Painéis com profundidade sutil (borda em degradê, sombra), título de tela com
  divisória, menu lateral fixo (sticky) com barra de acento no item ativo,
  teclas dos atalhos como keycaps, abas em pílula, diálogos com animação suave e
  campos com anel de foco. Gráficos SVG aproveitam melhor o painel largo. Tudo em
  CSS puro no arquivo único embutido, sem dependências novas.

## [0.10.2] - 2026-06-22

### Corrigido
- **Projeções passam a considerar as recorrências cadastradas.** As recorrências
  só são materializadas em lançamentos até 3 meses à frente; além desse horizonte,
  os relatórios prospectivos caíam para a média dos últimos 3 meses quitados e
  ignoravam o salário e demais regras, projetando saldo negativo ou runway curto
  sem motivo quando faltavam lançamentos (histórico curto, mês atípico). Agora
  cada mês futuro é estimado nesta ordem: lançamentos agendados → recorrências
  vigentes no mês (mensal/anual, respeitando início/fim) → média histórica como
  último recurso. Afeta `prisma previsao` e `prisma simular`, e no Analytics o
  Runway, as Metas e o Simulador de cenários, além da Saúde Financeira em
  `prisma estatisticas`. Na tabela da previsão, `≈` marca o valor vindo da
  recorrência ainda não lançada.

## [0.10.1] - 2026-06-21

### Adicionado
- **Prisma Analytics no navegador**: `prisma --analytics --web` abre o módulo de
  análise (somente leitura) na interface web, com o selo ANALYTICS, espelhando a
  TUI exclusiva do Analytics.
- **Paridade de atalhos na interface web**: as telas com abas (Estatísticas,
  Gráficos) ganham a régua de visões alternáveis por ←/→ e as listagens mensais
  (Pagar/Receber) passam a responder a ←/→ (mês) e `t` (pagar/receber/todos),
  como na TUI de terminal.

### Corrigido
- **Saldo**: as linhas "Pendente a pagar/receber" passam a considerar só o mês
  atual, não todos os pendentes futuros — as recorrências são materializadas com
  meses de antecedência e inflavam o total (parecia a soma de vários meses). O
  recorte casa com a tela Pagar/Receber, que também abre no mês corrente.

## [0.10.0] - 2026-06-21

### Adicionado
- Recuperação de dados: comandos `prisma restaurar` e `prisma verificar` para
  recuperar o banco a partir dos backups diários e checar sua integridade.
- **Visualização em texto**: novo toolkit de gráficos ASCII (`internal/app/viz.go`)
  com gráficos de linha (asciigraph), barras com resolução de 1/8 de caractere,
  sparklines e mapas de calor, reaproveitado por `prisma graficos` e pelo
  módulo Analytics. As telas da TUI capturam a saída e colorem por regex.
- Oferta de atualização na abertura: quando a checagem diária encontra uma
  versão nova, o Prisma pergunta antes de baixar e instalar (estilo oh-my-zsh).

### Segurança
- Modo cliente/servidor: limite de tamanho do corpo das requisições (8 MiB) e
  timeouts de leitura de cabeçalho/ociosidade no servidor HTTP, contra slowloris
  e exaustão de memória.
- TLS mínimo fixado em 1.2 no cliente e no servidor.
- Pinning de certificado coberto por teste de handshake ponta a ponta; chave
  privada do servidor gravada com permissão `0600`.

### Corrigido
- Corridas de dados no servidor remoto (ping/reaper/sessões) corrigidas com
  travamento por sessão; validadas com `go test -race`.

## [0.9.1] - 2026-06-20

### Adicionado
- **Prisma Analytics** (`prisma --analytics`): módulo de análise financeira
  somente leitura.
- Empacotamento para desktop: atalho `.desktop` e ícone no menu de aplicativos.

### Adicionado / Corrigido
- Melhorias em cartões, recorrências, filtros e lembretes.

## [0.9.0] - 2026-06-17

### Adicionado
- **Módulo empresa** (`prisma --empresa`): sócios, capital, imposto,
  investimento e lucro.

## [0.8.1] - 2026-06-17

### Adicionado
- Grupos: opção de reembolso quando os outros te pagam de volta.

## [0.8.0] - 2026-06-17

### Adicionado
- Categorias, campo de observação, auto-quitar, estatísticas e outras melhorias.

## [0.7.0] - 2026-06-15

### Adicionado
- **Modo cliente/servidor**: compartilhamento do banco na rede local.
- README documentando grupos, cartões, assinaturas, gráficos e simulação.

## [0.6.0] - 2026-06-15

### Adicionado
- Cartões de crédito e assinaturas, com ajustes nas tabelas.

## [0.4.1] - 2026-06-14

### Adicionado
- Bot: marcador `grupo:N` para dividir despesa e comando `/grupos`.

## [0.4.0] - 2026-06-14

### Adicionado
- Grupos, gráficos e bot rodando como serviço.
- Simulação de compra, tela "Como usar" e auto-atualização.
- Interface web (`prisma --web`): abre as telas da TUI no navegador.

### Outros
- Licença GPL-3.0.

## [0.2.1] - 2026-06-12

### Adicionado
- Backup diário automático do banco.
- TUI: esquema de cores semântico na saída das telas.

## [0.2.0] - 2026-06-12

### Adicionado
- TUI: cabeçalho com o prisma em 3D e espectro saindo da face frontal.
- Bot: lembretes, resumo do dia, quitar, corrigir, transferir e comprovantes.

## [0.1.0] - 2026-06-12

### Adicionado
- Bot de Telegram: registra e consulta lançamentos por mensagem.
- Primeira versão do projeto (CLI de finanças em Go + SQLite).

[Não lançado]: https://github.com/jpbarbosa44/Prisma/compare/v0.10.1...HEAD
[0.10.1]: https://github.com/jpbarbosa44/Prisma/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/jpbarbosa44/Prisma/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.8.1...v0.9.0
[0.8.1]: https://github.com/jpbarbosa44/Prisma/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.4.1...v0.6.0
[0.4.1]: https://github.com/jpbarbosa44/Prisma/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.2.1...v0.4.0
[0.2.1]: https://github.com/jpbarbosa44/Prisma/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/jpbarbosa44/Prisma/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/jpbarbosa44/Prisma/releases/tag/v0.1.0
