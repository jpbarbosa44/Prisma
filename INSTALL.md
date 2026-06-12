# Instalando o Prisma

O Prisma é um único arquivo executável — não tem instalador, dependências nem conexão com a internet. Baixe (ou receba) o arquivo da sua plataforma, coloque em uma pasta do sistema e pronto.

## Qual arquivo baixar?

| Seu computador | Arquivo |
|---|---|
| Linux (Intel/AMD) | `prisma-linux-amd64.tar.gz` |
| Mac com Apple Silicon (M1, M2, M3...) | `prisma-mac-arm64.tar.gz` |
| Mac com Intel (até ~2020) | `prisma-mac-intel.tar.gz` |
| Windows (Intel/AMD) | `prisma-windows-amd64.zip` |
| Windows ARM (Surface Pro X etc.) | `prisma-windows-arm64.zip` |

> Não sabe qual Mac você tem? Menu  › Sobre este Mac: se aparecer "Chip Apple M...", use o `arm64`; se aparecer "Intel", use o `intel`.

## Linux

```sh
tar xzf prisma-linux-amd64.tar.gz
mkdir -p ~/.local/bin
mv prisma-linux-amd64 ~/.local/bin/prisma
chmod +x ~/.local/bin/prisma
```

Se o comando `prisma` não for encontrado, `~/.local/bin` não está no PATH. Adicione ao seu `~/.bashrc` ou `~/.zshrc`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

(Abra um terminal novo depois.) Alternativa para todos os usuários da máquina: `sudo mv prisma-linux-amd64 /usr/local/bin/prisma`.

## macOS

```sh
tar xzf prisma-mac-arm64.tar.gz          # ou prisma-mac-intel.tar.gz
chmod +x prisma-mac-arm64
sudo mv prisma-mac-arm64 /usr/local/bin/prisma
```

Na primeira execução, o macOS pode bloquear com *"não pode ser aberto porque é de um desenvolvedor não identificado"* — o binário não é assinado pela Apple. Para liberar:

```sh
xattr -d com.apple.quarantine /usr/local/bin/prisma
```

ou vá em **Ajustes do Sistema › Privacidade e Segurança** e clique em **Abrir Mesmo Assim**.

## Windows

1. Extraia o `prisma-windows-amd64.zip` (clique direito › *Extrair Tudo*).
2. Crie uma pasta para ele, por exemplo `C:\Ferramentas\prisma\`, e mova o `prisma-windows-amd64.exe` para lá, renomeando para `prisma.exe`.
3. Adicione a pasta ao PATH: tecle Win, digite "variáveis de ambiente" › **Editar as variáveis de ambiente do sistema** › **Variáveis de Ambiente** › selecione `Path` › **Editar** › **Novo** › `C:\Ferramentas\prisma`.
4. Abra o **Windows Terminal** ou **PowerShell** (a interface precisa de um terminal moderno — evite o Prompt de Comando antigo) e digite `prisma`.

PowerShell direto, sem cliques:

```powershell
Expand-Archive prisma-windows-amd64.zip -DestinationPath $env:LOCALAPPDATA\prisma
Rename-Item $env:LOCALAPPDATA\prisma\prisma-windows-amd64.exe prisma.exe
[Environment]::SetEnvironmentVariable('Path', $env:Path + ';' + $env:LOCALAPPDATA + '\prisma', 'User')
```

## Conferindo a integridade (opcional, recomendado)

Junto dos pacotes vai um arquivo `SHA256SUMS`. Confira que o seu download não foi corrompido ou alterado:

```sh
sha256sum -c SHA256SUMS --ignore-missing     # Linux
shasum -a 256 -c SHA256SUMS --ignore-missing # macOS
```

```powershell
Get-FileHash prisma-windows-amd64.zip        # Windows: compare com a linha do SHA256SUMS
```

## Primeiro uso

Digite `prisma` no terminal: a interface abre em tela cheia, com o menu de funcionalidades (Saldo, Contas, Carteiras, Pagar/Receber, Emergência, Planejamento, Previsão). Navegue com `↑/↓` + `enter`, volte com `esc`, saia com `q`.

Todos os recursos também existem como comandos diretos — `prisma ajuda` mostra a lista com exemplos.

## Onde ficam meus dados?

Em um único arquivo SQLite, criado no primeiro uso:

| Sistema | Caminho |
|---|---|
| Linux | `~/.local/share/prisma/prisma.db` |
| macOS | `~/Library/Application Support/prisma/prisma.db` |
| Windows | `%AppData%\prisma\prisma.db` |

**Backup** = copiar esse arquivo. Para usar outro local, defina a variável de ambiente `PRISMA_DB` com o caminho desejado.

## Atualizando e desinstalando

- **Atualizar:** substitua o executável pelo novo. Os dados não são tocados.
- **Desinstalar:** apague o executável; se quiser remover também os dados, apague a pasta `prisma` indicada na tabela acima.

## Compilando do código-fonte

Requer apenas Go 1.22+ ([go.dev/dl](https://go.dev/dl)):

```sh
go build -o prisma ./cmd/prisma
```

Para gerar os pacotes de todas as plataformas a partir de qualquer sistema: `make release` (ou veja os comandos `go build` equivalentes no Makefile — não há CGO, então a compilação cruzada funciona sem toolchain extra).
