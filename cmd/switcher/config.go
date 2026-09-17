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
// fails to parse or has an invalid value: writing it then would replace
// the user's hand-edits (and, for a file that doesn't parse, every
// disconnected device's row) with defaults.
var errConfigInvalid = errors.New("config file has errors; not overwriting it")

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
// isn't in devices, and writing back the file's own hotkey/autostart/poll
// interval - never a command-line override), and
// updates a.cfg/a.configModTime to match. It first picks up any edit made
// since the file was last read, so it never writes over one - and refuses
// to write at all while the file on disk fails to parse.
func (a *app) syncConfig(devices []audio.Device) (map[string]outputconfig.Entry, error) {
	a.reloadConfigIfChanged()

	a.cfgMu.Lock()
	cfg, fileSettings, configErr, problems := a.cfg, a.fileSettings, a.configErr, a.problems
	a.cfgMu.Unlock()
	if configErr != nil || len(problems) > 0 {
		return nil, errConfigInvalid
	}

	merged, err := outputconfig.Sync(fileSettings, toConfigDevices(devices), cfg)
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
// mtime against the last time it was read - device settings, autostart,
// the poll interval, and the hotkey if it changed to something that still parses
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
	before := a.fileSettings.With(a.overrides)
	a.cfg = loaded.Devices
	a.fileSettings = loaded.Settings()
	after := a.fileSettings.With(a.overrides)
	// Apply autostart after an invalid file, too: the startup check
	// skipped it then.
	wasInvalid := a.configErr != nil
	a.configErr = nil
	a.problems = loaded.Problems
	a.cfgMu.Unlock()

	for _, p := range loaded.Problems {
		log.Printf("config file: %s - using its default", p)
	}
	a.announceConfigTrouble()

	if after.Autostart != before.Autostart || wasInvalid {
		applyAutostart(after.Autostart)
	}

	newCombo := after.Hotkey
	if newCombo == a.currentHotkey() {
		return
	}
	if !outputconfig.HotkeyEnabled(newCombo) {
		a.disableHotkey(newCombo)
		log.Print("hotkey switched off")
		osd.Show("Hotkey switched off")
		return
	}
	if err := a.applyHotkey(newCombo); err != nil {
		log.Printf("failed to apply new hotkey %q: %v", newCombo, err)
		osd.Show(fmt.Sprintf("Could not use hotkey %s: %v", newCombo, err))
		return
	}
	osd.Show("Hotkey set to " + newCombo)
}

func (a *app) devices() map[string]outputconfig.Entry {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg
}

// settings returns the values the app runs with: the config file's,
// with any command-line overrides applied.
func (a *app) settings() outputconfig.Settings {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.fileSettings.With(a.overrides)
}

func (a *app) currentPollInterval() time.Duration {
	return a.settings().PollInterval()
}

// announceConfigTrouble tells the user, on screen, if the config file
// can't be read or has a value that was replaced by its default.
func (a *app) announceConfigTrouble() {
	a.cfgMu.Lock()
	configErr, problems := a.configErr, a.problems
	a.cfgMu.Unlock()

	switch {
	case configErr != nil:
		osd.Show("Config file has an error - using defaults until it's fixed")
	case len(problems) == 1:
		osd.Show("Config file: " + problems[0] + " - using its default")
	case len(problems) > 1:
		osd.Show(fmt.Sprintf("Config file has %d invalid values - using their defaults", len(problems)))
	}
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
