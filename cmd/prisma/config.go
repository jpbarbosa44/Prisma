package main

import (
	"flag"
	"fmt"

	"prisma/internal/db"
	"prisma/internal/remote"
)

const ajudaConfig = `prisma config — modo de operação (local ou cliente de um servidor)

USO
  prisma config                       mostra a configuração atual
  prisma config cliente [opções]      conecta a um servidor Prisma na rede
  prisma config local                 volta ao modo normal (banco local)

OPÇÕES de "config cliente"
  --host IP          endereço do servidor (obrigatório)
  --token SEGREDO    token combinado com o servidor (obrigatório)
  --fingerprint X    impressão digital do certificado (o servidor mostra)
  --porta N          porta do servidor (padrão 8456)
  --sem-tls          conecta sem criptografia (só em rede confiável)
`

// configurar gerencia o arquivo de configuração: ver, virar cliente, voltar a
// local. Não depende do banco, então roda antes de qualquer conexão.
func configurar(atual remote.Config, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "mostrar", "status":
		return mostrarConfig(atual)
	case "cliente", "conectar":
		return configCliente(args[1:])
	case "local", "normal", "desconectar":
		return configLocal()
	case "ajuda", "help", "-h", "--help":
		fmt.Print(ajudaConfig)
		return nil
	default:
		return fmt.Errorf("subcomando desconhecido %q (use: mostrar, cliente ou local)", sub)
	}
}

func mostrarConfig(cfg remote.Config) error {
	caminho, _ := remote.CaminhoConfig()
	fmt.Printf("Modo atual: %s\n", cfg.Modo)
	if cfg.Modo == remote.ModoCliente {
		fmt.Printf("  Servidor:    %s:%d\n", cfg.Host, cfg.Porta)
		if cfg.TLS {
			fmt.Printf("  TLS:         ligado (fingerprint %s…)\n", trunc12(cfg.Fingerprint))
		} else {
			fmt.Printf("  TLS:         desligado\n")
		}
	} else {
		caminhoBanco, _ := db.Path()
		fmt.Printf("  Banco local: %s\n", caminhoBanco)
	}
	fmt.Printf("Arquivo de configuração: %s\n", caminho)
	return nil
}

func configCliente(args []string) error {
	fs := flag.NewFlagSet("config cliente", flag.ContinueOnError)
	host := fs.String("host", "", "endereço do servidor (obrigatório)")
	token := fs.String("token", "", "token combinado com o servidor (obrigatório)")
	fingerprint := fs.String("fingerprint", "", "impressão digital do certificado do servidor")
	porta := fs.Int("porta", remote.PortaPadrao, "porta do servidor")
	semTLS := fs.Bool("sem-tls", false, "conecta sem criptografia (só em rede confiável)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host == "" || *token == "" {
		return fmt.Errorf("--host e --token são obrigatórios (veja `prisma config ajuda`)")
	}

	cfg := remote.Config{
		Modo:        remote.ModoCliente,
		Host:        *host,
		Porta:       *porta,
		Token:       *token,
		TLS:         !*semTLS,
		Fingerprint: *fingerprint,
	}
	if cfg.TLS && cfg.Fingerprint == "" {
		return fmt.Errorf("com TLS é preciso o --fingerprint (o `prisma servidor` mostra); " +
			"ou use --sem-tls para testar sem criptografia")
	}

	if err := remote.SalvaConfig(cfg); err != nil {
		return fmt.Errorf("gravando configuração: %w", err)
	}
	caminho, _ := remote.CaminhoConfig()
	fmt.Printf("Modo cliente configurado (%s:%d). Salvo em %s\n", cfg.Host, cfg.Porta, caminho)

	// testa a conexão na hora, para o usuário saber se já está valendo.
	fmt.Print("Testando conexão com o servidor... ")
	conn, err := db.OpenCliente(cfg)
	if err != nil {
		fmt.Println("falhou.")
		fmt.Printf("  %v\n", err)
		fmt.Println("A configuração foi salva mesmo assim; ajuste o servidor e tente `prisma saldo`.")
		return nil
	}
	conn.Close()
	fmt.Println("ok! Agora `prisma` usa o banco do servidor.")
	return nil
}

func configLocal() error {
	existia, err := remote.RemoveConfig()
	if err != nil {
		return fmt.Errorf("removendo configuração: %w", err)
	}
	if existia {
		fmt.Println("Modo normal restaurado. O Prisma volta a usar o banco local desta máquina.")
	} else {
		fmt.Println("Já estava no modo normal (não havia configuração de cliente).")
	}
	caminhoBanco, _ := db.Path()
	fmt.Printf("Banco local: %s\n", caminhoBanco)
	return nil
}

func trunc12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
