//go:build windows

package main

import (
	"errors"
	"log"
	"strings"
	"time"
	"unicode"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

// maxDeviceSlots caps how many playback devices can be listed in the tray
// menu at once. Slots are pre-created and hidden/shown as the device list
// changes, since the tray library has no way to insert or reorder menu
// items after the fact.
const maxDeviceSlots = 16

// strikeMark is U+0336 COMBINING LONG STROKE OVERLAY: it draws a line
// through the character in front of it, which is how a skipped device is
// struck through in the menu. Windows menu items can't be greyed out
// while staying clickable (MF_GRAYED also blocks the click at the OS
// level, and the tray library has no owner-draw hook to fake it), so the
// strike lives in the label text itself - the item stays fully clickable
// for a direct, one-off switch.
const strikeMark = '\u0336'

// strikeThrough returns s with every character struck through. Combining
// marks already in s are left to attach to their base character, so the
// stroke is added once per visible character rather than once per rune.
func strikeThrough(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 3)
	struck := false
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) && struck {
			b.WriteRune(strikeMark)
			struck = false
		}
		b.WriteRune(r)
		if !unicode.Is(unicode.Mn, r) {
			struck = true
		}
	}
	if struck {
		b.WriteRune(strikeMark)
	}
	return b.String()
}

type deviceSlot struct {
	item *systray.MenuItem
	id   string
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

// syncDeviceMenu re-reads the list of active playback devices and updates
// the tray menu's device entries (label, checkmark, visibility) to match.
func (a *app) syncDeviceMenu() {
	a.lastRefresh.Store(time.Now().UnixNano())

	devices, err := a.worker.List()
	if err != nil {
		log.Printf("list devices: %v", err)
		return
	}
	current, err := a.worker.Current()
	if err != nil {
		log.Printf("get current device: %v", err)
	}

	cfg := a.devices()
	if hasNewDevice(devices, cfg) {
		merged, err := a.syncConfig(devices)
		switch {
		case err == nil:
			cfg = merged
		case !errors.Is(err, errConfigInvalid):
			log.Printf("auto-add new device(s) to config: %v", err)
		}
	}

	systray.SetTooltip(a.tooltip(outputconfig.DisplayName(cfg, current.ID, current.Name)))

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
		name := outputconfig.DisplayName(cfg, d.ID, d.Name)
		title := name
		if cfg[d.ID].Skip {
			title = strikeThrough(name)
		}
		item.SetTitle(title)
		// The tooltip keeps the plain name: the strikethrough is there to
		// be seen in the menu, not read out again in a hover text.
		item.SetTooltip("Switch to " + name)
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
	a.switchMu.Lock()
	defer a.switchMu.Unlock()

	result := a.worker.Next(a.skipSet())
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		osd.Show("Could not switch output: " + result.Err.Error())
	case !result.Switched:
		osd.Show("Only one output available: " + a.displayName(result.Device))
	default:
		osd.Show(a.displayName(result.Device))
	}
	a.syncDeviceMenu()
}

// switchTo makes the device with the given ID active directly, as
// requested from the tray menu's device list.
func (a *app) switchTo(id string) {
	a.switchMu.Lock()
	defer a.switchMu.Unlock()

	result := a.worker.SwitchTo(id)
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		osd.Show("Could not switch output: " + result.Err.Error())
	case result.Switched:
		osd.Show(a.displayName(result.Device))
	}
	a.syncDeviceMenu()
}
