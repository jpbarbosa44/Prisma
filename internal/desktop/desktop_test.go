package desktop

import (
	"bytes"
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
