// Command switcher runs in the system tray and cycles the default Windows
// playback device on a global hotkey press.
package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/llhotkey"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/notifier"
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

type deviceSlot struct {
	item *systray.MenuItem
	id   string
}

type app struct {
	enabled bool

	worker *audio.Worker
	hk     *llhotkey.Hotkey

	deviceSlots [maxDeviceSlots]deviceSlot
	deviceMu    sync.Mutex

	mEnable  *systray.MenuItem
	mDisable *systray.MenuItem
	mExit    *systray.MenuItem
}

func main() {
	a := &app{enabled: true}
	systray.Run(a.onReady, a.onExit)
}

func (a *app) onReady() {
	systray.SetIcon(a.iconBytes())
	systray.SetTooltip(a.tooltip())

	for i := range a.deviceSlots {
		item := systray.AddMenuItemCheckbox("", "", false)
		item.Hide()
		a.deviceSlots[i].item = item
		go a.watchDeviceSlot(i)
	}
	systray.AddSeparator()

	a.mEnable = systray.AddMenuItem("Enable", "Enable the switch hotkey")
	a.mDisable = systray.AddMenuItem("Disable", "Disable the switch hotkey")
	systray.AddSeparator()
	a.mExit = systray.AddMenuItem("Exit", "Quit Audio Output Switcher")
	a.updateMenuState()

	// Left click switches to the next output device directly, same as
	// the hotkey; right click shows the menu built above, listing every
	// output device plus Enable/Disable/Exit.
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
		_ = notifier.Show("Audio Output Switcher",
			fmt.Sprintf("Could not register the switch hotkey (%s): %v", hotkeyCombo, err))
		return
	}

	a.hk = hk
	go a.handleHotkey()
}

func (a *app) handleHotkey() {
	for range a.hk.Keydown() {
		if a.enabled {
			a.switchOutput()
		}
	}
}

func (a *app) watchMenu() {
	for {
		select {
		case <-a.mEnable.ClickedCh:
			a.setEnabled(true)
		case <-a.mDisable.ClickedCh:
			a.setEnabled(false)
		case <-a.mExit.ClickedCh:
			systray.Quit()
			return
		}
	}
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
		item.SetTitle(d.Name)
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
	result := a.worker.Next()
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		_ = notifier.Show("Audio Output Switcher", "Could not switch output: "+result.Err.Error())
	case !result.Switched:
		_ = notifier.Show("Audio Output Switcher", "Only one output available: "+result.Device.Name)
	default:
		_ = notifier.Show("Audio output switched", result.Device.Name)
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
		_ = notifier.Show("Audio Output Switcher", "Could not switch output: "+result.Err.Error())
	case result.Switched:
		_ = notifier.Show("Audio output switched", result.Device.Name)
	}
	a.syncDeviceMenu()
}

func (a *app) setEnabled(enabled bool) {
	if a.enabled == enabled {
		return
	}
	a.enabled = enabled

	systray.SetIcon(a.iconBytes())
	systray.SetTooltip(a.tooltip())
	a.updateMenuState()
}

func (a *app) updateMenuState() {
	if a.enabled {
		a.mEnable.Disable()
		a.mDisable.Enable()
	} else {
		a.mEnable.Enable()
		a.mDisable.Disable()
	}
}

func (a *app) iconBytes() []byte {
	if a.enabled {
		return icons.IconEnabled
	}
	return icons.IconDisabled
}

func (a *app) tooltip() string {
	state := "enabled"
	if !a.enabled {
		state = "disabled"
	}
	return fmt.Sprintf("Audio Output Switcher %s — hotkey %s is %s\nLeft-click: switch now. Right-click: pick a device.", version, hotkeyCombo, state)
}
