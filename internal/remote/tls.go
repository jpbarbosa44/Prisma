package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// O Prisma usa certificado autoassinado com pinning de fingerprint: não há
// autoridade certificadora nem nome de host para validar — o cliente confia
// apenas no certificado cujo SHA-256 bate com o que ele tem fixado. Isso dá
// sigilo e proteção contra interceptação numa LAN sem a burocracia de uma CA.

// CarregaOuGeraCert devolve o certificado do servidor (gerando e salvando um
// autoassinado na primeira vez) e o seu fingerprint SHA-256 em hexadecimal.
// Persistir o par mantém o fingerprint estável entre reinícios, então o pin do
// cliente continua valendo.
func CarregaOuGeraCert() (tls.Certificate, string, error) {
	certPath, keyPath, err := caminhoCert()
	if err != nil {
		return tls.Certificate{}, "", err
	}

	certPEM, errC := os.ReadFile(certPath)
	keyPEM, errK := os.ReadFile(keyPath)
	if errC != nil || errK != nil {
		certPEM, keyPEM, err = geraCertPEM()
		if err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return tls.Certificate{}, "", err
		}
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("lendo certificado: %w", err)
	}
	return cert, Fingerprint(cert.Certificate[0]), nil
}

// geraCertPEM cria um certificado ECDSA autoassinado válido por 10 anos.
func geraCertPEM() (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "prisma-servidor"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// Fingerprint devolve o SHA-256 do certificado (DER) em hexadecimal minúsculo.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// normalizaFP tira separadores comuns (':', '-', espaços) e baixa a caixa, para
// o usuário poder colar o fingerprint em qualquer formato.
func normalizaFP(s string) string {
	r := strings.NewReplacer(":", "", "-", "", " ", "")
	return strings.ToLower(r.Replace(strings.TrimSpace(s)))
}

// tlsConfigCliente monta o tls.Config do cliente que valida o servidor apenas
// pelo fingerprint fixado (e ignora CA/host, que não se aplicam aqui).
func tlsConfigCliente(fingerprint string) *tls.Config {
	alvo := normalizaFP(fingerprint)
	return &tls.Config{
		// Verificação padrão (CA + host) é substituída pela checagem manual
		// abaixo; sem InsecureSkipVerify o handshake falharia antes dela.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("servidor não apresentou certificado")
			}
			veio := Fingerprint(rawCerts[0])
			if subtle.ConstantTimeCompare([]byte(veio), []byte(alvo)) != 1 {
				return fmt.Errorf("fingerprint do servidor não confere (esperado %s…, veio %s…)",
					trunc(alvo), trunc(veio))
			}
			return nil
		},
	}
}

func trunc(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// caminhoCert devolve os caminhos do certificado e da chave do servidor, ao
// lado dos dados do Prisma.
func caminhoCert() (cert, key string, err error) {
	dir, err := dirDados()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "servidor-cert.pem"), filepath.Join(dir, "servidor-key.pem"), nil
}

// dirDados espelha o diretório de dados usado pelo banco (sem importar o pacote
// db, para não criar ciclo de importação).
func dirDados() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "prisma"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "prisma"), nil
}
