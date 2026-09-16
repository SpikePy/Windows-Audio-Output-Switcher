//go:build windows

package updater

import (
	"path/filepath"
	"testing"
)

func TestLegacyVersionFilePathIsTheOldStartupMarker(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)

	if got, want := LegacyVersionFilePath(), filepath.Join(StartupDir(), ".audiooutputswitcher_version"); got != want {
		t.Errorf("LegacyVersionFilePath() = %q, want %q", got, want)
	}
}
