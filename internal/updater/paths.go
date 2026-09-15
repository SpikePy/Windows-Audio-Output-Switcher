//go:build windows

// Package updater implements what internal/install needs from the outside
// world: locating the Startup folder entry, finding the latest GitHub
// release, and downloading its assets.
package updater

import (
	"os"
	"path/filepath"
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

	versionFileName = ".audiooutputswitcher_version"
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

// VersionFilePath stores the tag name of the currently installed release.
func VersionFilePath() string {
	return filepath.Join(StartupDir(), versionFileName)
}
