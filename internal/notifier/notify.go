//go:build windows

// Package notifier shows native Windows toast notifications.
package notifier

import (
	"encoding/base64"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/aumid"
)

const (
	// appID must match the AppUserModelID registered by internal/aumid
	// (via a Start Menu shortcut) - otherwise Windows can't resolve a
	// display name/icon/settings entry for it, and tends to file the
	// notification straight into Action Center without ever showing the
	// on-screen banner.
	appID = aumid.ID
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

// pending tracks the most recently spawned, still-running notification
// process (if any), so a new Show() can kill it before it has a chance
// to display stale content.
var (
	mu      sync.Mutex
	pending *exec.Cmd
)

// Show displays a toast notification with the given title and message.
// Spawning powershell.exe to talk to the WinRT toast API is slow enough
// (up to a couple of seconds) that this must never block the caller -
// switching the audio device must not wait on it - so it runs
// asynchronously and logs any failure instead of returning it. If a
// previous call is still in flight when a new one comes in, it's killed
// first so a burst of rapid switches can't show stale, out-of-order
// content.
func Show(title, message string) {
	script := fmt.Sprintf(scriptTemplate, escapeCDATA(title), escapeCDATA(message), tag, group, appID)

	// -EncodedCommand takes the script as Base64-encoded UTF-16LE
	// directly on the command line, so there's no temp .ps1 file to
	// create, clean up, or have execution policy / antivirus scanning
	// delay it.
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-EncodedCommand", encodeCommand(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	mu.Lock()
	if pending != nil && pending.Process != nil {
		_ = pending.Process.Kill()
	}
	pending = cmd
	mu.Unlock()

	go func() {
		out, err := cmd.CombinedOutput()

		mu.Lock()
		if pending == cmd {
			pending = nil
		}
		mu.Unlock()

		if err != nil {
			log.Printf("show toast: %v: %s", err, out)
		}
	}()
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
