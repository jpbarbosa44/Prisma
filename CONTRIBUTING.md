# Contribuindo com o Prisma

Obrigado pelo interesse! Este guia cobre o essencial para desenvolver e enviar
mudanças.

## Pré-requisitos

- **Go** na versão declarada em [`go.mod`](go.mod) (ou mais recente).
- Nada de CGO: o projeto usa `modernc.org/sqlite` (SQLite em Go puro), então o
  build é estático e não precisa de toolchain de C.

## Build

Com `make` (se tiver):

```sh
make build      # gera bin/prisma com a versão da tag git embutida
make install    # instala em ~/.local/bin e cria o atalho .desktop (Linux)
```

Sem `make`, direto pelo Go:

```sh
go build -o bin/prisma ./cmd/prisma
# para embutir a versão (igual ao Makefile/release):
go build -ldflags "-X prisma/internal/update.Versao=$(git describe --tags --always --dirty)" -o bin/prisma ./cmd/prisma
```

## Antes de abrir um PR

Rode localmente o que o CI também roda (veja [`.github/workflows/ci.yml`](.github/workflows/ci.yml)):

```sh
gofmt -l .                 # não pode listar nenhum arquivo
go vet ./...
staticcheck ./...          # go install honnef.co/go/tools/cmd/staticcheck@latest
govulncheck ./...          # go install golang.org/x/vuln/cmd/govulncheck@latest
go test -race ./...
```

- **Formatação:** sempre `gofmt -w .` antes de commitar — o CI falha se houver
  arquivo fora do padrão.
- **Testes:** todo caminho que mexe em dinheiro deve ter teste. Os testes abrem um
  banco temporário via a variável de ambiente `PRISMA_DB` (veja
  `internal/app/app_test.go`); não tocam no banco real nem na rede.
- **CHANGELOG:** registre mudanças relevantes em `CHANGELOG.md`, na seção
  "Não lançado", seguindo o [Keep a Changelog](https://keepachangelog.com/pt-BR/).

## Estrutura do projeto

```
cmd/prisma        ponto de entrada e despacho de comandos da CLI
internal/app      regras de negócio (contas, lançamentos, recorrências, cartão…)
internal/tui      interface de terminal (bubbletea) e a interface web (web.go/web.html)
internal/bot      bot de Telegram
internal/remote   modo cliente/servidor (compartilhar o banco na rede, TLS + token)
internal/db       schema, migrações e backup do SQLite
internal/money    parsing e formatação de valores (centavos)
internal/update   verificação e auto-update com conferência de SHA256
```

## Estilo de commit

- Assunto curto e em português, no imperativo (ex.: "Corrige duplicação de
  recorrência após falha").
- No corpo, explique **o porquê** da mudança, não só o quê.
- Commits focados: uma mudança lógica por commit.

## Versionamento e releases

O projeto segue [SemVer](https://semver.org/lang/pt-BR/). A versão sai da tag git
(`git describe`), e o push de uma tag `vX.Y.Z` dispara o workflow de release, que
publica os binários das três plataformas com `SHA256SUMS`.
