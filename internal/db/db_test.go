package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupDiario(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "prisma.db")
	pastaBk := filepath.Join(dir, "backups")
	hoje := time.Now().Format("2006-01-02")

	// grava um valor reconhecível num banco SQLite de verdade
	gravaBanco := func(valor string) {
		t.Helper()
		conn, err := sql.Open("sqlite", caminho)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS t (v TEXT)`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM t`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO t (v) VALUES (?)`, valor); err != nil {
			t.Fatal(err)
		}
	}
	leBackup := func(alvo string) string {
		t.Helper()
		conn, err := sql.Open("sqlite", alvo)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var v string
		if err := conn.QueryRow(`SELECT v FROM t`).Scan(&v); err != nil {
			t.Fatalf("lendo backup %s: %v", alvo, err)
		}
		return v
	}

	// sem banco ainda: não cria nada nem reclama
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("sem banco: %v", err)
	}
	if _, err := os.Stat(pastaBk); !os.IsNotExist(err) {
		t.Fatal("não devia criar a pasta sem haver banco")
	}

	// primeiro backup do dia: um snapshot válido com o dado da manhã
	gravaBanco("manha")
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("primeiro backup: %v", err)
	}
	alvo := filepath.Join(pastaBk, "prisma-"+hoje+".db")
	if v := leBackup(alvo); v != "manha" {
		t.Fatalf("backup de hoje = %q, quer manha", v)
	}

	// segunda chamada no mesmo dia não sobrescreve
	gravaBanco("tarde")
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("segunda chamada: %v", err)
	}
	if v := leBackup(alvo); v != "manha" {
		t.Errorf("backup do dia foi sobrescrito: %q", v)
	}

	// expurgo: com 10 cópias antigas, ficam só as maxBackups mais novas
	for i := 1; i <= 10; i++ {
		nome := filepath.Join(pastaBk, fmt.Sprintf("prisma-2020-01-%02d.db", i))
		os.WriteFile(nome, []byte("velho"), 0o644)
	}
	os.Remove(alvo) // libera para recriar o de hoje e disparar o expurgo
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("expurgo: %v", err)
	}
	nomes, _ := filepath.Glob(filepath.Join(pastaBk, "prisma-*.db"))
	if len(nomes) != maxBackups {
		t.Errorf("expurgo devia deixar %d cópias, deixou %d: %v", maxBackups, len(nomes), nomes)
	}
	if _, err := os.Stat(alvo); err != nil {
		t.Error("a cópia de hoje (mais nova) devia sobreviver ao expurgo")
	}
}
