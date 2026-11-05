package notifica

import (
	"os/exec"
	"strings"
)

func enviar(titulo, texto string) error {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	return exec.Command("osascript", "-e", `display notification "`+q(texto)+`" with title "`+q(titulo)+`"`).Run()
}
