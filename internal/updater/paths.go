//go:build windows

// Package updater implements what internal/install needs from the outside
// world: locating the installed exe, finding the latest GitHub
// release, and downloading its assets.
package updater

import (
	"path/filepath"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/autostart"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
)

const (
	// Owner and Repo identify the GitHub repository releases are
	// fetched from.
	Owner = "SpikePy"
	Repo  = "Windows-Audio-Output-Switcher"

	// AssetName is the exact release asset name the CI build publishes
	// (see .github/workflows/release.yml) and that the installer looks
	// for on the latest release.
	AssetName = "AudioOutputSwitcher.exe"

	// ExeName is the fixed filename the switcher is installed under.
	// Always using the same name is what guarantees there is ever only
	// one installed copy, even across updates.
	ExeName = "AudioOutputSwitcher.exe"

	// legacyVersionFileName is a marker versions up to v1.0.4 wrote next
	// to the installed exe to remember what they had installed. Nothing
	// writes it anymore - install compares the downloaded release with
	// the installed copy instead - so it's only ever deleted.
	legacyVersionFileName = ".audiooutputswitcher_version"
)

// InstalledExePath is where the switcher binary lives once installed:
// next to its config file, in %APPDATA%\AudioOutputSwitcher. The Startup
// folder only ever holds a shortcut to it (see internal/autostart).
func InstalledExePath() string {
	return filepath.Join(outputconfig.Dir(), ExeName)
}

// LegacyExePath is where versions up to v1.0.5 installed the exe itself:
// straight into the Startup folder. Install and uninstall remove it (and
// its .old/.new siblings) so it doesn't linger there.
func LegacyExePath() string {
	return filepath.Join(autostart.StartupDir(), ExeName)
}

// LegacyVersionFilePath is that leftover marker, which install and
// uninstall delete so it doesn't linger in the Startup folder.
func LegacyVersionFilePath() string {
	return filepath.Join(autostart.StartupDir(), legacyVersionFileName)
}
