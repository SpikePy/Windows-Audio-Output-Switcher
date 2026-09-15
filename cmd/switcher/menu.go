//go:build windows

package main

import (
	"errors"
	"log"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

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
		title := outputconfig.DisplayName(cfg, d.ID, d.Name)
		if cfg[d.ID].Skip {
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
