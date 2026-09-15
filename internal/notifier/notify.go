//go:build windows

// Package notifier shows native Windows toast notifications.
package notifier

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	appID = "Audio Output Switcher"
	// tag and group are shared by every notification this app shows. A
	// new toast with the same tag+group replaces the previous one
	// instead of stacking, so pressing the switch hotkey repeatedly
	// only ever shows the latest device.
	tag   = "audio-output-switcher"
	group = "audio-output-switcher"
)

const scriptTemplate = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.UI.Notifications.ToastNotification, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null

$template = @"
<toast duration="short">
    <visual>
        <binding template="ToastGeneric">
            <text><![CDATA[%s]]></text>
            <text><![CDATA[%s]]></text>
        </binding>
    </visual>
    <audio src="ms-winsoundevent:Notification.Default" />
</toast>
"@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)
$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
$toast.Tag = '%s'
$toast.Group = '%s'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show($toast)
`

// Show displays a toast notification with the given title and message,
// replacing any notification this app is currently showing.
func Show(title, message string) error {
	script := fmt.Sprintf(scriptTemplate, escapeCDATA(title), escapeCDATA(message), tag, group, appID)

	tmp, err := os.CreateTemp("", "audio-output-switcher-*.ps1")
	if err != nil {
		return fmt.Errorf("create notification script: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return fmt.Errorf("write notification script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("show toast: %w: %s", err, out)
	}
	return nil
}

// escapeCDATA guards against a literal "]]>" ending the CDATA section
// early; XML's other special characters are otherwise fine verbatim
// inside CDATA.
func escapeCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}
