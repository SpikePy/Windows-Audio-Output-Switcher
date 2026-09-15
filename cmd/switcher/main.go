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

	// hk and hotkeyCombo are the currently-registered global hotkey and
	// the combo string (see internal/hotkeycfg) it was built from -
	// outputconfig.DefaultHotkey ("win+s") if never customized in the
	// config file. Always changed together via applyHotkey.
	hk          *llhotkey.Hotkey
	hotkeyCombo string
	hotkeyMu    sync.Mutex

	// switchMu serializes switchOutput/switchTo end to end - from
	// performing the switch through announcing it in the OSD - across
	// every trigger path (hotkey, tray left-click, and each per-device
	// menu item), which otherwise run on independent goroutines with
	// nothing else keeping a later switch's announcement from racing
	// ahead of, or losing to, an earlier one's.
	switchMu sync.Mutex

	deviceSlots [maxDeviceSlots]deviceSlot
	deviceMu    sync.Mutex

	// cfgMu guards everything loaded from the config file. cfg is always
	// replaced wholesale, never mutated, so a map read under cfgMu stays
	// safe to use after unlocking.
	cfg           map[string]outputconfig.Entry
	pollInterval  time.Duration // background re-check interval; switches always re-check immediately
	configModTime time.Time     // file mtime as of the last read or write - see reloadConfigIfChanged
	cfgMu         sync.Mutex

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

	loaded := outputconfig.Load()
	a := &app{
		cfg:           loaded.Devices,
		configModTime: outputconfig.ModTime(),
		hotkeyCombo:   loaded.EffectiveHotkey(),
		pollInterval:  loaded.EffectivePollInterval(),
	}
	systray.Run(a.onReady, a.onExit)
}

func (a *app) onReady() {
	systray.SetIcon(icons.Icon)
	systray.SetTooltip(a.tooltip(""))

	for i := range a.deviceSlots {
		item := systray.AddMenuItemCheckbox("", "", false)
		item.Hide()
		a.deviceSlots[i].item = item
		go a.watchDeviceSlot(i)
	}
	systray.AddSeparator()

	a.mConfigOutputs = systray.AddMenuItem("Configure", "Open the config file to rename/exclude outputs or change the hotkey")
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
	a.hotkeyMu.Lock()
	hk := a.hk
	a.hotkeyMu.Unlock()
	if hk != nil {
		llhotkey.Unregister(hk)
	}
	llhotkey.Stop()
	if a.worker != nil {
		a.worker.Stop()
	}
}

// registerHotkey registers a.hotkeyCombo, as loaded from the config file
// at startup (see main).
func (a *app) registerHotkey() {
	combo := a.currentHotkey()
	if err := a.applyHotkey(combo); err != nil {
		log.Printf("failed to register hotkey %q: %v", combo, err)
		osd.Show(fmt.Sprintf("Could not register the switch hotkey (%s): %v", combo, err))
	}
}

// applyHotkey parses and registers combo as the active global hotkey,
// only swapping out whatever was previously registered (if any) once
// the new one is confirmed working - so a bad hand-edit of the config
// file's hotkey never leaves the app with no working hotkey at all.
func (a *app) applyHotkey(combo string) error {
	mods, key, err := hotkeycfg.Parse(combo)
	if err != nil {
		return err
	}

	newHk := llhotkey.New(mods.Ctrl, mods.Alt, mods.Shift, mods.Win, key)
	if err := llhotkey.Register(newHk); err != nil {
		return err
	}

	a.hotkeyMu.Lock()
	oldHk := a.hk
	a.hk = newHk
	a.hotkeyCombo = combo
	a.hotkeyMu.Unlock()

	if oldHk != nil {
		llhotkey.Unregister(oldHk)
	}

	go a.handleHotkey(newHk)
	return nil
}

func (a *app) handleHotkey(hk *llhotkey.Hotkey) {
	for range hk.Keydown() {
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

	if _, err := a.syncConfig(deviceNames(devices)); err != nil {
		log.Printf("sync output config: %v", err)
		return
	}
	a.syncDeviceMenu()

	if err := openInDefaultApp(outputconfig.Path()); err != nil {
		log.Printf("open output config file: %v", err)
		osd.Show("Could not open the config file")
	}
}

// syncConfig writes the config file via outputconfig.Sync (adding any
// name in names it doesn't already have an entry for, refreshing
// LastSeen for all of them, and never dropping an entry for a device
// that isn't in names, or touching the saved hotkey/poll interval), and
// updates a.cfg/a.configModTime to match.
func (a *app) syncConfig(names []string) (map[string]outputconfig.Entry, error) {
	pollSeconds := int(a.currentPollInterval() / time.Second)
	merged, err := outputconfig.Sync(a.currentHotkey(), pollSeconds, names, a.devices())
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

// deviceNames returns the names of devices.
func deviceNames(devices []audio.Device) []string {
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.Name
	}
	return names
}

// hasNewDevice reports whether devices contains a name with no entry in
// cfg yet, e.g. one just plugged in.
func hasNewDevice(devices []audio.Device, cfg map[string]outputconfig.Entry) bool {
	for _, d := range devices {
		if _, ok := cfg[d.Name]; !ok {
			return true
		}
	}
	return false
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
// mtime against the last time it was read - device settings, and the
// hotkey if it changed to something that still parses and registers
// (an edit that doesn't is logged and otherwise ignored, leaving
// whatever hotkey was already working active).
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

	loaded := outputconfig.Load()

	a.cfgMu.Lock()
	a.cfg = loaded.Devices
	a.pollInterval = loaded.EffectivePollInterval()
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

func (a *app) currentHotkey() string {
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	return a.hotkeyCombo
}

// skipSet returns the set of device names currently excluded from
// cycling, derived from cfg.
func (a *app) skipSet() map[string]bool {
	cfg := a.devices()
	skip := make(map[string]bool, len(cfg))
	for name, e := range cfg {
		if e.Skip {
			skip[name] = true
		}
	}
	return skip
}

// displayName returns the alias configured for the device named name,
// or name itself if it has none - see outputconfig.DisplayName.
func (a *app) displayName(name string) string {
	return outputconfig.DisplayName(a.devices(), name)
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

// watchDeviceChanges periodically refreshes the device menu - at
// a.pollInterval, outputconfig.DefaultPollSeconds unless customized in
// the config file, picked up live if it's changed - so plugging or
// unplugging a device (or switching it elsewhere) is reflected without
// requiring a restart, and picks up edits made to the config file in
// the meantime. switchOutput/switchTo call syncDeviceMenu directly right
// after switching, so a switch is never left waiting on this interval.
func (a *app) watchDeviceChanges() {
	interval := a.currentPollInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		a.reloadConfigIfChanged()
		a.syncDeviceMenu()

		if newInterval := a.currentPollInterval(); newInterval != interval {
			interval = newInterval
			ticker.Reset(interval)
		}
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

	cfg := a.devices()
	if hasNewDevice(devices, cfg) {
		if merged, err := a.syncConfig(deviceNames(devices)); err != nil {
			log.Printf("auto-add new device(s) to config: %v", err)
		} else {
			cfg = merged
		}
	}

	systray.SetTooltip(a.tooltip(outputconfig.DisplayName(cfg, current.Name)))

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
	a.switchMu.Lock()
	defer a.switchMu.Unlock()

	result := a.worker.Next(a.skipSet())
	switch {
	case result.Err != nil:
		log.Printf("switch output: %v", result.Err)
		osd.Show("Could not switch output: " + result.Err.Error())
	case !result.Switched:
		osd.Show("Only one output available: " + a.displayName(result.Device.Name))
	default:
		osd.Show(a.displayName(result.Device.Name))
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
		osd.Show(a.displayName(result.Device.Name))
	}
	a.syncDeviceMenu()
}

func (a *app) tooltip(currentName string) string {
	if currentName == "" {
		currentName = "No active device"
	}
	return fmt.Sprintf("Audio Output Switcher %s — %s", version, currentName)
}
