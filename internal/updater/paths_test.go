//go:build windows

package updater

import (
	"path/filepath"
	"testing"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/autostart"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

func TestInstalledExeLivesNextToTheConfig(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)

	if got, want := InstalledExePath(), filepath.Join(filepath.Dir(outputconfig.Path()), ExeName); got != want {
		t.Errorf("InstalledExePath() = %q, want %q", got, want)
	}
	for _, legacy := range LegacyExePaths() {
		if legacy == InstalledExePath() {
			t.Errorf("LegacyExePaths() includes the current install path %q", legacy)
		}
	}
}

func TestLegacyVersionFilePathIsTheOldStartupMarker(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)

	if got, want := LegacyVersionFilePath(), filepath.Join(autostart.StartupDir(), ".audiooutputswitcher_version"); got != want {
		t.Errorf("LegacyVersionFilePath() = %q, want %q", got, want)
	}
}
