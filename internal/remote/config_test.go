package remote_test

import (
	"os"
	"path/filepath"
	"testing"

	"prisma/internal/remote"
)

// TestCarregaArquivoEAmbiente garante a precedência: o arquivo é a base, o
// ambiente sobrepõe.
func TestCarregaArquivoEAmbiente(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	conteudo := "modo=cliente\nhost=10.0.0.5\nporta=9000\ntoken=do-arquivo\ntls=off\n"
	if err := os.WriteFile(cfgPath, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRISMA_CONFIG", cfgPath)
	// limpa qualquer override herdado do ambiente do CI/dev
	for _, k := range []string{"PRISMA_MODO", "PRISMA_HOST", "PRISMA_PORTA", "PRISMA_TLS", "PRISMA_FINGERPRINT"} {
		t.Setenv(k, "")
	}
	t.Setenv("PRISMA_TOKEN", "do-ambiente") // sobrepõe o token do arquivo

	cfg, err := remote.Carrega()
	if err != nil {
		t.Fatalf("Carrega: %v", err)
	}
	if cfg.Modo != remote.ModoCliente || cfg.Host != "10.0.0.5" || cfg.Porta != 9000 {
		t.Errorf("arquivo não aplicado: %+v", cfg)
	}
	if cfg.Token != "do-ambiente" {
		t.Errorf("ambiente deveria sobrepor o token; veio %q", cfg.Token)
	}
	if cfg.TLS {
		t.Errorf("tls=off no arquivo deveria desligar o TLS")
	}
}

// TestValidaCliente cobre as exigências do modo cliente: host, token e (com TLS)
// fingerprint. São as travas que evitam um cliente conectar inseguro por engano.
func TestValidaCliente(t *testing.T) {
	t.Setenv("PRISMA_CONFIG", filepath.Join(t.TempDir(), "naoexiste"))
	for _, k := range []string{"PRISMA_HOST", "PRISMA_PORTA", "PRISMA_FINGERPRINT"} {
		t.Setenv(k, "")
	}

	casos := []struct {
		nome            string
		modo, token, fp string
		tls             bool
		querErro        bool
	}{
		{"cliente sem token", remote.ModoCliente, "", "", false, true},
		{"cliente tls sem fingerprint", remote.ModoCliente, "tok", "", true, true},
		{"cliente tls completo", remote.ModoCliente, "tok", "abc123", true, false},
		{"cliente sem tls com token", remote.ModoCliente, "tok", "", false, false},
		{"servidor sem token", remote.ModoServidor, "", "", true, true},
		{"modo inexistente", "fantasma", "tok", "", false, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			t.Setenv("PRISMA_MODO", c.modo)
			t.Setenv("PRISMA_HOST", "127.0.0.1")
			t.Setenv("PRISMA_TOKEN", c.token)
			t.Setenv("PRISMA_FINGERPRINT", c.fp)
			if c.tls {
				t.Setenv("PRISMA_TLS", "on")
			} else {
				t.Setenv("PRISMA_TLS", "off")
			}
			_, err := remote.Carrega()
			if (err != nil) != c.querErro {
				t.Errorf("Carrega erro=%v; quero erro=%v", err, c.querErro)
			}
		})
	}
}
