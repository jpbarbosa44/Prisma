package db

import (
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

	// sem banco ainda: não cria nada nem reclama
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("sem banco: %v", err)
	}
	if _, err := os.Stat(pastaBk); !os.IsNotExist(err) {
		t.Fatal("não devia criar a pasta sem haver banco")
	}

	// primeiro backup do dia
	if err := os.WriteFile(caminho, []byte("versão da manhã"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("primeiro backup: %v", err)
	}
	alvo := filepath.Join(pastaBk, "prisma-"+hoje+".db")
	dados, err := os.ReadFile(alvo)
	if err != nil || string(dados) != "versão da manhã" {
		t.Fatalf("backup de hoje errado: %q, err=%v", dados, err)
	}

	// segunda chamada no mesmo dia não sobrescreve
	os.WriteFile(caminho, []byte("versão da tarde"), 0o644)
	if err := backupDiario(caminho); err != nil {
		t.Fatalf("segunda chamada: %v", err)
	}
	dados, _ = os.ReadFile(alvo)
	if string(dados) != "versão da manhã" {
		t.Errorf("backup do dia foi sobrescrito: %q", dados)
	}

	// expurgo: com 10 cópias antigas, ficam só as maxBackups mais novas
	for i := 1; i <= 10; i++ {
		nome := filepath.Join(pastaBk, fmt.Sprintf("prisma-2020-01-%02d.db", i))
		os.WriteFile(nome, []byte("velho"), 0o644)
	}
	os.Remove(alvo) // libera para criar o de hoje de novo e disparar o expurgo
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
