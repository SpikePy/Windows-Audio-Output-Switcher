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
// next to its config file, in %LOCALAPPDATA%\AudioOutputSwitcher. The Startup
// folder only ever holds a shortcut to it (see internal/autostart).
func InstalledExePath() string {
	return filepath.Join(outputconfig.Dir(), ExeName)
}

// LegacyExePaths are where older versions installed the exe: straight
// into the Startup folder up to v1.0.5, and into %APPDATA% in v1.0.6.
// Install and uninstall remove them (and their .old/.new siblings) so
// they don't linger.
func LegacyExePaths() []string {
	return []string{
		filepath.Join(autostart.StartupDir(), ExeName),
		filepath.Join(outputconfig.LegacyDir(), ExeName),
	}
}

// LegacyVersionFilePath is that leftover marker, which install and
// uninstall delete so it doesn't linger in the Startup folder.
func LegacyVersionFilePath() string {
	return filepath.Join(autostart.StartupDir(), legacyVersionFileName)
}
