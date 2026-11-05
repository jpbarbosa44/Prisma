// Package notifica mostra notificações do sistema (o balão no canto da tela)
// sem CGO: notify-send/gdbus no Linux, toast via PowerShell no Windows e
// osascript no macOS. É o lembrete de contas do app desktop.
package notifica

// Enviar mostra uma notificação com título e texto.
func Enviar(titulo, texto string) error { return enviar(titulo, texto) }
