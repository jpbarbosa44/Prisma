package update

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

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
