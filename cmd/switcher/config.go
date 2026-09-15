//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

// createNoWindow (CREATE_NO_WINDOW) stops a spawned helper process from
// flashing a console window, since this app has none of its own.
const createNoWindow = 0x08000000

// errConfigInvalid is returned by syncConfig while the config file on disk
// fails to parse: writing it then would replace the user's hand-edits (and
// every disconnected device's row) with defaults.
var errConfigInvalid = errors.New("config file is invalid; not overwriting it")

// openConfigFile (re)writes the config file so it lists every currently
// known device - active ones, plus any previously excluded name even if
// that device isn't connected right now - then opens it in whatever
// application Windows has associated with .yaml files, for the user to
// hand-edit (devices, or the hotkey).
func (a *app) openConfigFile() {
	devices, err := a.worker.List()
	if err != nil {
		log.Printf("list devices for config file: %v", err)
	}

	// Open the file even if refreshing it failed - e.g. it has an error
	// the user now needs to fix.
	if _, err := a.syncConfig(devices); err != nil {
		log.Printf("sync config: %v", err)
	}
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

// syncConfig writes the config file via outputconfig.Sync (adding any
// device in devices it doesn't already have an entry for, refreshing
// LastSeen for all of them, and never dropping an entry for a device that
// isn't in devices, or touching the saved hotkey/poll interval), and
// updates a.cfg/a.configModTime to match. It first picks up any edit made
// since the file was last read, so it never writes over one - and refuses
// to write at all while the file on disk fails to parse.
func (a *app) syncConfig(devices []audio.Device) (map[string]outputconfig.Entry, error) {
	a.reloadConfigIfChanged()

	a.cfgMu.Lock()
	cfg, pollInterval, configErr := a.cfg, a.pollInterval, a.configErr
	a.cfgMu.Unlock()
	if configErr != nil {
		return nil, errConfigInvalid
	}

	merged, err := outputconfig.Sync(a.currentHotkey(), int(pollInterval/time.Second), toConfigDevices(devices), cfg)
	if err != nil {
		return nil, err
	}

	modTime := outputconfig.ModTime()
	a.cfgMu.Lock()
	a.cfg = merged
	a.configModTime = modTime
	a.cfgMu.Unlock()
	return merged, nil
}

func toConfigDevices(devices []audio.Device) []outputconfig.Device {
	out := make([]outputconfig.Device, len(devices))
	for i, d := range devices {
		out[i] = outputconfig.Device{ID: d.ID, Name: d.Name}
	}
	return out
}

// hasNewDevice reports whether devices contains one with no entry in cfg
// yet, e.g. one just plugged in.
func hasNewDevice(devices []audio.Device, cfg map[string]outputconfig.Entry) bool {
	for _, d := range devices {
		if _, ok := cfg[d.ID]; !ok {
			return true
		}
	}
	return false
}

// reloadConfigIfChanged picks up an edit made outside the app (i.e. in
// whatever editor openConfigFile opened) by comparing the config file's
// mtime against the last time it was read - device settings, the poll
// interval, and the hotkey if it changed to something that still parses
// and registers. If the edit left the file unparseable, the previous
// settings stay in effect and the user is told.
func (a *app) reloadConfigIfChanged() {
	mt := outputconfig.ModTime()

	a.cfgMu.Lock()
	changed := !mt.IsZero() && !mt.Equal(a.configModTime)
	if changed {
		a.configModTime = mt
	}
	a.cfgMu.Unlock()

	if !changed {
		return
	}

	loaded, err := outputconfig.Load()
	if err != nil {
		a.cfgMu.Lock()
		a.configErr = err
		a.cfgMu.Unlock()
		log.Printf("config file is invalid, keeping previous settings: %v", err)
		osd.Show("Config file has an error - keeping previous settings")
		return
	}

	a.cfgMu.Lock()
	a.cfg = loaded.Devices
	a.pollInterval = loaded.EffectivePollInterval()
	a.configErr = nil
	a.cfgMu.Unlock()

	if newCombo := loaded.EffectiveHotkey(); newCombo != a.currentHotkey() {
		if err := a.applyHotkey(newCombo); err != nil {
			log.Printf("failed to apply new hotkey %q: %v", newCombo, err)
			osd.Show(fmt.Sprintf("Could not use hotkey %s: %v", newCombo, err))
		} else {
			osd.Show("Hotkey set to " + newCombo)
		}
	}
}

func (a *app) devices() map[string]outputconfig.Entry {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg
}

func (a *app) currentPollInterval() time.Duration {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.pollInterval
}

func (a *app) configError() error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.configErr
}

// skipSet returns the IDs of the devices currently excluded from cycling.
func (a *app) skipSet() map[string]bool {
	cfg := a.devices()
	skip := make(map[string]bool, len(cfg))
	for id, e := range cfg {
		if e.Skip {
			skip[id] = true
		}
	}
	return skip
}

// displayName returns d's configured alias, or its Windows name if it has
// none - see outputconfig.DisplayName.
func (a *app) displayName(d audio.Device) string {
	return outputconfig.DisplayName(a.devices(), d.ID, d.Name)
}
