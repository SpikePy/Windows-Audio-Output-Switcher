//go:build windows

// Package updater implements what internal/install needs from the outside
// world: locating the Startup folder entry, finding the latest GitHub
// release, and downloading its assets.
package updater

import (
	"os"
	"path/filepath"

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

	// ExeName is the fixed filename used in the Startup folder. Always
	// using the same name is what guarantees there is ever only one
	// autostart entry, even across updates.
	ExeName = "AudioOutputSwitcher.exe"

	versionFileName = "version"

	// legacyVersionFileName is where the marker used to be written: into
	// the Startup folder itself, which is no place for a data file.
	legacyVersionFileName = ".audiooutputswitcher_version"
)

// StartupDir returns the current user's Startup folder; anything placed
// there is launched automatically at login.
func StartupDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

// InstalledExePath is where the switcher binary lives once installed.
func InstalledExePath() string {
	return filepath.Join(StartupDir(), ExeName)
}

// VersionFilePath stores the tag name of the currently installed release,
// alongside the app's config rather than in the Startup folder.
func VersionFilePath() string {
	return filepath.Join(outputconfig.Dir(), versionFileName)
}

// LegacyVersionFilePath is where that marker used to live; install and
// uninstall delete it so it doesn't linger in the Startup folder.
func LegacyVersionFilePath() string {
	return filepath.Join(StartupDir(), legacyVersionFileName)
}
