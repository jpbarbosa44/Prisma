# Plano para a versão 2.0 do Prisma

> Análise feita em 2026-07-03, partindo da `v1.2.0`.
> Objetivo: definir o que justifica um novo major e o caminho até lá.
> (Para o histórico do 1.0, ver `PLANO-v1.0.md`.)

## Sumário do estado atual

O 1.x entregou a promessa do 1.0 e um pouco mais: CLI/TUI/web/bot estáveis,
cliente/servidor em rede local, Analytics com 16 análises, gráficos SVG na
web, importação OFX/CSV, auto-update com SHA256. Cobertura hoje:

| Pacote | Cobertura | Observação |
|---|---|---|
| `money` | 97% | núcleo de valores, sólido |
| `remote` | 70% | endurecido no 1.0 |
| `app` | 51% | subiu pouco desde o 1.0 (49%) |
| `update` | 45% | |
| `db` | 44% | migrações ad-hoc, pouco testadas |
| `bot` | 29% | |
| `tui` | 22% | interação quase sem teste |
| `cmd/prisma` | 4% | despacho de comandos |

Limites estruturais do 1.x que motivam este plano:

- **Valores são `int64` de centavos implicitamente em BRL** — não há moeda no
  schema; `money.Format`/`Parse` assumem pt-BR.
- **Dívidas não têm amortização real**: `emergencias` guarda o valor original
  e o Analytics *estima* o saldo devedor pelo plano de aportes
  (`saldoDevedorEstimado`, `internal/app/analytics.go`). Pagamentos de verdade
  não são registrados nem vinculados.
- **Investimento pessoal não existe** (só a empresa tem `investimento`);
  o Patrimônio Líquido enxerga apenas contas + carteiras − dívidas.
- **Migração de schema é ad-hoc**: `CREATE TABLE IF NOT EXISTS` + `ALTER`
  best-effort em `internal/db/db.go`, sem `PRAGMA user_version`, sem rollback.
- **O contrato entre core e interfaces é o stdout**: a TUI e a web capturam o
  texto impresso pelas funções da CLI (`captura`, `internal/tui/tui.go`).
  Funciona muito bem, mas o formato de saída é API de fato.
- **Import não concilia**: `Importar` (`internal/app/exportar.go:102`) insere;
  dedupe e categorização são manuais.

## Critérios de "pronto para 2.0"

1. Schema versionado com migração automática, backup prévio e teste que abre
   um banco 1.x real e o migra até o schema atual.
2. Dívida é fato, não estimativa: cada pagamento é um lançamento vinculado e
   o saldo devedor sai do razão.
3. Investimentos pessoais existem e o Patrimônio Líquido é completo
   (líquido + investido − dívidas).
4. Importar um extrato pela segunda vez não duplica nada e sugere categorias.
5. Nenhum dado de usuário 1.x é perdido ou reinterpretado na migração.

---

## P0 — O núcleo do major (mudanças de schema)

### 1. Migrações versionadas (pré-requisito de tudo)
- **Onde:** `internal/db/db.go` (hoje: DDL idempotente + seeds; sem versão).
- **Ação:** `PRAGMA user_version` + lista ordenada de migrações, cada uma numa
  transação; backup automático do arquivo antes de migrar (reusar a rotina de
  `backup.go`); recusa de abrir banco de versão mais nova que o binário.
- **Teste:** fixture de um banco gerado pela `v1.2.0` migrando até o atual.

### 2. Dívidas com amortização real
- **Onde:** `emergencias` (schema em `db.go`), `emergencia.go`, Analytics.
- **Ação:** registrar pagamentos como lançamentos vinculados à dívida
  (`emergencia_id` em `lancamentos` ou tabela de amortizações); `emergencia
  pagar <id>` quita o aporte do mês e recalcula o saldo devedor de fato;
  o plano (`simulaPlano`) vira projeção *a partir do saldo real*.
- **Efeito:** `AnalyticsPatrimonio` deixa de estimar (`saldoDevedorEstimado`
  sai de cena) e a evolução mensal do patrimônio passa a usar a dívida real
  de cada mês.

### 3. Investimentos pessoais
- **Ação:** entidade própria (nome, tipo, aportes/resgates como lançamentos
  vinculados, valor atual atualizável manualmente); entra no `saldo`, no
  Patrimônio Líquido e ganha vista no Analytics (alocação, evolução,
  aporte médio vs meta).
- **Escopo contido:** sem cotação automática no 2.0 — valor atual é informado
  pelo usuário (como o `--fatura-atual` do cartão: pragmático e honesto).

### 4. Moeda explícita no dado
- **Ação mínima que evita retrabalho futuro:** coluna `moeda` (default 'BRL')
  em contas/carteiras/lançamentos e no `money` um formatador por moeda.
  Multi-moeda *de verdade* (câmbio, consolidação) fica para 2.x, mas o dado
  já nasce certo no major — mudar isso depois seria outra migração dolorosa.

---

## P1 — O que faz o 2.0 valer a pena no dia a dia

### Conciliação no import
- **Onde:** `Importar` (`internal/app/exportar.go:102`).
- **Ação:** dedupe por (conta, data, valor, FITID quando houver); casamento
  com lançamentos pendentes existentes (quitar em vez de duplicar); sugestão
  de categoria por histórico (descrição→categoria mais frequente, aprendendo
  das edições). Relatório do que entrou/casou/ficou em dúvida.

### Metas com aporte rastreado
- **Ação:** envelope/conta-meta com aportes reais (transferências marcadas);
  `AnalyticsMetas` passa a mostrar progresso de fato vs plano, não só
  viabilidade projetada.

### Core desacoplado do stdout (gradual)
- **Ação:** os cálculos passam a devolver structs (como `GraficosDados` e
  `DadosGraficos` já fazem) e a impressão vira camada à parte. A captura de
  stdout continua funcionando durante a transição; o ganho imediato é a web
  poder renderizar tabelas/gráficos nativos em vez de texto, e o bot compor
  mensagens sem regex.

---

## P2 — Desejável (pode entrar no 2.x)

- **PWA:** manifest + service worker na web local → "app" instalável no
  celular; o bot do Telegram segue como captura rápida.
- **Sync multi-dispositivo:** log de eventos por cima do SQLite com resolução
  de conflito simples (last-write-wins por campo + aviso), evoluindo o modo
  cliente/servidor atual; alternativa modesta: backup criptografado remoto.
- **Criptografia em repouso** (SQLCipher ou equivalente sem CGO, a avaliar —
  o projeto usa modernc/sqlite justamente para não depender de CGO).
- **Multi-moeda completa** (câmbio manual/automático, consolidação em BRL).
- **i18n** — segue decisão do 1.0: PT-BR primeiro; extrair strings só se
  houver demanda real.

---

## Fundação de engenharia (paralelo, sem gate)

- `app` ≥ 70% de cobertura (mesma meta pragmática do plano 1.0, ainda não
  atingida — está em 51%); prioridade nos caminhos novos de dinheiro
  (amortização, investimento, conciliação).
- Testes de integração da TUI nos fluxos principais (bubbletea permite
  dirigir o programa com mensagens sintéticas) e das rotas web.
- Bump das actions do CI (aviso de deprecação do Node 20 já aparece no
  workflow de release).
- `cmd/prisma` (4%): teste do despacho de comandos.

## Sequência sugerida até o 2.0

1. **1.3.0 — Fundação:** migrações versionadas (#1) + fixture de migração +
   bump do CI. Nada muda para o usuário; tudo muda para o projeto.
2. **1.4.0 — Conciliação:** import com dedupe/casamento/sugestão (P1) — não
   depende de schema novo e destrava o uso diário.
3. **2.0.0-beta — Schema novo:** dívidas reais (#2), investimentos (#3),
   moeda explícita (#4), migração automática do banco 1.x; período de beta
   com o banco de verdade (o backup prévio protege o arrependimento).
4. **2.0.0:** metas com aporte, Patrimônio/Analytics sobre os dados novos,
   docs (MANUAL/README) e CHANGELOG revisados, taguear.

## Explicitamente fora do escopo do 2.0

- Open Finance / conexão bancária automática (Pluggy etc.) — o caminho fica
  aberto pela conciliação, mas integração com terceiros é aposta de 2.x.
- Sync em nuvem e PWA (2.1/2.2, ver P2).
- Multiusuário além do modo cliente/servidor atual.
- Cotação automática de investimentos.
- i18n.
