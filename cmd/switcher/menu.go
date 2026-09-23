//go:build windows

package main

import (
	"errors"
	"log"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/menupaint"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

// maxDeviceSlots caps how many playback devices can be listed in the tray
// menu at once. Slots are pre-created and hidden/shown as the device list
// changes, since the tray library has no way to insert or reorder menu
// items after the fact.
const maxDeviceSlots = 16

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
	current, currentErr := a.worker.Current()
	if currentErr != nil {
		log.Printf("get current device: %v", currentErr)
	}

	// Keep the config file's device rows current: new devices, Windows
	// names and which one is active. Without a known default output,
	// leave the file alone rather than clear its active flag.
	cfg := a.devices()
	if currentErr == nil && outputconfig.Stale(toConfigDevices(devices), current.ID, cfg) {
		merged, err := a.syncConfig(devices, current.ID)
		switch {
		case err == nil:
			cfg = merged
		case !errors.Is(err, errConfigInvalid):
			log.Printf("update devices in config: %v", err)
		}
	}

	systray.SetTooltip(a.tooltip(outputconfig.DisplayName(cfg, current.ID, current.Name)))

	a.deviceMu.Lock()
	defer a.deviceMu.Unlock()

	entries := make([]menupaint.Entry, 0, len(devices))
	for i := range a.deviceSlots {
		item := a.deviceSlots[i].item
		if i >= len(devices) {
			item.Hide()
			a.deviceSlots[i].id = ""
			continue
		}

		d := devices[i]
		name := outputconfig.DisplayName(cfg, d.ID, d.Name)
		item.SetTitle(name)
		item.SetTooltip("Switch to " + name)
		a.deviceSlots[i].id = d.ID
		if d.ID == current.ID {
			item.Check()
		} else {
			item.Uncheck()
		}
		item.Show()
		entries = append(entries, menupaint.Entry{Text: name, Grey: cfg[d.ID].Skip})
	}
	// The hidden slots are dropped from the menu, so the visible devices
	// are its first entries, in this order - which is what menupaint
	// matches its list against when the menu opens.
	menupaint.SetEntries(entries)
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
