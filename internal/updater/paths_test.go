//go:build windows

package updater

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFilePathLivesWithTheConfig(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)

	got := VersionFilePath()
	if want := `C:\Users\test\AppData\Roaming\AudioOutputSwitcher\version`; got != want {
		t.Errorf("VersionFilePath() = %q, want %q", got, want)
	}
	if strings.Contains(got, "Startup") {
		t.Errorf("VersionFilePath() = %q, want it out of the Startup folder", got)
	}
}

func TestLegacyVersionFilePathIsTheOldStartupMarker(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)

	if got, want := LegacyVersionFilePath(), filepath.Join(StartupDir(), ".audiooutputswitcher_version"); got != want {
		t.Errorf("LegacyVersionFilePath() = %q, want %q", got, want)
	}
}
