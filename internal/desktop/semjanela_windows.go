package desktop

import (
	"os/exec"
	"syscall"
)

// createNoWindow é o CREATE_NO_WINDOW do CreateProcess.
const createNoWindow = 0x08000000

// SemJanela evita que um comando de console (powershell) abra uma janela
// preta por trás do prisma-app, que é compilado sem console (-H=windowsgui).
func SemJanela(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}
