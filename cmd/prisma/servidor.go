package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"prisma/internal/app"
	"prisma/internal/db"
	"prisma/internal/remote"
)

// rodarServidor abre o banco local e o disponibiliza na rede para clientes
// Prisma. Fica em primeiro plano até receber Ctrl-C / SIGTERM.
func rodarServidor(cfg remote.Config, args []string) error {
	fs := flag.NewFlagSet("servidor", flag.ContinueOnError)
	porta := fs.Int("porta", cfg.Porta, "porta TCP de escuta")
	token := fs.String("token", cfg.Token, "token compartilhado (ou use PRISMA_TOKEN)")
	endereco := fs.String("endereco", "0.0.0.0", "endereço de escuta (0.0.0.0 = toda a rede)")
	semTLS := fs.Bool("sem-tls", false, "desliga a criptografia (NÃO recomendado; só para teste)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" {
		return fmt.Errorf("defina um token: prisma servidor --token SEGREDO  (ou a variável PRISMA_TOKEN)")
	}
	usarTLS := !*semTLS

	// o servidor é o dono do arquivo: abre o banco local de verdade.
	conn, err := db.Open()
	if err != nil {
		return err
	}
	defer conn.Close()

	// materializa as recorrências uma vez ao subir, como faria um comando normal.
	if _, err := app.GerarRecorrencias(conn); err != nil {
		return fmt.Errorf("recorrências: %w", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *endereco, *porta))
	if err != nil {
		return fmt.Errorf("escutando em %s:%d: %w", *endereco, *porta, err)
	}

	var fingerprint string
	if usarTLS {
		cert, fp, err := remote.CarregaOuGeraCert()
		if err != nil {
			return fmt.Errorf("certificado TLS: %w", err)
		}
		fingerprint = fp
		ln = tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
	}

	srv := remote.NovoServidor(conn, *token)
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	imprimePareamento(ipsLAN(), *porta, *token, fingerprint, usarTLS)
	return srv.Serve(ctx, ln)
}

// imprimePareamento mostra o comando pronto que o usuário copia e cola no
// cliente, já com host, token e (com TLS) fingerprint preenchidos.
func imprimePareamento(ips []string, porta int, token, fingerprint string, tls bool) {
	fmt.Println("Prisma servidor no ar.")
	if !tls {
		fmt.Println("⚠  SEM TLS: o token e os dados trafegam em claro. Só para teste em rede confiável.")
	}
	fmt.Println("\nNo outro computador (cliente), copie e cole este comando:")
	fmt.Printf("\n  %s\n", comandoCliente(ips[0], porta, token, fingerprint, tls))

	if len(ips) > 1 {
		fmt.Printf("\nSe o endereço %s não funcionar, troque o --host por um destes:\n", ips[0])
		for _, ip := range ips[1:] {
			fmt.Printf("  %s\n", ip)
		}
	}
	fmt.Println("\nPara desfazer no cliente depois: prisma config local")
	fmt.Println("\nServidor escutando. Ctrl-C para parar.")
}

// comandoCliente monta a linha `prisma config cliente ...` com os campos certos.
func comandoCliente(host string, porta int, token, fingerprint string, tls bool) string {
	cmd := fmt.Sprintf("prisma config cliente --host %s --token %s", host, token)
	if porta != remote.PortaPadrao {
		cmd += fmt.Sprintf(" --porta %d", porta)
	}
	if tls {
		cmd += fmt.Sprintf(" --fingerprint %s", fingerprint)
	} else {
		cmd += " --sem-tls"
	}
	return cmd
}

// ipsLAN devolve os IPs IPv4 não-loopback das interfaces, para o usuário saber
// qual endereço informar no cliente.
func ipsLAN() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			ips = append(ips, v4.String())
		}
	}
	if len(ips) == 0 {
		ips = []string{"127.0.0.1"}
	}
	return ips
}
