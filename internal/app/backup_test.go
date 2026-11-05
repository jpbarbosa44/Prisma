package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"prisma/internal/db"
)

// TestRestaurar confere o ciclo: faz um backup, muda o banco, restaura e o
// estado volta ao do backup — guardando antes uma cópia de segurança.
func TestRestaurar(t *testing.T) {
	conn := abreDB(t)
	dbPath := os.Getenv("PRISMA_DB")
	if _, err := conn.Exec(`INSERT INTO contas (nome) VALUES ('Original')`); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "snap.db")
	if err := db.Snapshot(dbPath, backup); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// muda o banco depois do backup
	if _, err := conn.Exec(`INSERT INTO contas (nome) VALUES ('Depois')`); err != nil {
		t.Fatal(err)
	}

	silencia(t, func() error { return Restaurar([]string{"--arquivo", backup}, false) })

	// conexão nova: deve ler o estado restaurado (só "Original")
	novo, err := db.Open()
	if err != nil {
		t.Fatalf("reabrindo: %v", err)
	}
	defer novo.Close()
	var n int
	if err := novo.QueryRow(`SELECT COUNT(*) FROM contas`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("após restaurar, %d conta(s), quer 1 (só 'Original')", n)
	}
	var nome string
	if err := novo.QueryRow(`SELECT nome FROM contas`).Scan(&nome); err == nil && nome != "Original" {
		t.Errorf("conta restaurada = %q, quer 'Original'", nome)
	}
	// a cópia de segurança do estado anterior foi criada
	pre, _ := filepath.Glob(filepath.Join(filepath.Dir(dbPath), "backups", "*pre-restauracao*"))
	if len(pre) == 0 {
		t.Error("restaurar não guardou a cópia de segurança do estado atual")
	}
}

// TestRestaurarBackupCorrompido recusa um arquivo que não é um SQLite válido,
// sem tocar no banco.
func TestRestaurarBackupCorrompido(t *testing.T) {
	abreDB(t)
	ruim := filepath.Join(t.TempDir(), "ruim.db")
	if err := os.WriteFile(ruim, []byte("isto não é um banco"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restaurar([]string{"--arquivo", ruim}, false); err == nil {
		t.Error("restaurar deveria recusar um backup corrompido")
	}
}

// TestVerificar reporta o banco como íntegro e sem violações de FK.
func TestVerificar(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome) VALUES ('X')`); err != nil {
		t.Fatal(err)
	}
	out := capturaSaida(t, func() error { return Verificar(conn, false) })
	if !strings.Contains(out, "íntegro") || !strings.Contains(out, "sem violações") {
		t.Errorf("verificar não reportou banco íntegro/sem violações:\n%s", out)
	}
}

// TestResetarEmpresaBackup: zerar o banco da empresa faz o backup com o nome
// dele (empresa.db.bak-*), e o `restaurar --empresa` o encontra; o pessoal não.
func TestResetarEmpresaBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRISMA_DB", filepath.Join(dir, "prisma.db"))
	t.Setenv("PRISMA_EMPRESA_DB", filepath.Join(dir, "empresa.db"))
	conn, err := db.OpenEmpresa()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO contas (nome) VALUES ('Caixa')`); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error { return Resetar(conn, []string{"--sim"}, true) })

	baks, _ := filepath.Glob(filepath.Join(dir, "empresa.db.bak-*"))
	if len(baks) != 1 {
		t.Fatalf("backup da empresa: %v, quer 1 arquivo empresa.db.bak-*", baks)
	}
	if outros, _ := filepath.Glob(filepath.Join(dir, "prisma.db.bak-*")); len(outros) != 0 {
		t.Fatalf("não devia criar backup com o nome do banco pessoal: %v", outros)
	}
	achou := func(lista []backupInfo, alvo string) bool {
		for _, b := range lista {
			if b.caminho == alvo {
				return true
			}
		}
		return false
	}
	if !achou(listarBackups(filepath.Join(dir, "empresa.db")), baks[0]) {
		t.Error("restaurar --empresa devia listar o backup da empresa")
	}
	if achou(listarBackups(filepath.Join(dir, "prisma.db")), baks[0]) {
		t.Error("restaurar (pessoal) não devia listar o backup da empresa")
	}
}

// TestListarBackupsSeparaBancos: na pasta backups/ compartilhada, cada banco só
// vê as próprias cópias diárias; os snapshots do MCP são do pessoal.
func TestListarBackupsSeparaBancos(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRISMA_EMPRESA_DB", filepath.Join(dir, "empresa.db"))
	bk := filepath.Join(dir, "backups")
	os.MkdirAll(bk, 0o755)
	for _, n := range []string{"prisma-2026-11-01.db", "empresa-2026-11-01.db", "mcp-antes-20261101-100000.db"} {
		os.WriteFile(filepath.Join(bk, n), []byte("x"), 0o644)
	}
	nomes := func(dbPath string) string {
		var out []string
		for _, b := range listarBackups(dbPath) {
			out = append(out, filepath.Base(b.caminho))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	if got := nomes(filepath.Join(dir, "prisma.db")); got != "mcp-antes-20261101-100000.db prisma-2026-11-01.db" {
		t.Errorf("pessoal: %q", got)
	}
	if got := nomes(filepath.Join(dir, "empresa.db")); got != "empresa-2026-11-01.db" {
		t.Errorf("empresa: %q", got)
	}
}
