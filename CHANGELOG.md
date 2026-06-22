# Changelog

Todas as mudanças relevantes do Prisma são registradas aqui.

O formato segue o [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/)
e o projeto adota o [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Não lançado]

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
