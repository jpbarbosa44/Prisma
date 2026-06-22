# Plano para a versão 1.0 do Prisma

> Análise de ponta a ponta feita em 2026-06-22, partindo da `v0.10.3`.
> Objetivo: definir o que falta para um lançamento estável e confiável do 1.0.

## Sumário do estado atual

Prisma é uma CLI de finanças pessoais em Go + SQLite (modernc, sem CGO), com:

- **CLI** completa (`cmd/prisma`) e **TUI** em tela cheia (`internal/tui`, bubbletea).
- **Web** local (`prisma --web`, servida em `127.0.0.1`, HTML único embutido).
- **Bot de Telegram** com serviço systemd (`internal/bot`).
- **Modo cliente/servidor** para compartilhar o banco na rede (`internal/remote`, TLS + token).
- **Analytics** (somente leitura), **empresa** (banco separado), **backup/restauração/verificação**, **auto-update** com conferência de SHA256.

Cobertura de testes por pacote hoje:

| Pacote | Cobertura | Observação |
|---|---|---|
| `money` | 98% | sólido — é o núcleo de valores |
| `remote` | 71% | bom, já endurecido |
| `app` | 49% | núcleo de negócio; lacunas em caminhos de dinheiro |
| `update` | 45% | |
| `db` | 44% | migrações pouco testadas |
| `bot` | 20% | baixo |
| `tui` | 17% | baixo (difícil de testar) |
| `cmd/prisma` | 0% | despacho de comandos sem teste |

CI roda `gofmt`, `go vet` e `go test` (só Linux). Release publica 5 binários com SHA256SUMS.

## Critérios de "pronto para 1.0"

1. Nenhuma operação que mexe em dinheiro pode deixar o banco em estado parcial.
2. Os caminhos críticos (lançamentos, parcelas, grupos, recorrências, cartão, transferência, empresa) têm teste automatizado.
3. CI cobre corrida de dados (`-race`), vulnerabilidades e build nas 3 plataformas.
4. Documentação reflete exatamente os comandos e telas atuais.
5. Repositório limpo e com os arquivos de projeto de praxe (CONTRIBUTING, SECURITY).

---

## P0 — Bloqueadores (integridade de dados e correção)

### 1. `CriarLancamentos` não é transacional
- **Onde:** `internal/app/lancamento.go:62` (loop de inserts em `:205`, reembolso de grupo em `:219`, update `parcela_grupo` em `:235`).
- **Problema:** criar um parcelado/recorrência com reembolso faz vários `conn.Exec` soltos. Uma falha no meio (disco, constraint, crash) deixa parcelas órfãs ou um reembolso sem a despesa correspondente.
- **Ação:** abrir uma transação no início da função e fazer todos os inserts/updates dentro dela; `Commit` só no fim. Adaptar as funções auxiliares para aceitar `*sql.Tx` (ou um executor comum).

### 2. `GerarRecorrencias` pode duplicar lançamentos após falha
- **Onde:** `internal/app/recorrencia.go:599` (inserts no loop) e `:611` (`UPDATE ... ultima_ref`).
- **Problema:** os lançamentos do mês são inseridos um a um e o `ultima_ref` só é gravado ao final do loop. Se houver crash entre os inserts e a atualização do `ultima_ref`, a próxima execução recomeça do mesmo mês e **duplica** os lançamentos (não há constraint impedindo).
- **Ação:** envolver cada regra (inserts + update do `ultima_ref`) numa transação única; **e/ou** criar um índice único defensivo, ex.: `UNIQUE(recorrencia_id, vencimento)` para tornar a materialização idempotente por construção.

### 3. Auditar todas as operações multi-passo de dinheiro
- **Já transacionais (ok):** `cartao.go:271` (pagar fatura), `empresa.go:242`/`:562` (capital, distribuir lucro), `categoria.go:101`, `grupo.go:61`/`:112`, `resetar.go:90`.
- **Verificar caso a caso:** edição/remoção de parcelados (`lancamento.go:656`/`:681`), quitação em lote, `emergencia` (hoje writes isolados — provavelmente ok, confirmar).
- **Ação:** revisão dirigida garantindo que toda sequência "debita aqui, credita ali / cria N linhas relacionadas" esteja numa transação.

---

## P1 — Fortemente recomendado antes do 1.0

### Testes (rede de segurança dos caminhos de dinheiro)
- Elevar a cobertura de `app` mirando os módulos sem teste dedicado e de alto risco: `lancamento` (parcelas, reembolso, edição/remoção em grupo), `recorrencia` (idempotência, anual, vigência, cartão), `cartao` (fatura, fechamento, pagar), `transferir`, `previsao`/`simular` (já corrigidos — travar com teste de regressão), `emergencia` (simulação de juros).
- `bot` (20%) e `cmd/prisma` (0%): ao menos testar o **despacho de comandos** e o parser do bot nos fluxos principais.
- Meta pragmática: `app` ≥ 70%, e nenhum caminho de dinheiro sem teste.

### CI/CD
- Adicionar `go test -race ./...` (detecta corrida no servidor remoto, web e bot).
- Adicionar **`govulncheck`** (vulnerabilidades nas dependências).
- Adicionar **`staticcheck`** ou `golangci-lint` (qualidade além do `vet`).
- **Matriz de build** (linux/mac/windows) compilando — hoje mac/windows só são compilados no release, sem garantia em PR.
- Opcional: gate de cobertura mínima.

### Documentação alinhada ao código
- Revisar `README.md` e `MANUAL.md` para refletir 100% dos comandos/telas atuais: Analytics, `--web`, recorrência `--intervalo anual`, o marcador `≈` na previsão, cliente/servidor.
- Adicionar `CONTRIBUTING.md` (como buildar sem `make`, rodar testes, padrão de commit) e `SECURITY.md` (como reportar falhas; relevante por causa do modo servidor e do auto-update).

### Robustez do servidor web local
- **Onde:** `internal/tui/web.go:82` (usa `net.Listen` + serve padrão).
- **Ação:** definir `http.Server` com `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout` e `MaxHeaderBytes`, e limite de corpo nos handlers `POST` (espelhando o que já existe no `remote`). Risco baixo (é localhost), mas é boa prática para o 1.0.

---

## P2 — Desejável (pode entrar no 1.x)

### Banco de dados / performance
- **Onde:** `internal/db/db.go:335-336` (só há índice em `vencimento` e `status`).
- **Ação:** avaliar índices em `recorrencia_id`, `cartao_id`, `parcela_grupo`, `conta_id`/`carteira_id` e `grupo_id` (consultas filtram por esses campos com frequência). Em escala pessoal o ganho é pequeno, mas baratíssimo de adicionar.
- Testar explicitamente o caminho de **migração** de bancos antigos (`db.go:377+`): abrir um banco "v0.x" e confirmar que todas as colunas adicionadas chegam corretas.

### UX e interface
- **Web — camada 2 (opcional):** transformar as telas de tabela larga (Pagar/Receber, Lançamentos) em `<table>`/cards de verdade, para as colunas se distribuírem; hoje é texto monoespaçado e sobra área à direita em telas largas.
- **Responsividade mobile:** adiada por decisão (foco desktop). Reavaliar para 1.x se houver uso em celular.
- Consistência de mensagens de erro (prefixo, capitalização) entre CLI/TUI/web/bot.

### Internacionalização
- Tudo está em PT-BR hardcoded. Decisão de escopo: **declarar o 1.0 como PT-BR** (mais simples e honesto) e tratar i18n como meta pós-1.0, ou começar a extrair strings agora. Recomendo declarar PT-BR no 1.0.

---

## Higiene do repositório (rápido, antes de taguear)

- Remover arquivos soltos da raiz: `imagem1.png` (não está no `.gitignore`), `Imagem colada.png`, `prisma.txt`. Os dois últimos já são ignorados, mas continuam no diretório.
- Adicionar `imagem1.png` (ou um `*.png` de rascunho) ao `.gitignore`.
- Conferir que `dist/` local não foi versionado (já está no `.gitignore`).
- Adicionar templates de issue/PR em `.github/` (opcional, mas ajuda num 1.0 público).

---

## Sequência sugerida até o 1.0

1. **0.11.0 — Integridade:** P0 #1, #2, #3 + testes de regressão dos caminhos corrigidos.
2. **0.12.0 — Qualidade:** P1 testes + CI (`-race`, `govulncheck`, lint, matriz) + robustez do web.
3. **0.13.0 — Acabamento:** docs alinhadas, CONTRIBUTING/SECURITY, higiene do repo, índices.
4. **1.0.0:** congelar escopo (PT-BR, conjunto de features atual), revisar CHANGELOG, taguear.

## Explicitamente fora do escopo do 1.0

- Internacionalização (i18n).
- Responsividade mobile da web e a "camada 2" de tabelas HTML.
- Sincronização em nuvem / multiusuário além do modo cliente/servidor em rede local já existente.
- App empacotado (desktop nativo além do `.desktop` do Linux já presente).
