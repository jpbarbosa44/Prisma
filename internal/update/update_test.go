package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

// TestOferece cobre o núcleo da oferta de atualização na abertura: só um "sim"
// explícito dispara a instalação; qualquer outra resposta (ou Enter/EOF) recusa.
func TestOferece(t *testing.T) {
	casos := []struct {
		resp         string
		querAtualiza bool
	}{
		{"s\n", true},
		{"sim\n", true},
		{"y\n", true},
		{"  S  \n", true}, // espaços e maiúscula
		{"n\n", false},
		{"\n", false}, // Enter vazio = recusa
		{"", false},   // EOF = recusa
		{"talvez\n", false},
	}
	for _, c := range casos {
		chamou := false
		var out strings.Builder
		atualizou, err := oferece(strings.NewReader(c.resp), &out, "v2.0.0", "v1.0.0",
			func() error { chamou = true; return nil })
		if err != nil {
			t.Errorf("resp %q: erro inesperado: %v", c.resp, err)
		}
		if chamou != c.querAtualiza || atualizou != c.querAtualiza {
			t.Errorf("resp %q: chamou=%v atualizou=%v; quero %v", c.resp, chamou, atualizou, c.querAtualiza)
		}
		if !strings.Contains(out.String(), "v2.0.0") {
			t.Errorf("resp %q: a oferta deveria citar a versão nova; veio %q", c.resp, out.String())
		}
	}
}

// TestOfereceErroNaInstalacao garante que um erro ao instalar é propagado e não
// conta como atualização concluída.
func TestOfereceErroNaInstalacao(t *testing.T) {
	var out strings.Builder
	atualizou, err := oferece(strings.NewReader("s\n"), &out, "v2.0.0", "v1.0.0",
		func() error { return errFalso })
	if err == nil || atualizou {
		t.Fatalf("esperava erro e atualizou=false; veio atualizou=%v err=%v", atualizou, err)
	}
}

type erroFalso struct{}

func (erroFalso) Error() string { return "falha de rede simulada" }

var errFalso = erroFalso{}

func TestMaisNova(t *testing.T) {
	casos := []struct {
		candidata, atual string
		quer             bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0-3-gabcdef", false}, // mesma base, sufixo de git describe
		{"v0.2.0", "v0.1.0-3-gabcdef", true},
		{"0.2.0", "0.1.0", true},    // sem o "v"
		{"v0.2.0", "dev", false},    // build de dev não recebe aviso
		{"banana", "v0.1.0", false}, // lixo não vira aviso
	}
	for _, c := range casos {
		if got := maisNova(c.candidata, c.atual); got != c.quer {
			t.Errorf("maisNova(%q, %q) = %v, quer %v", c.candidata, c.atual, got, c.quer)
		}
	}
}

// fazTarGz monta na memória um .tar.gz com os arquivos dados (nome→conteúdo).
func fazTarGz(t *testing.T, arquivos map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for nome, conteudo := range arquivos {
		if err := tw.WriteHeader(&tar.Header{Name: nome, Mode: 0o755, Size: int64(len(conteudo))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(conteudo); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fazZip monta na memória um .zip com os arquivos dados.
func fazZip(t *testing.T, arquivos map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for nome, conteudo := range arquivos {
		w, err := zw.Create(nome)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(conteudo); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtrai garante que o binário certo é recuperado de dentro do pacote, em
// ambos os formatos, mesmo quando vem aninhado num diretório, e que um pacote
// sem o binário esperado vira erro (não um binário vazio instalado por engano).
func TestExtrai(t *testing.T) {
	bin := []byte("\x7fELF binário falso do prisma")

	casos := []struct {
		nome       string
		arquivo    string // dita o formato em extrai (.zip vs .tar.gz)
		pacote     []byte
		binInterno string
		querErro   bool
	}{
		{
			nome:       "tar.gz na raiz",
			arquivo:    "prisma-linux-amd64.tar.gz",
			pacote:     fazTarGz(t, map[string][]byte{"prisma-linux-amd64": bin, "LEIAME.txt": []byte("oi")}),
			binInterno: "prisma-linux-amd64",
		},
		{
			nome:       "tar.gz aninhado em diretório",
			arquivo:    "prisma-linux-amd64.tar.gz",
			pacote:     fazTarGz(t, map[string][]byte{"dist/prisma-linux-amd64": bin}),
			binInterno: "prisma-linux-amd64",
		},
		{
			nome:       "zip do windows",
			arquivo:    "prisma-windows-amd64.zip",
			pacote:     fazZip(t, map[string][]byte{"prisma-windows-amd64.exe": bin}),
			binInterno: "prisma-windows-amd64.exe",
		},
		{
			nome:       "binário ausente no tar.gz",
			arquivo:    "prisma-linux-amd64.tar.gz",
			pacote:     fazTarGz(t, map[string][]byte{"outra-coisa": bin}),
			binInterno: "prisma-linux-amd64",
			querErro:   true,
		},
		{
			nome:       "pacote corrompido",
			arquivo:    "prisma-linux-amd64.tar.gz",
			pacote:     []byte("isto não é um gzip"),
			binInterno: "prisma-linux-amd64",
			querErro:   true,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := extrai(c.pacote, c.arquivo, c.binInterno)
			if c.querErro {
				if err == nil {
					t.Fatalf("esperava erro, veio %d bytes", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("extrai: %v", err)
			}
			if !bytes.Equal(got, bin) {
				t.Errorf("conteúdo extraído difere do original (%d vs %d bytes)", len(got), len(bin))
			}
		})
	}
}

// TestCacheDecisao cobre o caminho real da decisão de abertura: o que a checagem
// diária grava no cache é o que NovaDisponivel/Aviso leem para decidir se há
// novidade. Aponta o cache para um diretório temporário via PRISMA_DB.
func TestCacheDecisao(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "p.db"))

	// sem cache gravado: nada a oferecer
	if _, _, ok := NovaDisponivel(); ok {
		t.Error("sem cache não deveria haver versão nova")
	}

	// a versão atual do binário de teste é "dev"; uma release qualquer é mais nova
	salva(cache{Data: "2026-06-21", Versao: "v9.9.9", URL: "https://exemplo/rel"})

	// "dev" nunca recebe aviso (maisNova devolve false p/ versão não-parseável)
	if _, _, ok := NovaDisponivel(); ok {
		t.Error("build dev não deveria receber aviso")
	}

	// simula um binário versionado para exercitar a comparação de verdade
	old := Versao
	Versao = "v1.0.0"
	defer func() { Versao = old }()

	nova, atual, ok := NovaDisponivel()
	if !ok || nova != "v9.9.9" || atual != "v1.0.0" {
		t.Fatalf("NovaDisponivel = (%q, %q, %v); quero (v9.9.9, v1.0.0, true)", nova, atual, ok)
	}
	texto, url := Aviso()
	if !strings.Contains(texto, "v9.9.9") || url != "https://exemplo/rel" {
		t.Errorf("Aviso = (%q, %q); deveria citar a versão nova e a URL", texto, url)
	}

	// cache que aponta versão igual/anterior: sem aviso
	salva(cache{Data: "2026-06-21", Versao: "v1.0.0"})
	if _, _, ok := NovaDisponivel(); ok {
		t.Error("versão igual à instalada não é novidade")
	}
}

func TestConfereSHA256(t *testing.T) {
	bin := []byte("conteudo do binario")
	soma := sha256.Sum256(bin)
	somasOK := hex.EncodeToString(soma[:]) + "  prisma-linux-amd64\n"

	if err := confere(bin, []byte(somasOK), "prisma-linux-amd64"); err != nil {
		t.Errorf("confere com soma correta deveria passar: %v", err)
	}
	somasRuim := "0000000000000000000000000000000000000000000000000000000000000000  prisma-linux-amd64\n"
	if err := confere(bin, []byte(somasRuim), "prisma-linux-amd64"); err == nil {
		t.Error("confere com soma errada deveria falhar")
	}
	if err := confere(bin, []byte(somasOK), "prisma-mac-arm64"); err == nil {
		t.Error("confere sem o asset no SHA256SUMS deveria falhar")
	}
}
