package seguranca

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPIN(t *testing.T) {
	t.Setenv("PRISMA_DB", filepath.Join(t.TempDir(), "p.db"))
	c, err := Carrega()
	if err != nil || c.TemPIN() {
		t.Fatalf("sem arquivo: %+v %v", c, err)
	}
	for _, ruim := range []string{"123", " 1234", "1234 ", ""} {
		if DefinirPIN(ruim) == nil {
			t.Errorf("PIN %q deveria ser recusado", ruim)
		}
	}
	if err := DefinirPIN("4821"); err != nil {
		t.Fatal(err)
	}
	c, _ = Carrega()
	if !c.TemPIN() || !c.Confere("4821") || c.Confere("4822") || c.Confere("") {
		t.Fatalf("conferência errada: %+v", c)
	}
	p, _ := Caminho()
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v, quero 0600", st.Mode().Perm())
	}
	c.Rede = true
	Salva(c)
	if err := RemoverPIN(); err != nil {
		t.Fatal(err)
	}
	c, _ = Carrega()
	if c.TemPIN() || c.Rede {
		t.Errorf("remover deveria tirar o PIN e a rede: %+v", c)
	}
}

func TestSessoes(t *testing.T) {
	agora := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	s := NovasSessoes()
	s.agora = func() time.Time { return agora }
	tok := s.Nova()
	if len(tok) != 64 || !s.Valida(tok, time.Minute*15) || s.Valida("x", time.Minute) || s.Valida("", time.Minute) {
		t.Fatal("sessão nova deveria valer, as outras não")
	}
	agora = agora.Add(10 * time.Minute)
	if !s.Valida(tok, 15*time.Minute) {
		t.Fatal("10 min parado ainda vale")
	}
	agora = agora.Add(14 * time.Minute) // renovada aos 10: 14 depois ainda vale
	if !s.Valida(tok, 15*time.Minute) {
		t.Fatal("o uso deveria renovar o prazo")
	}
	agora = agora.Add(16 * time.Minute)
	if s.Valida(tok, 15*time.Minute) {
		t.Fatal("16 min parado deveria trancar")
	}
	t2 := s.Nova()
	s.Encerra(t2)
	if s.Valida(t2, time.Hour) {
		t.Fatal("encerrada não vale")
	}
}

func TestTentativas(t *testing.T) {
	agora := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	tt := NovasTentativas()
	tt.agora = func() time.Time { return agora }
	for i := 0; i < 4; i++ {
		if d := tt.Falhou("1.2.3.4"); d != 0 {
			t.Fatalf("falha %d já bloqueou", i+1)
		}
	}
	if d := tt.Falhou("1.2.3.4"); d != 30*time.Second {
		t.Fatalf("5ª falha: %v", d)
	}
	if tt.Espera("1.2.3.4") != 30*time.Second || tt.Espera("5.6.7.8") != 0 {
		t.Fatal("espera por endereço")
	}
	agora = agora.Add(31 * time.Second)
	if tt.Espera("1.2.3.4") != 0 {
		t.Fatal("passou a espera")
	}
	if d := tt.Falhou("1.2.3.4"); d != time.Minute {
		t.Fatalf("6ª falha dobra: %v", d)
	}
	tt.Acertou("1.2.3.4")
	if tt.Espera("1.2.3.4") != 0 || tt.Falhou("1.2.3.4") != 0 {
		t.Fatal("acertar zera")
	}
}
