package main

import (
	"unsafe"

	"github.com/ebitengine/purego"

	"prisma/internal/desktop"
)

// funções do GTK/GLib, já carregados pela biblioteca do WebView
var (
	gtkPresent     func(unsafe.Pointer)
	gtkSetIconName func(unsafe.Pointer, string)
)

func carrega(lib string) uintptr {
	h, err := purego.Dlopen(lib, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return 0
	}
	return h
}

// antesDaJanela dá ao processo o id do app: vira a classe da janela (X11) e o
// app_id (Wayland), que ligam a janela ao atalho e ao ícone do menu.
func antesDaJanela() {
	if glib := carrega("libglib-2.0.so.0"); glib != 0 {
		var setPrgname func(string)
		purego.RegisterLibFunc(&setPrgname, glib, "g_set_prgname")
		setPrgname(desktop.AppID)
	}
}

// depoisDaJanela põe o ícone do tema (instalado pelo prisma instalar-atalho).
func depoisDaJanela(win unsafe.Pointer) {
	gtk := carrega("libgtk-3.so.0")
	if gtk == 0 || win == nil {
		return
	}
	purego.RegisterLibFunc(&gtkPresent, gtk, "gtk_window_present")
	purego.RegisterLibFunc(&gtkSetIconName, gtk, "gtk_window_set_icon_name")
	gtkSetIconName(win, "prisma")
}

// focar traz a janela para a frente (roda na thread da interface).
func focar(win unsafe.Pointer) {
	if gtkPresent != nil && win != nil {
		gtkPresent(win)
	}
}
