//go:build windows

// Command switcher runs in the system tray and cycles the default Windows
// playback device on a global hotkey press.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/llhotkey"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

// createNoWindow (CREATE_NO_WINDOW) stops a spawned helper process from
// flashing a console window, since this app has none of its own.
const createNoWindow = 0x08000000

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

// hotkeyCombo is the fixed global shortcut that cycles the active output
// device. It's intentionally not user-configurable (no config file):
// Win+S is normally reserved by the shell for Search, but internal/llhotkey
// intercepts it via a low-level keyboard hook before the shell sees it.
const hotkeyCombo = "win+s"

// maxDeviceSlots caps how many playback devices can be listed in the tray
// menu at once. Slots are pre-created and hidden/shown as the device list
// changes, since the tray library has no way to insert or reorder menu
// items after the fact.
const maxDeviceSlots = 16

// excludedSuffix marks a device excluded from cycling in the menu. Windows
// menu items can't be greyed out while staying clickable (MF_GRAYED also
// blocks the click at the OS level, and the tray library has no
// owner-draw hook to fake it), so excluded devices are marked in the
// label instead - they stay fully clickable for a direct, one-off switch.
const excludedSuffix = "  (excluded)"

type deviceSlot struct {
	item *systray.MenuItem
	id   string
}

type app struct {
	worker *audio.Worker
	hk     *llhotkey.Hotkey

	deviceSlots [maxDeviceSlots]deviceSlot
	deviceMu    sync.Mutex

	// cfg holds each known device's saved settings (alias, skip), keyed
	// by device name (see internal/outputconfig). It's always replaced
	// wholesale, never mutated in place, so reading it under cfgMu and
	// then using the returned map after unlocking is safe.
	cfg   map[string]outputconfig.Entry
	cfgMu sync.Mutex

	// configModTime is the output config file's mtime as of the last
	// time it was read (by us writing it, or by picking up an edit made
	// in the user's editor) - see reloadConfigIfChanged.
	configModTime time.Time
	configMu      sync.Mutex

	mConfigOutputs *systray.MenuItem
	mExit          *systray.MenuItem
}

// logFilePath is where diagnostic output goes. This app has no console
// (it builds with -H=windowsgui), so log.Print output would otherwise
// vanish silently - writing to a file makes failures (e.g. a hotkey that
// failed to register, or a notification that failed to show) inspectable
// after the fact.
func logFilePath() string {
	return filepath.Join(os.TempDir(), "AudioOutputSwitcher.log")
}

func main() {
	if f, err := os.OpenFile(logFilePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Printf("Audio Output Switcher %s starting", version)

	a := &app{cfg: outputconfig.Load(), configModTime: outputconfig.ModTime()}
	systray.Run(a.onReady, a.onExit)
}

func (a *app) onReady() {
	systray.SetIcon(icons.IconEnabled)
	systray.SetTooltip(a.tooltip(""))

	for i := range a.deviceSlots {
		item := systray.AddMenuItemCheckbox("", "", false)
		item.Hide()
		a.deviceSlots[i].item = item
		go a.watchDeviceSlot(i)
	}
	systray.AddSeparator()

	a.mConfigOutputs = systray.AddMenuItem("Configure", "Open the device config file to rename or exclude outputs")
	systray.AddSeparator()
	a.mExit = systray.AddMenuItem("Exit", "Quit Audio Output Switcher")

	// Left click switches to the next output device directly, same as
	// the hotkey; right click shows the menu built above.
	systray.SetOnTapped(a.switchOutput)

	a.worker = audio.StartWorker()
	a.registerHotkey()
	a.syncDeviceMenu()

	go a.watchMenu()
	go a.watchDeviceChanges()
}

func (a *app) onExit() {
	if a.hk != nil {
		llhotkey.Unregister(a.hk)
	}
	llhotkey.Stop()
	if a.worker != nil {
		a.worker.Stop()
	}
}

func (a *app) registerHotkey() {
	mods, key, err := hotkeycfg.Parse(hotkeyCombo)
	if err != nil {
		// hotkeyCombo is a compile-time constant; a parse failure here
		// is a programming error, not a runtime condition to recover
		// from gracefully.
		log.Fatalf("invalid built-in hotkey %q: %v", hotkeyCombo, err)
	}

	hk := llhotkey.New(mods.Ctrl, mods.Alt, mods.Shift, mods.Win, key)
	if err := llhotkey.Register(hk); err != nil {
		log.Printf("failed to register hotkey %q: %v", hotkeyCombo, err)
		osd.Show(fmt.Sprintf("Could not register the switch hotkey (%s): %v", hotkeyCombo, err))
		return
	}

	a.hk = hk
	go a.handleHotkey()
}

func (a *app) handleHotkey() {
	for range a.hk.Keydown() {
		a.switchOutput()
	}
}

func (a *app) watchMenu() {
	for {
		select {
		case <-a.mConfigOutputs.ClickedCh:
			a.openConfigFile()
		case <-a.mExit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// openConfigFile (re)writes the output config file so it lists every
// currently known device - active ones, plus any previously excluded
// name even if that device isn't connected right now - then opens it in
// whatever application Windows has associated with .yaml files, for the
// user to hand-edit.
func (a *app) openConfigFile() {
	devices, err := a.worker.List()
	if err != nil {
		log.Printf("list devices for config file: %v", err)
	}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.Name
	}

	a.cfgMu.Lock()
	cfg := a.cfg
	a.cfgMu.Unlock()

	merged, err := outputconfig.Sync(names, cfg)
	if err != nil {
		log.Printf("sync output config: %v", err)
		return
	}
	a.cfgMu.Lock()
	a.cfg = merged
	a.cfgMu.Unlock()
	a.configMu.Lock()
	a.configModTime = outputconfig.ModTime()
	a.configMu.Unlock()
	a.syncDeviceMenu()

	if err := openInDefaultApp(outputconfig.Path()); err != nil {
		log.Printf("open output config file: %v", err)
		osd.Show("Could not open the config file")
	}
}

// openInDefaultApp opens path with whatever application Windows has
// associated with its extension - the same as double-clicking it in
// Explorer - without blocking the caller.
func openInDefaultApp(path string) error {
	// The empty "" argument is `start`'s window-title placeholder -
	// without it, `start` treats a quoted path containing spaces as the
	// title instead of the target to open.
	cmd := exec.Command("cmd", "/C", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Start()
}

// reloadConfigIfChanged picks up an edit made outside the app (i.e. in
// whatever editor openConfigFile opened) by comparing the config file's
// mtime against the last time it was read.
func (a *app) reloadConfigIfChanged() {
	mt := outputconfig.ModTime()

	a.configMu.Lock()
	changed := !mt.IsZero() && !mt.Equal(a.configModTime)
	if changed {
		a.configModTime = mt
	}
	a.configMu.Unlock()

	if !changed {
		return
	}

	a.cfgMu.Lock()
	a.cfg = outputconfig.Load()
	a.cfgMu.Unlock()
}

// skipSet returns the set of device names currently excluded from
// cycling, derived from cfg.
func (a *app) skipSet() map[string]bool {
	a.cfgMu.Lock()
	cfg := a.cfg
	a.cfgMu.Unlock()

	skip := make(map[string]bool, len(cfg))
	for name, e := range cfg {
		if e.Skip {
			skip[name] = true
		}
	}
	return skip
}

// watchDeviceSlot forwards clicks on one tray menu device entry to a
// direct switch to that device, whatever device currently occupies the
// slot.
func (a *app) watchDeviceSlot(i int) {
	for range a.deviceSlots[i].item.ClickedCh {
		a.deviceMu.Lock()
		id := a.deviceSlots[i].id
		a.deviceMu.Unlock()
		if id != "" {
			a.switchTo(id)
		}
	}
}

// watchDeviceChanges periodically refreshes the device menu so plugging
// or unplugging a device (or switching it elsewhere) is reflected
// without requiring a restart, and picks up edits made to the output
// config file in the meantime.
func (a *app) watchDeviceChanges() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.reloadConfigIfChanged()
		a.syncDeviceMenu()
	}
}

// syncDeviceMenu re-reads the list of active playback devices and updates
// the tray menu's device entries (label, checkmark, visibility) to match.
func (a *app) syncDeviceMenu() {
	devices, err := a.worker.List()
	if err != nil {
		log.Printf("list devices: %v", err)
		return
	}
	current, err := a.worker.Current()
	if err != nil {
		log.Printf("get current device: %v", err)
	}

	a.cfgMu.Lock()
	cfg := a.cfg
	a.cfgMu.Unlock()

	systray.SetTooltip(a.tooltip(current.Name))

	a.deviceMu.Lock()
	defer a.deviceMu.Unlock()

	for i := range a.deviceSlots {
		item := a.deviceSlots[i].item
		if i >= len(devices) {
			item.Hide()
			a.deviceSlots[i].id = ""
			continue
		}

		d := devices[i]
		title := outputconfig.DisplayName(cfg, d.Name)
		if cfg[d.Name].Skip {
			title += excludedSuffix
		}
		item.SetTitle(title)
		item.SetTooltip("Switch to " + title)
		a.deviceSlots[i].id = d.ID
		if d.ID == current.ID {
			item.Check()
		} else {
			item.Uncheck()
		}
		item.Show()
	}
}

func (a *app) switchOutput() {
	result := a.worker.Next(a.skipSet())
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		osd.Show("Could not switch output: " + result.Err.Error())
	case !result.Switched:
		osd.Show("Only one output available: " + result.Device.Name)
	default:
		osd.Show(result.Device.Name)
	}
	a.syncDeviceMenu()
}

// switchTo makes the device with the given ID active directly, as
// requested from the tray menu's device list.
func (a *app) switchTo(id string) {
	result := a.worker.SwitchTo(id)
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		osd.Show("Could not switch output: " + result.Err.Error())
	case result.Switched:
		osd.Show(result.Device.Name)
	}
	a.syncDeviceMenu()
}

func (a *app) tooltip(currentName string) string {
	if currentName == "" {
		return fmt.Sprintf("Audio Output Switcher %s — hotkey %s\nLeft-click: switch now. Right-click: pick a device.", version, hotkeyCombo)
	}
	return fmt.Sprintf("Audio Output Switcher %s — active: %s\nLeft-click: switch now. Right-click: pick a device.", version, currentName)
}
