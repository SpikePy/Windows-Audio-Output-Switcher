// Command switcher runs in the system tray and cycles the default Windows
// playback device on a global hotkey press.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/configwindow"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/llhotkey"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

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

	// skip holds the device names excluded from cycling (see
	// internal/outputconfig). It's always replaced wholesale, never
	// mutated in place, so reading it under skipMu and then using the
	// returned map after unlocking is safe.
	skip   map[string]bool
	skipMu sync.Mutex

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

	a := &app{skip: outputconfig.Load()}
	systray.Run(a.onReady, a.onExit)
}

func (a *app) onReady() {
	systray.SetIcon(icons.IconEnabled)
	systray.SetTooltip(a.tooltip())

	for i := range a.deviceSlots {
		item := systray.AddMenuItemCheckbox("", "", false)
		item.Hide()
		a.deviceSlots[i].item = item
		go a.watchDeviceSlot(i)
	}
	systray.AddSeparator()

	a.mConfigOutputs = systray.AddMenuItem("Configure Outputs...", "Choose which outputs to include when switching")
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
			a.openConfigWindow()
		case <-a.mExit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// openConfigWindow gathers every device name worth showing - currently
// active ones, plus any previously excluded name even if that device
// isn't connected right now - and opens the Configure Outputs window.
func (a *app) openConfigWindow() {
	devices, err := a.worker.List()
	if err != nil {
		log.Printf("list devices for config window: %v", err)
	}

	a.skipMu.Lock()
	skip := a.skip
	a.skipMu.Unlock()

	seen := make(map[string]bool, len(devices)+len(skip))
	names := make([]string, 0, len(devices)+len(skip))
	for _, d := range devices {
		if !seen[d.Name] {
			seen[d.Name] = true
			names = append(names, d.Name)
		}
	}
	for name := range skip {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	configwindow.Open(names, skip, a.onOutputsSaved)
}

// onOutputsSaved is called from the config window's own thread once the
// user clicks Save.
func (a *app) onOutputsSaved(skip map[string]bool) {
	a.skipMu.Lock()
	a.skip = skip
	a.skipMu.Unlock()

	if err := outputconfig.Save(skip); err != nil {
		log.Printf("saving output config: %v", err)
	}
	a.syncDeviceMenu()
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
// or unplugging a device (or switching it elsewhere) is reflected without
// requiring a restart.
func (a *app) watchDeviceChanges() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
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

	a.skipMu.Lock()
	skip := a.skip
	a.skipMu.Unlock()

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
		title := d.Name
		if skip[d.Name] {
			title += excludedSuffix
		}
		item.SetTitle(title)
		item.SetTooltip("Switch to " + d.Name)
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
	a.skipMu.Lock()
	skip := a.skip
	a.skipMu.Unlock()

	result := a.worker.Next(skip)
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

func (a *app) tooltip() string {
	return fmt.Sprintf("Audio Output Switcher %s — hotkey %s\nLeft-click: switch now. Right-click: pick a device.", version, hotkeyCombo)
}
