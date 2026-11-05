package notifica

import (
	"fmt"
	"os/exec"
	"strings"

	"prisma/internal/desktop"
)

// appID do PowerShell: um toast precisa de um AppUserModelID registrado, e o
// do PowerShell existe em todo Windows 10/11 (o aviso aparece como dele).
const appID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

func enviar(titulo, texto string) error {
	q := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	script := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode('%s')) > $null
$x.Item(1).AppendChild($t.CreateTextNode('%s')) > $null
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show([Windows.UI.Notifications.ToastNotification]::new($t))`,
		q(titulo), q(texto), q(appID))
	return desktop.SemJanela(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)).Run()
}
