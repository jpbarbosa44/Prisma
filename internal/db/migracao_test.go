package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigracaoBancoAntigo simula um banco de uma versão antiga (lancamentos só
// com as colunas originais) e confere que migrate adiciona todas as colunas e
// índices novos, preserva os dados e é idempotente.
func TestMigracaoBancoAntigo(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "antigo.db")
	conn, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// banco "v0.x": lancamentos sem nenhuma das colunas que vieram depois
	if _, err := conn.Exec(`CREATE TABLE lancamentos (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		tipo        TEXT NOT NULL,
		descricao   TEXT NOT NULL,
		valor       INTEGER NOT NULL,
		categoria   TEXT NOT NULL DEFAULT 'geral',
		vencimento  TEXT NOT NULL,
		status      TEXT NOT NULL DEFAULT 'pendente',
		quitado_em  TEXT,
		conta_id    INTEGER,
		carteira_id INTEGER,
		criado_em   TEXT NOT NULL DEFAULT (date('now','localtime'))
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(
		`INSERT INTO lancamentos (tipo, descricao, valor, vencimento) VALUES ('pagar','Aluguel',120000,'2026-06-10')`,
	); err != nil {
		t.Fatal(err)
	}

	if err := migrate(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// todas as colunas adicionadas por migração devem existir agora
	for _, col := range []string{
		"recorrencia_id", "grupo_id", "cartao_id", "data_compra",
		"parcela_grupo", "auto_quitar", "observacao", "recebe_pagamento", "reembolso_de",
	} {
		var n int
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('lancamentos') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("coluna %q não foi adicionada pela migração", col)
		}
	}

	// o dado antigo sobreviveu intacto
	var desc string
	var valor int64
	if err := conn.QueryRow(`SELECT descricao, valor FROM lancamentos WHERE id = 1`).Scan(&desc, &valor); err != nil {
		t.Fatal(err)
	}
	if desc != "Aluguel" || valor != 120000 {
		t.Errorf("dado antigo corrompido: descricao=%q valor=%d", desc, valor)
	}

	// os índices secundários novos foram criados
	for _, idx := range []string{"idx_lanc_rec", "idx_lanc_cartao", "idx_lanc_parcela", "idx_lanc_reembolso"} {
		var n int
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name = ?`, idx).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("índice %q não foi criado", idx)
		}
	}

	// idempotência: aplicar de novo não pode falhar
	if err := migrate(conn); err != nil {
		t.Fatalf("migrate na segunda vez: %v", err)
	}
}
