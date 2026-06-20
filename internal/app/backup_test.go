package app

import (
	"os"
	"path/filepath"
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
