// Package update verifica, sem incomodar, se há uma versão mais nova do
// Prisma publicada no GitHub, e sabe baixá-la com conferência de SHA256 e
// trocar o binário em execução pelo novo. A checagem roda no máximo uma vez
// por dia, em segundo plano, e falha em silêncio: sem internet ou sem release,
// nada aparece. É o estilo do oh-my-zsh — avisa na abertura, atualiza por
// comando explícito (prisma atualizar).
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"prisma/internal/db"
)

// Versao é preenchida no build via -ldflags "-X prisma/internal/update.Versao=...".
// Em build de desenvolvimento fica "dev", e aí nenhum aviso é mostrado.
var Versao = "dev"

const repo = "jpbarbosa44/Prisma"

// cache guarda a última checagem, ao lado do banco, para não bater na rede a
// cada abertura.
type cache struct {
	Data   string `json:"data"`          // AAAA-MM-DD da última checagem na rede
	Versao string `json:"versao"`        // versão mais nova vista no GitHub
	URL    string `json:"url,omitempty"` // página do release
}

func caminhoCache() (string, error) {
	p, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "update.json"), nil
}

func carrega() cache {
	var c cache
	p, err := caminhoCache()
	if err != nil {
		return c
	}
	dados, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	json.Unmarshal(dados, &c)
	return c
}

func salva(c cache) {
	p, err := caminhoCache()
	if err != nil {
		return
	}
	if dados, err := json.MarshalIndent(c, "", "  "); err == nil {
		os.WriteFile(p, dados, 0o644)
	}
}

// Aviso devolve uma linha de aviso (e a URL do release) se o cache aponta uma
// versão mais nova que a atual. Não toca na rede — é instantâneo e seguro para
// chamar a cada abertura.
func Aviso() (texto, url string) {
	c := carrega()
	if c.Versao == "" || !maisNova(c.Versao, Versao) {
		return "", ""
	}
	return fmt.Sprintf("Nova versão %s disponível (você está na %s). Rode: prisma atualizar",
		c.Versao, Versao), c.URL
}

// NovaDisponivel diz, a partir do cache da checagem diária, se há uma versão
// mais nova que a instalada e qual é. ok=false quando não há novidade (ou o
// build é "dev", que nunca recebe aviso). Não toca na rede.
func NovaDisponivel() (nova, atual string, ok bool) {
	c := carrega()
	if c.Versao == "" || !maisNova(c.Versao, Versao) {
		return "", Versao, false
	}
	return c.Versao, Versao, true
}

// OfereceAtualizar é o gancho de abertura: se a checagem diária já viu uma
// versão nova, PERGUNTA ao usuário (lendo de r, escrevendo em w) se quer
// atualizar agora e, com um "sim", baixa e instala (com conferência de SHA256).
// Devolve true se atualizou. É silenciosa quando não há novidade — pode ser
// chamada sempre na abertura, inclusive quando r/w não são um terminal.
func OfereceAtualizar(r io.Reader, w io.Writer) (bool, error) {
	nova, atual, ok := NovaDisponivel()
	if !ok {
		return false, nil
	}
	return oferece(r, w, nova, atual, Atualizar)
}

// oferece é o núcleo testável de OfereceAtualizar: faz a pergunta e só chama
// `atualizar` diante de um "sim" explícito (qualquer outra coisa, inclusive
// Enter vazio ou EOF, recusa — atualizar é uma ação que troca o binário).
func oferece(r io.Reader, w io.Writer, nova, atual string, atualizar func() error) (bool, error) {
	fmt.Fprintf(w, "Nova versão do Prisma disponível: %s (você está na %s).\n", nova, atual)
	fmt.Fprint(w, "Deseja atualizar agora? [s/N] ")
	resp := ""
	sc := bufio.NewScanner(r)
	if sc.Scan() {
		resp = strings.ToLower(strings.TrimSpace(sc.Text()))
	}
	switch resp {
	case "s", "sim", "y", "yes":
	default:
		fmt.Fprintln(w, "Sem problema — atualize quando quiser com: prisma atualizar")
		return false, nil
	}
	if err := atualizar(); err != nil {
		fmt.Fprintf(w, "erro ao atualizar: %v\n", err)
		return false, err
	}
	return true, nil
}

// AtualizaCache refaz a checagem na rede no máximo uma vez por dia e guarda o
// resultado. Pensada para rodar em segundo plano (go update.AtualizaCache()):
// não bloqueia e ignora qualquer erro — o aviso aparece na próxima abertura.
func AtualizaCache() {
	c := carrega()
	hoje := time.Now().Format("2006-01-02")
	if c.Data == hoje {
		return
	}
	tag, url, err := ultimaRelease(4 * time.Second)
	if err != nil {
		// trava a checagem por hoje, mas preserva o que já sabíamos
		salva(cache{Data: hoje, Versao: c.Versao, URL: c.URL})
		return
	}
	salva(cache{Data: hoje, Versao: tag, URL: url})
}

// Atualizar baixa a versão mais nova do GitHub, confere o SHA256 e troca o
// binário em execução. Imprime o progresso direto na saída.
func Atualizar() error {
	fmt.Println("Procurando atualizações no GitHub...")
	tag, _, err := ultimaRelease(10 * time.Second)
	if err != nil {
		return fmt.Errorf("não consegui consultar o GitHub: %w", err)
	}
	if !maisNova(tag, Versao) {
		fmt.Printf("Você já está na versão mais recente (%s).\n", Versao)
		salva(cache{Data: time.Now().Format("2006-01-02"), Versao: tag})
		return nil
	}
	arquivo, binInterno, err := nomeAsset()
	if err != nil {
		return err
	}
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s/", repo, tag)

	fmt.Printf("Baixando %s (%s)...\n", arquivo, tag)
	pacote, err := baixa(base+arquivo, 60*time.Second)
	if err != nil {
		return fmt.Errorf("baixando o pacote: %w", err)
	}
	somas, err := baixa(base+"SHA256SUMS", 20*time.Second)
	if err != nil {
		return fmt.Errorf("baixando SHA256SUMS: %w", err)
	}
	// confere o pacote compactado e, depois de extrair, o binário de dentro
	if err := confere(pacote, somas, arquivo); err != nil {
		return err
	}
	bin, err := extrai(pacote, arquivo, binInterno)
	if err != nil {
		return err
	}
	if err := confere(bin, somas, binInterno); err != nil {
		return err
	}
	if err := troca(bin); err != nil {
		return err
	}
	salva(cache{Data: time.Now().Format("2006-01-02"), Versao: tag})
	fmt.Printf("Atualizado de %s para %s. Reabra o prisma para usar a versão nova.\n", Versao, tag)
	return nil
}

// ultimaRelease consulta a API do GitHub e devolve a tag e a URL do release
// mais recente.
func ultimaRelease(timeout time.Duration) (tag, url string, err error) {
	cli := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := cli.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub respondeu %d", resp.StatusCode)
	}
	var r struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	if r.Tag == "" {
		return "", "", fmt.Errorf("release sem tag")
	}
	return r.Tag, r.URL, nil
}

// nomeAsset mapeia o SO/arquitetura atual no pacote publicado no release e no
// nome do binário que vem dentro dele. Linux e macOS usam .tar.gz; Windows .zip.
func nomeAsset() (arquivo, binInterno string, err error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "prisma-linux-amd64.tar.gz", "prisma-linux-amd64", nil
	case "darwin/arm64":
		return "prisma-mac-arm64.tar.gz", "prisma-mac-arm64", nil
	case "darwin/amd64":
		return "prisma-mac-intel.tar.gz", "prisma-mac-intel", nil
	case "windows/amd64":
		return "prisma-windows-amd64.zip", "prisma-windows-amd64.exe", nil
	case "windows/arm64":
		return "prisma-windows-arm64.zip", "prisma-windows-arm64.exe", nil
	}
	return "", "", fmt.Errorf("não há binário pronto para %s/%s; baixe manualmente em github.com/%s/releases",
		runtime.GOOS, runtime.GOARCH, repo)
}

// extrai recupera o binário de dentro do pacote baixado (.tar.gz ou .zip).
func extrai(pacote []byte, arquivo, binInterno string) ([]byte, error) {
	if strings.HasSuffix(arquivo, ".zip") {
		return extraiZip(pacote, binInterno)
	}
	return extraiTarGz(pacote, binInterno)
}

func extraiTarGz(pacote []byte, binInterno string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(pacote))
	if err != nil {
		return nil, fmt.Errorf("abrindo o .tar.gz: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == binInterno {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("não encontrei %s dentro do pacote", binInterno)
}

func extraiZip(pacote []byte, binInterno string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(pacote), int64(len(pacote)))
	if err != nil {
		return nil, fmt.Errorf("abrindo o .zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binInterno {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("não encontrei %s dentro do pacote", binInterno)
}

func baixa(url string, timeout time.Duration) ([]byte, error) {
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s respondeu %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// confere valida o SHA256 do binário baixado contra a linha do asset no
// arquivo SHA256SUMS. Sem isso, um download corrompido (ou adulterado) seria
// instalado no lugar do prisma.
func confere(bin, somas []byte, asset string) error {
	soma := sha256.Sum256(bin)
	esperado := hex.EncodeToString(soma[:])
	for _, linha := range strings.Split(string(somas), "\n") {
		campos := strings.Fields(linha)
		if len(campos) == 2 && campos[1] == asset {
			if campos[0] == esperado {
				return nil
			}
			return fmt.Errorf("SHA256 não confere para %s — download corrompido ou adulterado; abortei", asset)
		}
	}
	return fmt.Errorf("não encontrei %s no SHA256SUMS do release", asset)
}

// troca substitui o binário em execução pelo novo, escrevendo num arquivo
// temporário no mesmo diretório e renomeando por cima (atômico). No Linux e no
// macOS o rename funciona mesmo com o processo rodando; no Windows o binário em
// uso é movido para .old antes.
func troca(novo []byte) error {
	alvo, err := os.Executable()
	if err != nil {
		return err
	}
	if resolvido, err := filepath.EvalSymlinks(alvo); err == nil {
		alvo = resolvido
	}
	dir := filepath.Dir(alvo)
	tmp, err := os.CreateTemp(dir, ".prisma-novo-*")
	if err != nil {
		return fmt.Errorf("sem permissão de escrita em %s: %w", dir, err)
	}
	tmpNome := tmp.Name()
	defer os.Remove(tmpNome) // no-op se o rename abaixo der certo
	if _, err := tmp.Write(novo); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpNome, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		os.Rename(alvo, alvo+".old") // o .exe em uso não pode ser sobrescrito, mas pode ser movido
	}
	if err := os.Rename(tmpNome, alvo); err != nil {
		return fmt.Errorf("não consegui substituir %s: %w", alvo, err)
	}
	return nil
}

// maisNova diz se a versão candidata é maior que a atual (semver simples,
// X.Y.Z). Sufixos como -rc1 ou -5-gabcdef são ignorados; se qualquer lado não
// for parseável (ex.: build "dev"), devolve false — nada de aviso.
func maisNova(candidata, atual string) bool {
	c, ok1 := parseVersao(candidata)
	a, ok2 := parseVersao(atual)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if c[i] != a[i] {
			return c[i] > a[i]
		}
	}
	return false
}

func parseVersao(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i] // descarta sufixo de pré-release ou de git describe
	}
	partes := strings.Split(s, ".")
	if s == "" || len(partes) > 3 {
		return v, false
	}
	for i, p := range partes {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
