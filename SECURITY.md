# Política de Segurança

## Versões com suporte

O Prisma é desenvolvido continuamente; correções de segurança vão para a versão
mais recente. Use sempre a última release (`prisma versao` mostra a instalada;
`prisma atualizar` baixa a nova, conferindo o SHA256).

## Como reportar uma vulnerabilidade

**Não abra uma issue pública** para falhas de segurança.

Use o canal privado de **Security Advisories** do GitHub neste repositório
("Security" → "Report a vulnerability"). Descreva:

- o que é afetado e qual o impacto;
- passos para reproduzir;
- versão do Prisma e sistema operacional.

A resposta costuma vir em poucos dias. Pedimos um prazo razoável para a correção
antes de qualquer divulgação pública.

## Superfície de ataque a considerar

O Prisma é, por padrão, **offline e local** (um arquivo SQLite na máquina). Os
pontos que envolvem rede ou execução externa, e que merecem atenção num report:

- **Modo cliente/servidor** (`prisma servidor` / `prisma config cliente`):
  compartilha o banco na rede local com TLS e um token compartilhado. Há limite
  de tamanho de corpo e verificação de fingerprint do certificado.
- **Interface web** (`prisma --web`): escuta apenas em `127.0.0.1`, com timeouts
  e limites de corpo/cabeçalho. Não é feita para exposição em rede.
- **Bot de Telegram** (`prisma bot`): autentica o chat autorizado pelo `chat_id`
  configurado.
- **Auto-update** (`prisma atualizar`): baixa binários do GitHub Releases e
  confere o `SHA256SUMS` antes de instalar (troca atômica do arquivo).

Relatos sobre esses vetores — ou sobre integridade dos dados financeiros — são
especialmente bem-vindos.
