package bot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// O bot fala com o Telegram por long polling: ele abre uma conexão DE SAÍDA
// para api.telegram.org e o Telegram entrega as mensagens por ali. Ou seja, o
// bot é acessível de qualquer rede — não precisa de IP fixo, porta aberta nem
// estar na mesma rede do computador. O único requisito é que o processo esteja
// rodando. Por isso "só funciona em casa" quase sempre significa que o
// `prisma bot` só fica de pé enquanto o terminal está aberto. Este serviço
// systemd resolve isso: mantém o bot rodando sempre que o computador estiver
// ligado, inclusive sem ninguém logado e após reiniciar.

const servicoNome = "prisma-bot.service"

// instalarServico escreve e ativa o serviço systemd de usuário.
func instalarServico() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("instalação automática do serviço só no Linux (systemd).\n" +
			"No macOS use um launchd agent; no Windows, o Agendador de Tarefas para rodar `prisma bot` na inicialização")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// fixa o banco que está em uso, para o serviço não depender do ambiente
	dbLinha := ""
	if p := os.Getenv("PRISMA_DB"); p != "" {
		dbLinha = "Environment=PRISMA_DB=" + p + "\n"
	}
	unit := fmt.Sprintf(`[Unit]
Description=Prisma — bot de Telegram
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=PRISMA_BOT_SERVICE=1
ExecStart=%s bot
Restart=always
RestartSec=10
%s
[Install]
WantedBy=default.target
`, exe, dbLinha)
	caminho := filepath.Join(dir, servicoNome)
	if err := os.WriteFile(caminho, []byte(unit), 0o644); err != nil {
		return err
	}

	// linger mantém os serviços do usuário no ar mesmo sem sessão aberta
	if out, err := exec.Command("loginctl", "enable-linger").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não consegui habilitar o linger automaticamente (%v).\n", err)
		fmt.Fprintf(os.Stderr, "       sem ele o bot só roda enquanto você está logado. Rode:\n")
		fmt.Fprintf(os.Stderr, "       sudo loginctl enable-linger %s\n", os.Getenv("USER"))
		_ = out
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", servicoNome},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %v falhou: %v\n%s", args, err, out)
		}
	}

	fmt.Printf("Serviço instalado e ativo: %s\n\n", caminho)
	fmt.Println("O bot agora sobe sozinho com o computador e responde de qualquer rede,")
	fmt.Println("enquanto o PC estiver ligado e com internet.")
	fmt.Println()
	fmt.Println("  estado:    systemctl --user status prisma-bot")
	fmt.Println("  logs:      journalctl --user -u prisma-bot -f")
	fmt.Println("  desligar:  prisma bot --remover-servico")
	return nil
}

// servicoAtivo diz se o serviço systemd do bot está rodando agora.
func servicoAtivo() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return exec.Command("systemctl", "--user", "is-active", "--quiet", servicoNome).Run() == nil
}

// reiniciarServico reinicia o serviço para ele recarregar a configuração nova.
func reiniciarServico() error {
	return exec.Command("systemctl", "--user", "restart", servicoNome).Run()
}

// removerServico desativa e apaga o serviço.
func removerServico() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("serviço automático só no Linux (systemd)")
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", servicoNome).Run()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	caminho := filepath.Join(home, ".config", "systemd", "user", servicoNome)
	if err := os.Remove(caminho); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	fmt.Println("Serviço do bot removido. (O `prisma bot` no terminal continua funcionando.)")
	return nil
}
