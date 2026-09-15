// Command switcher runs in the system tray and cycles the default Windows
// playback device on a global hotkey press.
package main

import (
	"fmt"
	"log"

	"fyne.io/systray"
	"golang.design/x/hotkey"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/appstate"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/notifier"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

type app struct {
	cfg     appstate.Config
	enabled bool

	worker *audio.Worker
	hk     *hotkey.Hotkey

	mEnable  *systray.MenuItem
	mDisable *systray.MenuItem
	mExit    *systray.MenuItem
}

func main() {
	cfg, err := appstate.Load()
	if err != nil {
		log.Printf("loading config: %v", err)
		cfg = appstate.Config{Hotkey: appstate.DefaultHotkey, Enabled: true}
	}

	a := &app{cfg: cfg, enabled: cfg.Enabled}
	systray.Run(a.onReady, a.onExit)
}

func (a *app) onReady() {
	systray.SetIcon(a.iconBytes())
	systray.SetTooltip(a.tooltip())

	a.mEnable = systray.AddMenuItem("Enable", "Enable audio output switching")
	a.mDisable = systray.AddMenuItem("Disable", "Disable audio output switching")
	systray.AddSeparator()
	a.mExit = systray.AddMenuItem("Exit", "Quit Audio Output Switcher")
	a.updateMenuState()

	// Left click toggles enabled/disabled; right click falls back to the
	// default context menu built above.
	systray.SetOnTapped(a.toggleEnabled)

	a.worker = audio.StartWorker()
	a.registerHotkey()

	go a.watchMenu()
}

func (a *app) onExit() {
	if a.hk != nil {
		_ = a.hk.Unregister()
	}
	if a.worker != nil {
		a.worker.Stop()
	}
}

func (a *app) registerHotkey() {
	mods, key, err := hotkeycfg.Parse(a.cfg.Hotkey)
	if err != nil {
		log.Printf("invalid hotkey %q: %v", a.cfg.Hotkey, err)
		mods, key, _ = hotkeycfg.Parse(appstate.DefaultHotkey)
	}

	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		log.Printf("failed to register hotkey %q: %v", a.cfg.Hotkey, err)
		_ = notifier.Show("Audio Output Switcher",
			fmt.Sprintf("Could not register the switch hotkey (%s). Another app might already be using it.", a.cfg.Hotkey))
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
}

func (a *app) toggleEnabled() {
	a.setEnabled(!a.enabled)
}

func (a *app) setEnabled(enabled bool) {
	if a.enabled == enabled {
		return
	}
	a.enabled = enabled
	a.cfg.Enabled = enabled
	if err := a.cfg.Save(); err != nil {
		log.Printf("saving config: %v", err)
	}

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
	return fmt.Sprintf("Audio Output Switcher %s — %s\nHotkey: %s", version, state, a.cfg.Hotkey)
}
