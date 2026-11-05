package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"prisma/internal/update"
	"prisma/packaging"
)

func TestEntradaDesktop(t *testing.T) {
	e := entradaDesktop("Prisma", "x", `/opt/meu "app"/prisma-app`, false)
	if !strings.Contains(e, "Exec=\"/opt/meu \\\"app\\\"/prisma-app\"\n") {
		t.Errorf("Exec com aspas mal escapado:\n%s", e)
	}
	if !strings.Contains(e, "Terminal=false") || !strings.Contains(e, "Icon=prisma") {
		t.Errorf("faltou Terminal=false ou Icon=prisma:\n%s", e)
	}
	if !strings.Contains(e, "StartupWMClass="+AppID+"\n") {
		t.Errorf("a janela do app devia ter StartupWMClass:\n%s", e)
	}
	if strings.Contains(entradaDesktop("Prisma", "x", "/p", true), "StartupWMClass") {
		t.Error("o atalho do terminal não devia ter StartupWMClass")
	}
}

func TestIconesEmbutidos(t *testing.T) {
	if !bytes.HasPrefix(packaging.IconePNG, []byte("\x89PNG")) {
		t.Error("prisma.png não é PNG")
	}
	if !bytes.HasPrefix(packaging.IconeICO, []byte{0, 0, 1, 0}) {
		t.Error("prisma.ico não é ICO")
	}
	if !bytes.HasPrefix(packaging.IconeICNS, []byte("icns")) {
		t.Error("prisma.icns não é ICNS")
	}
	if !bytes.Contains(packaging.IconeSVG, []byte("<svg")) {
		t.Error("prisma.svg não é SVG")
	}
}

func TestInfoPlist(t *testing.T) {
	antes := update.Versao
	t.Cleanup(func() { update.Versao = antes })
	update.Versao = "v1.4.0"
	if p := infoPlist(); !strings.Contains(p, "<string>1.4.0</string>") {
		t.Errorf("a versão devia ir sem o v no plist:\n%s", p)
	}
}

// TestInstalarAtalhoDoApp: chamado de dentro do prisma-app (primeira abertura),
// o atalho da janela aponta para o app e o "(terminal)" para o prisma ao lado.
func TestInstalarAtalhoDoApp(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("atalhos .desktop são do Linux")
	}
	dados, bin := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", dados)
	app := filepath.Join(bin, "prisma-app")
	os.WriteFile(app, []byte("x"), 0o755)

	// sem o prisma ao lado: só o atalho do app
	if err := InstalarAtalhoDoApp(app, io.Discard); err != nil {
		t.Fatal(err)
	}
	janela, err := os.ReadFile(filepath.Join(dados, "applications", AppID+".desktop"))
	if err != nil || !strings.Contains(string(janela), `Exec="`+app+`"`) {
		t.Fatalf("atalho do app: %v\n%s", err, janela)
	}
	if _, err := os.Stat(filepath.Join(dados, "applications", "prisma.desktop")); err == nil {
		t.Fatal("sem o prisma ao lado, não devia criar o atalho de terminal")
	}

	// com o prisma ao lado: também o "(terminal)", apontando para ele
	os.WriteFile(filepath.Join(bin, "prisma"), []byte("x"), 0o755)
	if err := InstalarAtalhoDoApp(app, io.Discard); err != nil {
		t.Fatal(err)
	}
	term, err := os.ReadFile(filepath.Join(dados, "applications", "prisma.desktop"))
	if err != nil || !strings.Contains(string(term), `Exec="`+filepath.Join(bin, "prisma")+`"`) {
		t.Fatalf("atalho do terminal: %v\n%s", err, term)
	}
}
