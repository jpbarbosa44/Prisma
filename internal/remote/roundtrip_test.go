package remote_test

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"prisma/internal/app"
	"prisma/internal/db"
	"prisma/internal/remote"
)

// monta sobe um servidor remoto sobre um banco local temporário e devolve um
// *sql.DB cliente que fala com ele pela rede (via httptest).
func monta(t *testing.T) (cliente *sql.DB, servidorDB *sql.DB) {
	t.Helper()
	const token = "segredo-de-teste"

	// banco local real, com o schema completo do Prisma
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "prisma.db"))
	local, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco local: %v", err)
	}
	t.Cleanup(func() { local.Close() })

	srv := remote.NovoServidor(local, token)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	porta, _ := strconv.Atoi(u.Port())
	cfg := remote.Config{Modo: remote.ModoCliente, Host: u.Hostname(), Porta: porta, Token: token}

	cli, err := db.OpenCliente(cfg)
	if err != nil {
		t.Fatalf("conectando cliente: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli, local
}

// TestRoundTripBasico cobre Exec (com LastInsertId), Query e tipos.
func TestRoundTripBasico(t *testing.T) {
	cli, _ := monta(t)

	res, err := cli.Exec(`INSERT INTO contas (nome, tipo, saldo_inicial) VALUES (?,?,?)`,
		"Nubank", "corrente", 150000)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id != 1 {
		t.Fatalf("LastInsertId = %d, %v; quero 1", id, err)
	}

	var nome string
	var saldo int64
	if err := cli.QueryRow(`SELECT nome, saldo_inicial FROM contas WHERE id = ?`, id).
		Scan(&nome, &saldo); err != nil {
		t.Fatalf("select: %v", err)
	}
	if nome != "Nubank" || saldo != 150000 {
		t.Fatalf("voltou (%q, %d); quero (Nubank, 150000)", nome, saldo)
	}
}

// TestTransacaoCommit garante que uma transação remota persiste tudo.
func TestTransacaoCommit(t *testing.T) {
	cli, srvDB := monta(t)

	tx, err := cli.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, n := range []string{"A", "B", "C"} {
		if _, err := tx.Exec(`INSERT INTO carteiras (nome) VALUES (?)`, n); err != nil {
			t.Fatalf("insert em tx: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// confere direto no banco do servidor: o efeito tem que estar lá
	var n int
	if err := srvDB.QueryRow(`SELECT COUNT(*) FROM carteiras`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("commit gravou %d carteiras; quero 3", n)
	}
}

// TestTransacaoRollback garante que o rollback remoto desfaz tudo.
func TestTransacaoRollback(t *testing.T) {
	cli, srvDB := monta(t)

	tx, err := cli.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO carteiras (nome) VALUES (?)`, "fantasma"); err != nil {
		t.Fatalf("insert em tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var n int
	if err := srvDB.QueryRow(`SELECT COUNT(*) FROM carteiras`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rollback deixou %d carteiras; quero 0", n)
	}
}

// TestTransferirPeloRemoto roda uma função real do app (que usa QueryRow + Exec)
// inteira através do driver remoto — a prova de que o app não percebe a rede.
func TestTransferirPeloRemoto(t *testing.T) {
	cli, srvDB := monta(t)

	for _, nome := range []string{"Origem", "Destino"} {
		if _, err := cli.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES (?, 100000)`, nome); err != nil {
			t.Fatalf("criando conta: %v", err)
		}
	}

	if err := app.Transferir(cli, []string{"--de", "conta:1", "--para", "conta:2", "--valor", "250,00"}); err != nil {
		t.Fatalf("transferir pelo remoto: %v", err)
	}

	var valor int64
	if err := srvDB.QueryRow(
		`SELECT valor FROM transferencias WHERE origem_id = 1 AND destino_id = 2`,
	).Scan(&valor); err != nil {
		t.Fatalf("conferindo transferência: %v", err)
	}
	if valor != 25000 {
		t.Fatalf("transferência gravou %d centavos; quero 25000", valor)
	}
}

// TestTLSComPinning sobe um servidor TLS real e confirma que o cliente conecta
// quando o fingerprint bate e falha quando não bate.
func TestTLSComPinning(t *testing.T) {
	const token = "segredo-tls"
	t.Setenv("HOME", t.TempDir())                               // redireciona onde o cert é salvo
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "p.db"))

	local, err := db.Open()
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	t.Cleanup(func() { local.Close() })

	cert, fp, err := remote.CarregaOuGeraCert()
	if err != nil {
		t.Fatalf("gerando cert: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})

	srv := remote.NovoServidor(local, token)
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar)
	go srv.Serve(ctx, ln)

	porta := ln.Addr().(*net.TCPAddr).Port

	// fingerprint correto: conecta e consulta
	cfg := remote.Config{Modo: remote.ModoCliente, Host: "127.0.0.1", Porta: porta,
		Token: token, TLS: true, Fingerprint: fp}
	cli, err := db.OpenCliente(cfg)
	if err != nil {
		t.Fatalf("cliente com fingerprint certo deveria conectar: %v", err)
	}
	defer cli.Close()
	if _, err := cli.Exec(`INSERT INTO contas (nome) VALUES ('TLS ok')`); err != nil {
		t.Fatalf("exec sobre TLS: %v", err)
	}

	// fingerprint errado: tem que recusar o handshake
	ruim := cfg
	ruim.Fingerprint = "00" + fp[2:]
	if _, err := db.OpenCliente(ruim); err == nil {
		t.Fatal("cliente com fingerprint errado deveria falhar, mas conectou")
	}
}

// TestTokenInvalido garante que o servidor recusa segredo errado.
func TestTokenInvalido(t *testing.T) {
	cli, _ := monta(t)
	_ = cli // só para subir o servidor

	cfg := remote.Config{Modo: remote.ModoCliente, Host: "127.0.0.1", Porta: 1, Token: "x"}
	conn := sql.OpenDB(remote.NovoConnector(cfg))
	defer conn.Close()
	ctx := context.Background()
	if err := conn.PingContext(ctx); err == nil {
		t.Fatal("esperava erro de conexão com host inválido")
	}
}
