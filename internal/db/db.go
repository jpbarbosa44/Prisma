// Package db abre o banco SQLite do Prisma e aplica as migrações.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// Path retorna o caminho do banco: $PRISMA_DB ou o diretório de dados
// padrão de cada sistema (Linux: ~/.local/share; macOS: ~/Library/Application
// Support; Windows: %AppData%).
func Path() (string, error) {
	if p := os.Getenv("PRISMA_DB"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "prisma", "prisma.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "prisma", "prisma.db"), nil
}

// Open abre (criando se necessário) o banco e aplica o schema. Antes de
// abrir, faz o backup diário — a cópia retrata o banco antes da sessão.
func Open() (*sql.DB, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("criando diretório do banco: %w", err)
	}
	if err := backupDiario(p); err != nil {
		// backup falho não pode impedir o uso; só avisa
		fmt.Fprintf(os.Stderr, "aviso: backup diário falhou: %v\n", err)
	}
	conn, err := sql.Open("sqlite", p+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	dados, err := os.ReadFile(caminho)
	if err != nil {
		return err
	}
	if err := os.WriteFile(alvo, dados, 0o600); err != nil {
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
	criado_em   TEXT NOT NULL DEFAULT (date('now','localtime'))
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
	criada_em   TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE TABLE IF NOT EXISTS comprovantes (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	lancamento_id INTEGER NOT NULL REFERENCES lancamentos(id) ON DELETE CASCADE,
	file_id       TEXT NOT NULL,                  -- id do arquivo no Telegram
	criado_em     TEXT NOT NULL DEFAULT (date('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_lanc_venc   ON lancamentos (vencimento);
CREATE INDEX IF NOT EXISTS idx_lanc_status ON lancamentos (status);
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
	return nil
}
