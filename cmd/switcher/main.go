//go:build windows

// Command switcher runs in the system tray and cycles the default Windows
// playback device on a global hotkey press.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/autostart"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/llhotkey"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/updater"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

type app struct {
	worker *audio.Worker

	// hk and hotkeyCombo are the currently-registered global hotkey and
	// the combo string (see internal/hotkeycfg) it was built from -
	// outputconfig.DefaultHotkey ("win+a") if there's no config file yet.
	// hk is nil, and hotkeyCombo empty or "disabled", when the config file
	// switched the hotkey off. Always changed together, via applyHotkey or
	// disableHotkey.
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
	lastRefresh atomic.Int64 // UnixNano when syncDeviceMenu last started - see watchDeviceChanges

	// cfgMu guards everything loaded from the config file. cfg (keyed by
	// device ID) is always replaced wholesale, never mutated, so a map read
	// under cfgMu stays safe to use after unlocking.
	cfg           map[string]outputconfig.Entry
	autostart     bool          // whether the Startup folder shortcut should exist
	pollInterval  time.Duration // how often to re-check the config file (and devices, as a fallback)
	configModTime time.Time     // file mtime as of the last read or write - see reloadConfigIfChanged
	configErr     error         // non-nil while the file on disk fails to parse
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

	loaded, err := outputconfig.Load()
	if err != nil {
		log.Printf("config file is invalid, using defaults until it's fixed: %v", err)
	}
	a := &app{
		cfg:           loaded.Devices,
		configModTime: outputconfig.ModTime(),
		configErr:     err,
		hotkeyCombo:   loaded.EffectiveHotkey(),
		autostart:     loaded.EffectiveAutostart(),
		pollInterval:  loaded.EffectivePollInterval(),
	}
	if err == nil {
		applyAutostart(a.autostart)
	}
	systray.Run(a.onReady, a.onExit)
}

// applyAutostart creates or removes the Startup folder shortcut as the
// config file says, so editing autostart takes effect without re-running
// setup. Only the installed copy does this - a build run from anywhere
// else must not point the shortcut at itself.
func applyAutostart(enabled bool) {
	self, err := os.Executable()
	if err != nil {
		log.Printf("autostart: locate own exe: %v", err)
		return
	}
	installed := updater.InstalledExePath()
	if !strings.EqualFold(filepath.Clean(self), filepath.Clean(installed)) {
		log.Printf("autostart: not the installed copy (%s), leaving the Startup folder alone", self)
		return
	}
	if err := autostart.Set(enabled, installed); err != nil {
		log.Printf("autostart: %v", err)
	}
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
	if a.configError() != nil {
		osd.Show("Config file has an error - using defaults until it's fixed")
	}
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

func (a *app) tooltip(currentName string) string {
	if currentName == "" {
		currentName = "No active device"
	}
	return fmt.Sprintf("Audio Output Switcher %s — %s", version, currentName)
}
