// Command installer downloads the latest Audio Output Switcher release,
// places it in the current user's Startup folder under a fixed name, and
// (re)starts it. Running it again later updates in place: the fixed
// filename guarantees there is always exactly one autostart entry, and
// the version check guarantees the newest release is the one running.
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/updater"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "install failed:", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("Audio Output Switcher installer", version)
	fmt.Println("Checking for the latest release...")

	release, err := updater.LatestRelease()
	if err != nil {
		return err
	}

	asset, ok := release.FindAsset(updater.AssetName)
	if !ok {
		return fmt.Errorf("release %s has no asset named %s", release.TagName, updater.AssetName)
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

	if err := updater.Download(asset.BrowserDownloadURL, exePath); err != nil {
		_ = os.Rename(oldPath, exePath) // best-effort rollback
		return fmt.Errorf("download %s: %w", asset.Name, err)
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
