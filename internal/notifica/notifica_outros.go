//go:build !linux && !windows && !darwin

package notifica

import "errors"

func enviar(string, string) error { return errors.New("notificações não suportadas neste sistema") }
