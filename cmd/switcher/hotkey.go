//go:build windows

package main

import (
	"fmt"
	"log"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/llhotkey"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/osd"
)

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

func (a *app) currentHotkey() string {
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	return a.hotkeyCombo
}
