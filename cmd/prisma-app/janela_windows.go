package main

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"

	"prisma/packaging"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procIsIconic        = user32.NewProc("IsIconic")
	procShowWindow      = user32.NewProc("ShowWindow")
	procSetForeground   = user32.NewProc("SetForegroundWindow")
	procLoadImage       = user32.NewProc("LoadImageW")
	procSendMessage     = user32.NewProc("SendMessageW")
	procGetSystemMetric = user32.NewProc("GetSystemMetrics")
)

const (
	swRestore      = 9
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	imageIcon      = 1
	lrLoadFromFile = 0x0010
	smCxIcon       = 11
	smCxSmIcon     = 49
)

func antesDaJanela() {}

// depoisDaJanela põe o ícone do Prisma na janela e na barra de tarefas: grava
// o .ico embutido numa pasta temporária e o carrega nos dois tamanhos.
func depoisDaJanela(win unsafe.Pointer) {
	if win == nil {
		return
	}
	ico := filepath.Join(os.TempDir(), "prisma-app.ico")
	if err := os.WriteFile(ico, packaging.IconeICO, 0o644); err != nil {
		return
	}
	nome, err := windows.UTF16PtrFromString(ico)
	if err != nil {
		return
	}
	for _, t := range []struct{ tipo, metrica uintptr }{{iconBig, smCxIcon}, {iconSmall, smCxSmIcon}} {
		lado, _, _ := procGetSystemMetric.Call(t.metrica)
		h, _, _ := procLoadImage.Call(0, uintptr(unsafe.Pointer(nome)), imageIcon, lado, lado, lrLoadFromFile)
		if h != 0 {
			procSendMessage.Call(uintptr(win), wmSetIcon, t.tipo, h)
		}
	}
}

// focar traz a janela para a frente, restaurando-a se estiver minimizada.
func focar(win unsafe.Pointer) {
	if win == nil {
		return
	}
	if min, _, _ := procIsIconic.Call(uintptr(win)); min != 0 {
		procShowWindow.Call(uintptr(win), swRestore)
	}
	procSetForeground.Call(uintptr(win))
}
