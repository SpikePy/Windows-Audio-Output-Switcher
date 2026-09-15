//go:build windows

// Package notifier shows native Windows toast notifications.
package notifier

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	appID = "Audio Output Switcher"
	// tag and group are shared by every notification this app shows. A
	// new toast with the same tag+group replaces the previous one
	// instead of stacking, so pressing the switch hotkey repeatedly
	// only ever shows the latest device.
	tag   = "audio-output-switcher"
	group = "audio-output-switcher"

	// createNoWindow (CREATE_NO_WINDOW) stops the spawned powershell.exe
	// from flashing a console window, since this app has none of its own.
	createNoWindow = 0x08000000
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

	// -EncodedCommand takes the script as Base64-encoded UTF-16LE
	// directly on the command line, so there's no temp .ps1 file to
	// create, clean up, or have execution policy / antivirus scanning
	// delay it.
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-EncodedCommand", encodeCommand(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("show toast: %w: %s", err, out)
	}
	return nil
}

func encodeCommand(script string) string {
	u16 := utf16.Encode([]rune(script))
	buf := make([]byte, len(u16)*2)
	for i, u := range u16 {
		buf[2*i] = byte(u)
		buf[2*i+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// escapeCDATA guards against a literal "]]>" ending the CDATA section
// early; XML's other special characters are otherwise fine verbatim
// inside CDATA.
func escapeCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}
