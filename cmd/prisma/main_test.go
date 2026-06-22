package main

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"prisma/internal/db"
)

// dbTeste abre um banco temporário migrado, isolado por teste.
func dbTeste(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "teste.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// semSaida roda f descartando o stdout (os comandos imprimem) e devolve o erro.
func semSaida(t *testing.T, f func() error) error {
	t.Helper()
	antigo := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := f()
	w.Close()
	os.Stdout = antigo
	io.Copy(io.Discard, r)
	r.Close()
	return err
}

// TestDespachaRoteia garante que os comandos principais (e um apelido) chegam à
// função certa e rodam sem erro sobre um banco real. Pega regressões de
// roteamento: um case removido, um typo de comando, um apelido que parou de valer.
func TestDespachaRoteia(t *testing.T) {
	conn := dbTeste(t)
	passos := []struct {
		nome string
		cmd  string
		args []string
	}{
		{"conta add", "conta", []string{"add", "--nome", "Corrente", "--saldo", "100"}},
		{"pagar add", "pagar", []string{"add", "--desc", "Mercado", "--valor", "50", "--venc", "2026-06-10"}},
		{"receber add", "receber", []string{"add", "--desc", "Salário", "--valor", "5000", "--venc", "2026-06-05"}},
		{"transferir falta destino", "saldo", nil},
		{"lancamentos", "lancamentos", nil},
		{"cartoes (apelido)", "cartoes", nil},
		{"recorrencias (apelido)", "recorrencias", nil},
		{"previsao", "previsao", nil},
		{"estatisticas", "estatisticas", []string{"--meses", "1"}},
	}
	for _, p := range passos {
		if err := semSaida(t, func() error { return despacha(conn, false, p.cmd, p.args) }); err != nil {
			t.Fatalf("%s: erro inesperado: %v", p.nome, err)
		}
	}
}

// TestDespachaComandoDesconhecido garante o sinal de comando inválido (que o main
// usa para imprimir a ajuda e sair com código 2).
func TestDespachaComandoDesconhecido(t *testing.T) {
	conn := dbTeste(t)
	err := semSaida(t, func() error { return despacha(conn, false, "xpto", nil) })
	if !errors.Is(err, errComandoDesconhecido) {
		t.Fatalf("esperava errComandoDesconhecido, veio %v", err)
	}
}

// TestDespachaErroPropaga garante que um erro de um comando (ex.: argumentos
// inválidos) sobe — o main o transforma em saída de erro / código 1.
func TestDespachaErroPropaga(t *testing.T) {
	conn := dbTeste(t)
	err := semSaida(t, func() error {
		return despacha(conn, false, "pagar", []string{"add"}) // sem --desc/--valor
	})
	if err == nil {
		t.Fatal("esperava erro de argumentos faltando, veio nil")
	}
	if errors.Is(err, errComandoDesconhecido) {
		t.Fatalf("erro deveria ser de validação, não de comando desconhecido: %v", err)
	}
}
