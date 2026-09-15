// Package install implements installing/updating and uninstalling Audio
// Output Switcher, shared by cmd/setup. Kept separate from cmd/setup so
// the logic itself - the part worth getting right - isn't entangled
// with the interactive menu around it.
package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/aumid"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/updater"
)

// Install downloads the latest release (unless the installed one is
// already current) into the current user's Startup folder under a fixed
// name, and (re)starts it. Calling it again later updates in place: the
// fixed filename guarantees there is always exactly one autostart entry,
// and the version check guarantees the newest release is the one
// running. version is this setup tool's own version, only used in
// progress messages.
func Install(version string) error {
	fmt.Println("Audio Output Switcher setup", version)
	fmt.Println("Checking for the latest release...")

	release, err := updater.LatestRelease()
	if err != nil {
		return err
	}

	exePath := updater.InstalledExePath()
	_, statErr := os.Stat(exePath)
	exeExists := statErr == nil

	currentVersion, _ := os.ReadFile(updater.VersionFilePath())
	needsUpdate := !exeExists || string(currentVersion) != release.TagName

	if !needsUpdate {
		fmt.Printf("Already up to date (%s).\n", release.TagName)
		if !updater.IsRunning() {
			fmt.Println("Starting Audio Output Switcher...")
			_ = exec.Command(exePath).Start()
		}
		return nil
	}

	fmt.Printf("Installing %s...\n", release.TagName)
	updater.KillRunning()

	if err := os.MkdirAll(updater.StartupDir(), 0o755); err != nil {
		return fmt.Errorf("create startup folder: %w", err)
	}

	// A running exe can still be renamed out of the way on Windows even
	// though it can't be overwritten directly. Always deploying under
	// the same fixed name is what keeps there from ever being more than
	// one Startup folder entry.
	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	_ = os.Rename(exePath, oldPath)

	downloadURL := updater.AssetDownloadURL(release.TagName, updater.AssetName)
	if err := updater.Download(downloadURL, exePath); err != nil {
		_ = os.Rename(oldPath, exePath) // best-effort rollback
		return fmt.Errorf("download %s: %w", updater.AssetName, err)
	}
	_ = os.Remove(oldPath)

	if err := os.WriteFile(updater.VersionFilePath(), []byte(release.TagName), 0o644); err != nil {
		return fmt.Errorf("write version marker: %w", err)
	}

	fmt.Println("Starting Audio Output Switcher...")
	if err := exec.Command(exePath).Start(); err != nil {
		return fmt.Errorf("start %s: %w", exePath, err)
	}

	fmt.Printf("Done. Installed %s to %s.\nIt will now start automatically at login.\n", release.TagName, exePath)
	return nil
}

// Uninstall stops Audio Output Switcher and removes everything Install
// set up - the Startup folder entry, its saved device config, and a
// legacy Start Menu shortcut from versions old enough to have created
// one - leaving no trace behind. It does not touch the setup tool
// itself; the caller is responsible for that (see cmd/setup, which
// self-deletes after a successful uninstall).
func Uninstall(version string) error {
	fmt.Println("Audio Output Switcher setup", version)

	fmt.Println("Stopping Audio Output Switcher...")
	updater.KillRunning()
	time.Sleep(500 * time.Millisecond)

	removeAll(updater.InstalledExePath())
	removeAll(updater.InstalledExePath() + ".old")
	removeAll(updater.VersionFilePath())
	removeAll(aumid.ShortcutPath())
	// Everything under here - devices.yaml/outputs.yaml, and (up to
	// v0.9.3) config.json - lives in this one directory, so removing it
	// wholesale covers every version's settings file in one go.
	removeAll(filepath.Join(os.Getenv("APPDATA"), "AudioOutputSwitcher"))

	fmt.Println("Audio Output Switcher has been removed.")
	return nil
}

func removeAll(path string) {
	if path == "" {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		fmt.Printf("warning: could not remove %s: %v\n", path, err)
	}
}
