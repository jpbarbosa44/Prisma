package remote

import (
	"crypto/tls"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestNormalizaFP garante que o fingerprint colado em qualquer formato (com ':',
// '-', espaços, maiúsculas) vira a mesma forma canônica usada na comparação —
// senão um pin legítimo seria rejeitado por causa da pontuação.
func TestNormalizaFP(t *testing.T) {
	canon := "ab12cd34ef56"
	for _, in := range []string{
		"AB:12:CD:34:EF:56",
		"ab-12-cd-34-ef-56",
		"  Ab12 Cd34 Ef56  ",
		"ab12cd34ef56",
	} {
		if got := normalizaFP(in); got != canon {
			t.Errorf("normalizaFP(%q) = %q; quero %q", in, got, canon)
		}
	}
}

// TestCertPersistente cobre o ciclo de vida do certificado autoassinado: o
// fingerprint precisa ser um SHA-256 hex válido, ser estável entre reinícios
// (senão o pin do cliente quebraria a cada restart) e a chave privada tem que
// ficar com modo 0600.
func TestCertPersistente(t *testing.T) {
	t.Setenv("HOME", t.TempDir())            // dirDados no Linux deriva de $HOME
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // cobre o caminho darwin/windows

	_, fp1, err := CarregaOuGeraCert()
	if err != nil {
		t.Fatalf("primeira geração: %v", err)
	}
	if len(fp1) != 64 {
		t.Errorf("fingerprint %q tem %d chars; quero 64", fp1, len(fp1))
	}
	if _, err := hex.DecodeString(fp1); err != nil {
		t.Errorf("fingerprint não é hex: %v", err)
	}

	_, fp2, err := CarregaOuGeraCert()
	if err != nil {
		t.Fatalf("segunda leitura: %v", err)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprint mudou entre chamadas (%s → %s); o pin do cliente quebraria", fp1, fp2)
	}

	_, keyPath, err := caminhoCert()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("chave não foi persistida: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("chave privada com modo %o; quero 600 (segredo não pode vazar para o grupo)", perm)
	}
}

// TestPinningHandshake é o teste de ponta a ponta da segurança do transporte:
// um cliente com o fingerprint certo completa o handshake TLS, e um com o
// fingerprint errado é recusado — mesmo o certificado sendo autoassinado.
func TestPinningHandshake(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cert, fp, err := CarregaOuGeraCert()
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	get := func(fingerprint string) error {
		cli := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfigCliente(fingerprint)}}
		resp, err := cli.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}

	if err := get(fp); err != nil {
		t.Errorf("fingerprint certo deveria conectar: %v", err)
	}

	// inverte os dois primeiros nibbles para garantir um pin diferente do real
	errado := "ffff" + fp[4:]
	if errado == fp {
		errado = "0000" + fp[4:]
	}
	if err := get(errado); err == nil {
		t.Error("fingerprint errado deveria recusar a conexão (man-in-the-middle)")
	}
}
