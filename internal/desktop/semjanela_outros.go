//go:build !windows

package desktop

import "os/exec"

// SemJanela só tem efeito no Windows (lá, esconde a janela de console).
func SemJanela(cmd *exec.Cmd) *exec.Cmd { return cmd }
