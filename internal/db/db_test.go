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

// TestOpenEmpresaIndependenteDoPessoal garante que `prisma --empresa` usa um
// arquivo totalmente separado do banco pessoal, configurável por
// PRISMA_EMPRESA_DB do mesmo jeito que PRISMA_DB configura o pessoal.
func TestOpenEmpresaIndependenteDoPessoal(t *testing.T) {
	dir := t.TempDir()
	pessoal := filepath.Join(dir, "pessoal.db")
	empresa := filepath.Join(dir, "empresa.db")
	t.Setenv("PRISMA_DB", pessoal)
	t.Setenv("PRISMA_EMPRESA_DB", empresa)

	connP, err := Open()
	if err != nil {
		t.Fatalf("abrindo banco pessoal: %v", err)
	}
	defer connP.Close()
	connE, err := OpenEmpresa()
	if err != nil {
		t.Fatalf("abrindo banco da empresa: %v", err)
	}
	defer connE.Close()

	if _, err := connP.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Pessoal', 100)`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := connE.QueryRow(`SELECT COUNT(*) FROM contas`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("banco da empresa viu %d conta(s) do pessoal, queria 0 (arquivos deveriam ser independentes)", n)
	}
	if _, err := os.Stat(pessoal); err != nil {
		t.Errorf("arquivo do banco pessoal não foi criado em %s: %v", pessoal, err)
	}
	if _, err := os.Stat(empresa); err != nil {
		t.Errorf("arquivo do banco da empresa não foi criado em %s: %v", empresa, err)
	}
}
