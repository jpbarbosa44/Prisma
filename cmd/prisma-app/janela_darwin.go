package main

import (
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"prisma/packaging"
)

func sel(nome string) objc.SEL { return objc.RegisterName(nome) }

func nsApp() objc.ID {
	return objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
}

func antesDaJanela() {}

// depoisDaJanela põe o ícone do Prisma no Dock: o Prisma.app só chama o
// prisma-app, que roda fora do pacote e não herdaria o .icns.
func depoisDaJanela(unsafe.Pointer) {
	png := packaging.IconePNG
	dados := objc.ID(objc.GetClass("NSData")).Send(sel("dataWithBytes:length:"), unsafe.Pointer(&png[0]), uint64(len(png)))
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc")).Send(sel("initWithData:"), dados)
	if img != 0 {
		nsApp().Send(sel("setApplicationIconImage:"), img)
	}
}

// focar traz a janela para a frente, tirando-a do Dock se estiver minimizada.
func focar(win unsafe.Pointer) {
	nsApp().Send(sel("activateIgnoringOtherApps:"), true)
	if win == nil {
		return
	}
	w := objc.ID(uintptr(win))
	w.Send(sel("deminiaturize:"), objc.ID(0))
	w.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
}
