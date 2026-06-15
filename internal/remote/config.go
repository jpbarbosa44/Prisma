package remote

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Modos de operação do Prisma.
const (
	ModoLocal    = "local"    // banco SQLite local (comportamento padrão)
	ModoServidor = "servidor" // banco local + daemon na rede
	ModoCliente  = "cliente"  // sem banco local; fala com um servidor
)

// PortaPadrao é a porta TCP padrão do daemon.
const PortaPadrao = 8456

// Config descreve o papel desta instância e como ela alcança a outra ponta.
// É carregada de variáveis de ambiente e de um arquivo de config simples
// (chave=valor); o ambiente tem prioridade.
type Config struct {
	Modo        string // local | servidor | cliente
	Host        string // cliente: endereço do servidor (ex.: 192.168.0.10)
	Porta       int    // servidor: porta de escuta; cliente: porta do servidor
	Token       string // segredo compartilhado
	TLS         bool   // criptografia na conexão (ligada por padrão)
	Fingerprint string // cliente: SHA-256 do certificado do servidor (pinning)
}

// baseURL monta a URL do servidor para o cliente, http ou https conforme o TLS.
func (c Config) baseURL() string {
	esquema := "http"
	if c.TLS {
		esquema = "https"
	}
	return fmt.Sprintf("%s://%s:%d", esquema, c.Host, c.Porta)
}

// Carrega lê a config efetiva. Sem nada configurado, o modo é local — quem não
// usa compartilhamento não percebe diferença. TLS vem ligado: dado financeiro
// não trafega em claro nem na rede de casa.
func Carrega() (Config, error) {
	cfg := Config{Modo: ModoLocal, Host: "127.0.0.1", Porta: PortaPadrao, TLS: true}

	// 1) arquivo de config (se existir)
	if p, err := caminhoConfig(); err == nil {
		if err := aplicaArquivo(&cfg, p); err != nil {
			return cfg, err
		}
	}

	// 2) ambiente sobrepõe o arquivo
	if v := os.Getenv("PRISMA_MODO"); v != "" {
		cfg.Modo = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("PRISMA_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("PRISMA_PORTA"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("PRISMA_PORTA inválida: %q", v)
		}
		cfg.Porta = n
	}
	if v := os.Getenv("PRISMA_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("PRISMA_TLS"); v != "" {
		cfg.TLS = ligado(v)
	}
	if v := os.Getenv("PRISMA_FINGERPRINT"); v != "" {
		cfg.Fingerprint = v
	}

	if err := cfg.valida(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) valida() error {
	switch c.Modo {
	case ModoLocal, ModoServidor, ModoCliente:
	default:
		return fmt.Errorf("modo desconhecido %q (use local, servidor ou cliente)", c.Modo)
	}
	if c.Modo == ModoCliente {
		if c.Host == "" {
			return fmt.Errorf("modo cliente exige host do servidor")
		}
		if c.Token == "" {
			return fmt.Errorf("modo cliente exige token (combine com o servidor)")
		}
		if c.TLS && c.Fingerprint == "" {
			return fmt.Errorf("modo cliente com TLS exige fingerprint do servidor " +
				"(o comando `prisma servidor` mostra; ou use tls=off para testar sem criptografia)")
		}
	}
	if c.Modo == ModoServidor && c.Token == "" {
		return fmt.Errorf("modo servidor exige token (o cliente precisa dele)")
	}
	return nil
}

// caminhoConfig devolve o arquivo de config por SO (mesma lógica de diretório
// do banco).
func caminhoConfig() (string, error) {
	if p := os.Getenv("PRISMA_CONFIG"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "prisma", "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "prisma", "config"), nil
}

// CaminhoConfig expõe o caminho do arquivo de configuração.
func CaminhoConfig() (string, error) { return caminhoConfig() }

// SalvaConfig grava a configuração no arquivo (criando o diretório). Escreve só
// as chaves relevantes ao modo, num arquivo limpo e comentado.
func SalvaConfig(cfg Config) error {
	caminho, err := caminhoConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Prisma — configuração de compartilhamento\n")
	b.WriteString("# Gerado por `prisma config`. Edite à mão se preferir.\n")
	fmt.Fprintf(&b, "modo=%s\n", cfg.Modo)
	if cfg.Modo == ModoCliente {
		fmt.Fprintf(&b, "host=%s\n", cfg.Host)
		if cfg.Porta != 0 && cfg.Porta != PortaPadrao {
			fmt.Fprintf(&b, "porta=%d\n", cfg.Porta)
		}
		fmt.Fprintf(&b, "token=%s\n", cfg.Token)
		if cfg.TLS {
			fmt.Fprintf(&b, "fingerprint=%s\n", cfg.Fingerprint)
		} else {
			b.WriteString("tls=off\n")
		}
	}
	return os.WriteFile(caminho, []byte(b.String()), 0o600)
}

// RemoveConfig apaga o arquivo de configuração, voltando ao modo local padrão.
// O bool indica se o arquivo existia.
func RemoveConfig() (bool, error) {
	caminho, err := caminhoConfig()
	if err != nil {
		return false, err
	}
	if err := os.Remove(caminho); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// aplicaArquivo lê um arquivo chave=valor (linhas em branco e # são ignoradas).
// Arquivo ausente não é erro.
func aplicaArquivo(cfg *Config, caminho string) error {
	f, err := os.Open(caminho)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		linha := strings.TrimSpace(sc.Text())
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		chave, valor, ok := strings.Cut(linha, "=")
		if !ok {
			continue
		}
		chave = strings.TrimSpace(strings.ToLower(chave))
		valor = strings.TrimSpace(valor)
		switch chave {
		case "modo":
			cfg.Modo = strings.ToLower(valor)
		case "host":
			cfg.Host = valor
		case "porta":
			n, err := strconv.Atoi(valor)
			if err != nil {
				return fmt.Errorf("porta inválida no config: %q", valor)
			}
			cfg.Porta = n
		case "token":
			cfg.Token = valor
		case "tls":
			cfg.TLS = ligado(valor)
		case "fingerprint":
			cfg.Fingerprint = valor
		}
	}
	return sc.Err()
}

// ligado interpreta valores booleanos amigáveis vindos de config/ambiente.
func ligado(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "sim", "true", "yes", "ligado":
		return true
	default:
		return false
	}
}
