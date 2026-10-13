package main

import (
	"database/sql"
	"fmt"
	"os"

	"prisma/internal/db"
	"prisma/internal/mcp"
	"prisma/internal/remote"
)

// rodarMCP atende `prisma mcp [--escrita]` no stdio.
//
// Somente leitura (padrão): uma conexão query_only com o banco pessoal local.
// Com --escrita: a conexão de gravação é aberta como nos demais comandos
// (local, ou o servidor no modo cliente); as consultas continuam numa conexão
// query_only separada quando o banco é local. No modo cliente não há como abrir
// o banco remoto só para leitura, então a consulta_sql livre fica desligada.
func rodarMCP(args []string) error {
	escrita := false
	for _, a := range args {
		switch a {
		case "--escrita", "escrita":
			escrita = true
		default:
			return fmt.Errorf("opção desconhecida %q (use: prisma mcp [--escrita])", a)
		}
	}
	if !escrita {
		conn, err := db.AbrirAnalytics()
		if err != nil {
			return err
		}
		defer conn.Close()
		return mcp.Servir(conn, os.Stdin, os.Stdout)
	}

	cfg, err := remote.Carrega()
	if err != nil {
		return fmt.Errorf("configuração: %w", err)
	}
	grava, err := db.Abrir(cfg)
	if err != nil {
		return err
	}
	defer grava.Close()

	leitura, sqlLivre, banco := grava, false, ""
	if cfg.Modo != remote.ModoCliente {
		var ro *sql.DB
		if ro, err = db.AbrirAnalytics(); err != nil {
			return err
		}
		defer ro.Close()
		leitura, sqlLivre = ro, true
		if banco, err = db.Path(); err != nil {
			return err
		}
	}
	return mcp.ServirComEscrita(leitura, grava, sqlLivre, banco, os.Stdin, os.Stdout)
}
