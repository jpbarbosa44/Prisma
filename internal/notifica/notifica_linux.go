package notifica

import (
	"fmt"
	"os/exec"
	"strconv"
)

func enviar(titulo, texto string) error {
	if _, err := exec.LookPath("notify-send"); err == nil {
		return exec.Command("notify-send", "-a", "Prisma", "-i", "prisma", titulo, texto).Run()
	}
	// sem libnotify-bin: fala direto com o serviço de notificações (D-Bus)
	if _, err := exec.LookPath("gdbus"); err != nil {
		return fmt.Errorf("nem notify-send nem gdbus disponíveis")
	}
	return exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		"Prisma", "0", "prisma", strconv.Quote(titulo), strconv.Quote(texto), "[]", "{}", "10000").Run()
}
