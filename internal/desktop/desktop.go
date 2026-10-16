// Package desktop integra o Prisma à área de trabalho: `prisma app` abre a
// janela (o binário prisma-app, ao lado deste) e `prisma instalar-atalho` põe
// o Prisma no menu de aplicativos do sistema, com ícone.
package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"prisma/internal/update"
	"prisma/packaging"
)

// Uso resume os comandos do app desktop para a ajuda.
const Uso = `APP DESKTOP
  prisma app                abre o Prisma numa janela própria (precisa do prisma-app)
  prisma instalar-atalho    põe o Prisma no menu de aplicativos, com ícone
  prisma remover-atalho     tira o atalho do menu`

// AppID é o id do app: nome do .desktop no Linux e classe da janela (o
// Wayland liga a janela ao atalho, e ao ícone, por ele).
const AppID = "io.github.jpbarbosa44.Prisma"

const semApp = `o prisma-app não está instalado ao lado do prisma.
Ele vem nos pacotes do release (https://github.com/jpbarbosa44/Prisma/releases/latest);
coloque-o na mesma pasta do prisma. Enquanto isso, prisma --web abre no navegador`

// esteBinario é o executável em uso, com links resolvidos.
func esteBinario() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("não achei o executável em uso: %w", err)
	}
	if resolvido, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolvido
	}
	return exe, nil
}

// BinarioApp acha o prisma-app instalado ao lado deste executável.
func BinarioApp() (string, bool) { return update.AppAoLado() }

// Abrir trata `prisma app`: lança a janela e volta para o terminal.
func Abrir(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(Uso)
			return nil
		}
	}
	app, ok := BinarioApp()
	if !ok {
		return errors.New(semApp)
	}
	if err := exec.Command(app, args...).Start(); err != nil {
		return fmt.Errorf("abrindo %s: %w", app, err)
	}
	return nil
}

// dadosUsuario é a pasta de dados do usuário: ~/.local/share (ou
// $XDG_DATA_HOME) no Linux, %APPDATA% no Windows.
func dadosUsuario() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	casa, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("não achei a pasta do usuário: %w", err)
	}
	return filepath.Join(casa, ".local", "share"), nil
}

func grava(caminho string, conteudo []byte, modo os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return fmt.Errorf("criando %s: %w", filepath.Dir(caminho), err)
	}
	if err := os.WriteFile(caminho, conteudo, modo); err != nil {
		return fmt.Errorf("gravando %s: %w", caminho, err)
	}
	return nil
}

// entradaDesktop monta o arquivo .desktop (padrão freedesktop) de um atalho.
func entradaDesktop(nome, comentario, exe string, terminal bool) string {
	// a janela do app se identifica pelo AppID; a do terminal é do emulador
	classe := ""
	if !terminal {
		classe = "StartupWMClass=" + AppID + "\n"
	}
	// o Exec do freedesktop usa aspas duplas, com \ e " escapados
	exe = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(exe)
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
GenericName=Finanças pessoais
Comment=%s
Exec="%s"
Icon=prisma
Terminal=%t
Categories=Office;Finance;
Keywords=finanças;dinheiro;contas;cartão;orçamento;finance;money;
%s`, nome, comentario, exe, terminal, classe)
}

// InstalarAtalho trata `prisma instalar-atalho`.
func InstalarAtalho() error {
	prisma, err := esteBinario()
	if err != nil {
		return err
	}
	app, temApp := BinarioApp()
	switch runtime.GOOS {
	case "darwin":
		if !temApp {
			return errors.New(semApp)
		}
		return instalarMac(app)
	case "windows":
		if !temApp {
			return errors.New(semApp)
		}
		return instalarWindows(app)
	}
	dados, err := dadosUsuario()
	if err != nil {
		return err
	}
	aplicativos := filepath.Join(dados, "applications")
	icones := filepath.Join(dados, "icons", "hicolor")
	if err := grava(filepath.Join(icones, "scalable", "apps", "prisma.svg"), packaging.IconeSVG, 0o644); err != nil {
		return err
	}
	if err := grava(filepath.Join(icones, "256x256", "apps", "prisma.png"), packaging.IconePNG, 0o644); err != nil {
		return err
	}
	terminal := entradaDesktop("Prisma (terminal)", "O Prisma no terminal", prisma, true)
	if err := grava(filepath.Join(aplicativos, "prisma.desktop"), []byte(terminal), 0o644); err != nil {
		return err
	}
	if temApp {
		janela := entradaDesktop("Prisma", "Suas finanças: contas, cartões, faturas e previsões", app, false)
		if err := grava(filepath.Join(aplicativos, AppID+".desktop"), []byte(janela), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Printf("aviso: %s\nSó o atalho do terminal foi criado.\n\n", semApp)
	}
	// atualizam o menu na hora; se não existirem, o menu se atualiza sozinho depois
	exec.Command("update-desktop-database", aplicativos).Run()
	exec.Command("gtk-update-icon-cache", "-f", "-t", icones).Run()
	fmt.Printf("✔ Prisma no menu de aplicativos (%s).\n", aplicativos)
	return nil
}

// RemoverAtalho trata `prisma remover-atalho`.
func RemoverAtalho() error {
	var alvos []string
	switch runtime.GOOS {
	case "darwin":
		casa, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		alvos = append(alvos, filepath.Join(casa, "Applications", "Prisma.app"))
	case "windows":
		menu, err := menuIniciar()
		if err != nil {
			return err
		}
		dados, err := dadosUsuario()
		if err != nil {
			return err
		}
		alvos = append(alvos, filepath.Join(menu, "Prisma.lnk"), filepath.Join(dados, "prisma", "prisma.ico"))
	default:
		dados, err := dadosUsuario()
		if err != nil {
			return err
		}
		alvos = append(alvos,
			filepath.Join(dados, "applications", AppID+".desktop"),
			filepath.Join(dados, "applications", "prisma.desktop"),
			filepath.Join(dados, "icons", "hicolor", "scalable", "apps", "prisma.svg"),
			filepath.Join(dados, "icons", "hicolor", "256x256", "apps", "prisma.png"),
		)
		defer exec.Command("update-desktop-database", filepath.Join(dados, "applications")).Run()
	}
	removidos := 0
	for _, p := range alvos {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			fmt.Printf("não consegui remover %s: %v\n", p, err)
			continue
		}
		fmt.Printf("removido: %s\n", p)
		removidos++
	}
	if removidos == 0 {
		fmt.Println("Nenhum atalho do Prisma para remover.")
	}
	return nil
}

// menuIniciar é %APPDATA%\Microsoft\Windows\Start Menu\Programs.
func menuIniciar() (string, error) {
	roaming, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("não achei o AppData: %w", err)
	}
	return filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs"), nil
}

// instalarWindows cria Prisma.lnk no menu Iniciar (via PowerShell, que já vem no Windows).
func instalarWindows(app string) error {
	dados, err := dadosUsuario()
	if err != nil {
		return err
	}
	icone := filepath.Join(dados, "prisma", "prisma.ico")
	if err := grava(icone, packaging.IconeICO, 0o644); err != nil {
		return err
	}
	menu, err := menuIniciar()
	if err != nil {
		return err
	}
	atalho := filepath.Join(menu, "Prisma.lnk")
	// aspas simples no PowerShell: só a própria aspa precisa ser dobrada
	q := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	script := fmt.Sprintf("$s = (New-Object -ComObject WScript.Shell).CreateShortcut('%s'); $s.TargetPath = '%s'; "+
		"$s.WorkingDirectory = '%s'; $s.IconLocation = '%s'; $s.Description = 'Prisma: finanças pessoais'; $s.Save()",
		q(atalho), q(app), q(filepath.Dir(app)), q(icone))
	if err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
		return fmt.Errorf("o PowerShell não conseguiu criar o atalho: %w", err)
	}
	fmt.Printf("✔ Prisma no menu Iniciar (%s).\n", atalho)
	return nil
}

// infoPlist descreve o pacote Prisma.app do macOS.
func infoPlist() string {
	v := strings.TrimPrefix(update.Versao, "v")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Prisma</string>
  <key>CFBundleDisplayName</key><string>Prisma</string>
  <key>CFBundleIdentifier</key><string>io.github.jpbarbosa44.prisma</string>
  <key>CFBundleVersion</key><string>%[1]s</string>
  <key>CFBundleShortVersionString</key><string>%[1]s</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>Prisma</string>
  <key>CFBundleIconFile</key><string>prisma</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`, v)
}

// instalarMac monta ~/Applications/Prisma.app, que chama o prisma-app instalado.
func instalarMac(app string) error {
	casa, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("não achei a pasta do usuário: %w", err)
	}
	pacote := filepath.Join(casa, "Applications", "Prisma.app", "Contents")
	if err := grava(filepath.Join(pacote, "Info.plist"), []byte(infoPlist()), 0o644); err != nil {
		return err
	}
	if err := grava(filepath.Join(pacote, "Resources", "prisma.icns"), packaging.IconeICNS, 0o644); err != nil {
		return err
	}
	aspas := strings.ReplaceAll(app, "'", `'\''`)
	lancador := fmt.Sprintf("#!/bin/sh\nexec '%s' \"$@\"\n", aspas)
	if err := grava(filepath.Join(pacote, "MacOS", "Prisma"), []byte(lancador), 0o755); err != nil {
		return err
	}
	fmt.Printf("✔ Prisma.app em %s (abra pelo Launchpad ou pelo Spotlight).\n", filepath.Join(casa, "Applications"))
	return nil
}
