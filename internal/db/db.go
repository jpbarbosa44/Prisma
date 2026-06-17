// Package db abre o banco SQLite do Prisma e aplica as migrações.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"prisma/internal/remote"

	_ "modernc.org/sqlite"
)

// dataDir devolve o diretório de dados padrão de cada sistema (Linux:
// ~/.local/share/prisma; macOS/Windows: dir de config do usuário + prisma).
func dataDir() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "prisma"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "prisma"), nil
}

// Path retorna o caminho do banco pessoal: $PRISMA_DB ou prisma.db no
// diretório de dados padrão.
func Path() (string, error) {
	if p := os.Getenv("PRISMA_DB"); p != "" {
		return p, nil
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prisma.db"), nil
}

// PathEmpresa retorna o caminho do banco da empresa (modo `prisma --empresa`):
// $PRISMA_EMPRESA_DB ou empresa.db no diretório de dados padrão — um arquivo
// totalmente separado do banco pessoal.
func PathEmpresa() (string, error) {
	if p := os.Getenv("PRISMA_EMPRESA_DB"); p != "" {
		return p, nil
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "empresa.db"), nil
}

// Abrir escolhe o backend conforme a config: nos modos local e servidor abre o
// banco SQLite local (o servidor é o dono do arquivo); no modo cliente devolve
// um *sql.DB que fala com um servidor Prisma pela rede. O resto do programa
// recebe um *sql.DB e não distingue os dois.
func Abrir(cfg remote.Config) (*sql.DB, error) {
	if cfg.Modo == remote.ModoCliente {
		return OpenCliente(cfg)
	}
	return Open()
}

// OpenCliente conecta a um servidor Prisma. Migrações e backup ficam por conta
// do servidor (ele é quem tem o arquivo), então aqui só validamos a conexão
// cedo para falhar com uma mensagem clara se o servidor estiver fora do ar.
func OpenCliente(cfg remote.Config) (*sql.DB, error) {
	conn := sql.OpenDB(remote.NovoConnector(cfg))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("conectando ao servidor Prisma: %w", err)
	}
	return conn, nil
}

// Open abre (criando se necessário) o banco pessoal e aplica o schema.
func Open() (*sql.DB, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return abrirArquivo(p)
}

// OpenEmpresa abre (criando se necessário) o banco da empresa — um arquivo
// totalmente separado do pessoal, usado em `prisma --empresa`.
func OpenEmpresa() (*sql.DB, error) {
	p, err := PathEmpresa()
	if err != nil {
		return nil, err
	}
	return abrirArquivo(p)
}

// abrirArquivo abre (criando se necessário) o banco SQLite em p e aplica o
// schema. Antes de abrir, faz o backup diário — a cópia retrata o banco antes
// da sessão.
func abrirArquivo(p string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("criando diretório do banco: %w", err)
	}
	if err := backupDiario(p); err != nil {
		// backup falho não pode impedir o uso; só avisa
		fmt.Fprintf(os.Stderr, "aviso: backup diário falhou: %v\n", err)
	}
	// WAL: melhor concorrência (bot + web + CLI lendo/escrevendo) e snapshots de
	// backup consistentes sem bloquear quem escreve.
	conn, err := sql.Open("sqlite", p+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("aplicando schema: %w", err)
	}
	return conn, nil
}

// maxBackups é quantas cópias diárias ficam guardadas.
const maxBackups = 7

// Backup força a checagem do backup diário — usado pelo bot, que fica dias
// rodando sem reabrir o banco.
func Backup() error {
	p, err := Path()
	if err != nil {
		return err
	}
	return backupDiario(p)
}

// backupDiario copia o banco para backups/ ao lado dele, no máximo uma vez
// por dia, e apaga as cópias além das maxBackups mais recentes.
func backupDiario(caminho string) error {
	if _, err := os.Stat(caminho); err != nil {
		return nil // banco ainda não existe (primeiro uso)
	}
	dir := filepath.Join(filepath.Dir(caminho), "backups")
	alvo := filepath.Join(dir, "prisma-"+time.Now().Format("2006-01-02")+".db")
	if _, err := os.Stat(alvo); err == nil {
		return nil // o de hoje já existe
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// VACUUM INTO grava um snapshot consistente e já compactado, sob lock de
	// leitura — diferente de copiar o arquivo cru, não corrompe se o banco
	// estiver sendo escrito (ex.: pelo bot rodando como serviço).
	src, err := sql.Open("sqlite", caminho+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := src.Exec("VACUUM INTO ?", alvo); err != nil {
		return err
	}
	nomes, err := filepath.Glob(filepath.Join(dir, "prisma-*.db"))
	if err != nil || len(nomes) <= maxBackups {
		return nil
	}
	sort.Strings(nomes) // nomes datados (AAAA-MM-DD) ordenam por idade
	for _, n := range nomes[:len(nomes)-maxBackups] {
		os.Remove(n)
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS contas (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	nome          TEXT NOT NULL,
	banco         TEXT NOT NULL DEFAULT '',
	tipo          TEXT NOT NULL DEFAULT 'corrente', -- corrente | poupanca | investimento
	saldo_inicial INTEGER NOT NULL DEFAULT 0,       -- centavos
	criada_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS carteiras (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	nome          TEXT NOT NULL,
	descricao     TEXT NOT NULL DEFAULT '',
	saldo_inicial INTEGER NOT NULL DEFAULT 0,
	criada_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS lancamentos (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tipo        TEXT NOT NULL CHECK (tipo IN ('pagar','receber')),
	descricao   TEXT NOT NULL,
	valor       INTEGER NOT NULL,                   -- centavos, sempre positivo
	categoria   TEXT NOT NULL DEFAULT 'geral',
	vencimento  TEXT NOT NULL,                      -- AAAA-MM-DD
	status      TEXT NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','quitado')),
	quitado_em  TEXT,                               -- AAAA-MM-DD
	conta_id    INTEGER REFERENCES contas(id) ON DELETE SET NULL,
	carteira_id INTEGER REFERENCES carteiras(id) ON DELETE SET NULL,
	criado_em   TEXT NOT NULL DEFAULT (date('now','localtime')),
	recebe_pagamento INTEGER NOT NULL DEFAULT 0, -- 1 = valor já é a sua parte; o resto foi lançado como receita de reembolso
	reembolso_de      INTEGER REFERENCES lancamentos(id) ON DELETE CASCADE -- aponta pra despesa que gerou esta receita de reembolso
);

CREATE TABLE IF NOT EXISTS emergencias (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	descricao     TEXT NOT NULL,
	credor        TEXT NOT NULL DEFAULT '',
	valor_total   INTEGER NOT NULL,                 -- centavos
	juros_mes     REAL NOT NULL DEFAULT 0,          -- % ao mês
	aporte_mensal INTEGER NOT NULL,                 -- centavos
	status        TEXT NOT NULL DEFAULT 'ativa' CHECK (status IN ('ativa','quitada')),
	criada_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS planejamentos (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	categoria TEXT NOT NULL,
	limite    INTEGER NOT NULL,                     -- centavos
	periodo   TEXT NOT NULL CHECK (periodo IN ('semana','mes')),
	ref       TEXT NOT NULL,                        -- '2026-06' ou '2026-W24'
	criado_em TEXT NOT NULL DEFAULT (date('now','localtime')),
	UNIQUE (categoria, periodo, ref)
);

CREATE TABLE IF NOT EXISTS transferencias (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	valor        INTEGER NOT NULL,                  -- centavos
	data         TEXT NOT NULL,                     -- AAAA-MM-DD
	descricao    TEXT NOT NULL DEFAULT '',
	origem_tipo  TEXT NOT NULL CHECK (origem_tipo IN ('conta','carteira')),
	origem_id    INTEGER NOT NULL,
	destino_tipo TEXT NOT NULL CHECK (destino_tipo IN ('conta','carteira')),
	destino_id   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS recorrencias (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tipo        TEXT NOT NULL CHECK (tipo IN ('pagar','receber')),
	descricao   TEXT NOT NULL,
	valor       INTEGER NOT NULL,
	categoria   TEXT NOT NULL DEFAULT 'geral',
	dia         INTEGER NOT NULL CHECK (dia BETWEEN 1 AND 31),
	conta_id    INTEGER REFERENCES contas(id) ON DELETE SET NULL,
	carteira_id INTEGER REFERENCES carteiras(id) ON DELETE SET NULL,
	inicio      TEXT NOT NULL,                      -- AAAA-MM-DD
	fim         TEXT,                               -- AAAA-MM-DD, NULL = sem fim
	ultima_ref  TEXT NOT NULL DEFAULT '',           -- último AAAA-MM materializado
	cartao_id   INTEGER REFERENCES cartoes(id) ON DELETE SET NULL, -- gera os lançamentos na fatura
	assinatura  INTEGER NOT NULL DEFAULT 0,         -- 1 = é uma assinatura (Netflix, Spotify...)
	criada_em   TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS comprovantes (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	lancamento_id INTEGER NOT NULL REFERENCES lancamentos(id) ON DELETE CASCADE,
	file_id       TEXT NOT NULL,                  -- id do arquivo no Telegram
	criado_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS grupos (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	nome      TEXT NOT NULL,
	criado_em TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS categorias (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	nome      TEXT NOT NULL UNIQUE,
	criada_em TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS grupo_pessoas (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	grupo_id INTEGER NOT NULL REFERENCES grupos(id) ON DELETE CASCADE,
	nome     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gp_grupo ON grupo_pessoas (grupo_id);

CREATE TABLE IF NOT EXISTS cartoes (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	nome           TEXT NOT NULL,
	limite         INTEGER NOT NULL DEFAULT 0,       -- centavos (0 = não informado)
	dia_fechamento INTEGER NOT NULL CHECK (dia_fechamento BETWEEN 1 AND 31),
	dia_vencimento INTEGER NOT NULL CHECK (dia_vencimento BETWEEN 1 AND 31),
	conta_id       INTEGER REFERENCES contas(id) ON DELETE SET NULL, -- conta que paga a fatura
	criado_em      TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_lanc_venc   ON lancamentos (vencimento);
CREATE INDEX IF NOT EXISTS idx_lanc_status ON lancamentos (status);

-- módulo empresa (prisma --empresa): sócios com participação própria (não dá
-- pra reaproveitar "grupo", que só divide igualmente entre as pessoas).
CREATE TABLE IF NOT EXISTS socios (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	nome         TEXT NOT NULL,
	participacao REAL NOT NULL CHECK (participacao > 0 AND participacao <= 100),
	criado_em    TEXT NOT NULL DEFAULT (date('now','localtime'))
);

-- aporte de capital social: o valor/data ficam só no lançamento vinculado
-- (mesmo padrão de lancamentos.reembolso_de) para não duplicar dado.
CREATE TABLE IF NOT EXISTS aportes_capital (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	socio_id      INTEGER NOT NULL REFERENCES socios(id),
	lancamento_id INTEGER NOT NULL REFERENCES lancamentos(id) ON DELETE CASCADE,
	criado_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS distribuicoes_lucro (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	data        TEXT NOT NULL,
	lucro_total INTEGER NOT NULL, -- centavos; o valor que foi distribuído
	observacao  TEXT NOT NULL DEFAULT '',
	criado_em   TEXT NOT NULL DEFAULT (date('now','localtime'))
);

-- a parte de cada sócio numa distribuição; o valor fica no lançamento.
CREATE TABLE IF NOT EXISTS distribuicao_socios (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	distribuicao_id INTEGER NOT NULL REFERENCES distribuicoes_lucro(id) ON DELETE CASCADE,
	socio_id        INTEGER NOT NULL REFERENCES socios(id),
	lancamento_id   INTEGER NOT NULL REFERENCES lancamentos(id) ON DELETE CASCADE
);
`

func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(schema); err != nil {
		return err
	}
	// bancos criados antes das recorrências não têm a coluna de vínculo
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('lancamentos') WHERE name = 'recorrencia_id'`,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := conn.Exec(
			`ALTER TABLE lancamentos ADD COLUMN recorrencia_id INTEGER REFERENCES recorrencias(id) ON DELETE SET NULL`,
		); err != nil {
			return err
		}
	}
	// bancos criados antes dos grupos não têm a coluna de vínculo
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('lancamentos') WHERE name = 'grupo_id'`,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := conn.Exec(
			`ALTER TABLE lancamentos ADD COLUMN grupo_id INTEGER REFERENCES grupos(id) ON DELETE SET NULL`,
		); err != nil {
			return err
		}
	}
	// bancos criados antes dos cartões não têm as colunas de cartão/compra
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('lancamentos') WHERE name = 'cartao_id'`,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for _, ddl := range []string{
			`ALTER TABLE lancamentos ADD COLUMN cartao_id INTEGER REFERENCES cartoes(id) ON DELETE SET NULL`,
			`ALTER TABLE lancamentos ADD COLUMN data_compra TEXT`, // data da compra no cartão (competência)
		} {
			if _, err := conn.Exec(ddl); err != nil {
				return err
			}
		}
	}
	// bancos antigos não têm cartão nem assinatura na recorrência
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('recorrencias') WHERE name = 'cartao_id'`,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for _, ddl := range []string{
			`ALTER TABLE recorrencias ADD COLUMN cartao_id INTEGER REFERENCES cartoes(id) ON DELETE SET NULL`,
			`ALTER TABLE recorrencias ADD COLUMN assinatura INTEGER NOT NULL DEFAULT 0`,
		} {
			if _, err := conn.Exec(ddl); err != nil {
				return err
			}
		}
	}
	// colunas novas das melhorias de 2026: parcelas vinculadas, auto-quitar,
	// observação nos lançamentos e grupo nas recorrências.
	colunas := []struct{ tabela, coluna, ddl string }{
		{"lancamentos", "parcela_grupo", `ALTER TABLE lancamentos ADD COLUMN parcela_grupo INTEGER`},
		{"lancamentos", "auto_quitar", `ALTER TABLE lancamentos ADD COLUMN auto_quitar INTEGER NOT NULL DEFAULT 0`},
		{"lancamentos", "observacao", `ALTER TABLE lancamentos ADD COLUMN observacao TEXT NOT NULL DEFAULT ''`},
		{"recorrencias", "grupo_id", `ALTER TABLE recorrencias ADD COLUMN grupo_id INTEGER REFERENCES grupos(id) ON DELETE SET NULL`},
		{"recorrencias", "auto_quitar", `ALTER TABLE recorrencias ADD COLUMN auto_quitar INTEGER NOT NULL DEFAULT 0`},
		{"lancamentos", "recebe_pagamento", `ALTER TABLE lancamentos ADD COLUMN recebe_pagamento INTEGER NOT NULL DEFAULT 0`},
		{"lancamentos", "reembolso_de", `ALTER TABLE lancamentos ADD COLUMN reembolso_de INTEGER REFERENCES lancamentos(id) ON DELETE CASCADE`},
	}
	for _, c := range colunas {
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.tabela, c.coluna,
		).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := conn.Exec(c.ddl); err != nil {
				return err
			}
		}
	}
	// catálogo de categorias: semeia a partir das já usadas nos lançamentos e
	// nas recorrências (idempotente — categorias.nome é UNIQUE).
	if _, err := conn.Exec(`
		INSERT OR IGNORE INTO categorias (nome)
		SELECT DISTINCT categoria FROM lancamentos WHERE categoria <> ''
		UNION SELECT DISTINCT categoria FROM recorrencias WHERE categoria <> ''`); err != nil {
		return err
	}
	return nil
}
