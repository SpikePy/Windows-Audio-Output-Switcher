//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetCreatesReplacesAndRemovesTheShortcut(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	target := filepath.Join(t.TempDir(), "AudioOutputSwitcher.exe")

	for i := 0; i < 2; i++ { // the second round replaces the first shortcut
		if err := Set(true, target); err != nil {
			t.Fatalf("Set(true) #%d: %v", i+1, err)
		}
	}
	entries, err := os.ReadDir(StartupDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != shortcutName {
		t.Fatalf("Startup folder holds %v, want just %s", entries, shortcutName)
	}

	for i := 0; i < 2; i++ { // removing a missing shortcut is fine too
		if err := Set(false, target); err != nil {
			t.Fatalf("Set(false) #%d: %v", i+1, err)
		}
	}
	if _, err := os.Stat(ShortcutPath()); !os.IsNotExist(err) {
		t.Errorf("shortcut still there after Set(false): %v", err)
	}
}
