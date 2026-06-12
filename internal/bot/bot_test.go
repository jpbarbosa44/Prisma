package bot

import (
	"strings"
	"testing"
)

func TestFatiar(t *testing.T) {
	linha := strings.Repeat("x", 50)
	texto := strings.Repeat(linha+"\n", 100) // 5100 bytes

	partes := fatiar(texto, 3800)
	if len(partes) != 2 {
		t.Fatalf("fatiar: tem %d partes, quer 2", len(partes))
	}
	total := 0
	for i, p := range partes {
		if len(p) > 3800 {
			t.Errorf("parte %d tem %d bytes, máximo é 3800", i, len(p))
		}
		// nenhuma linha pode ser partida no meio
		for _, l := range strings.Split(p, "\n") {
			if l != linha {
				t.Errorf("parte %d tem linha quebrada no meio: %q", i, l)
			}
		}
		total += strings.Count(p, "\n") + 1
	}
	if total != 100 {
		t.Errorf("fatiar perdeu linhas: tem %d, quer 100", total)
	}

	if partes := fatiar("curto\n", 3800); len(partes) != 1 || partes[0] != "curto" {
		t.Errorf("fatiar(curto): %q", partes)
	}
	if partes := fatiar("", 3800); len(partes) != 0 {
		t.Errorf("fatiar(vazio): %q", partes)
	}
}
