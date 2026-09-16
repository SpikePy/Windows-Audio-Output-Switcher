//go:build windows

// Package install implements installing/updating and uninstalling Audio
// Output Switcher, shared by cmd/setup. Kept separate from cmd/setup so
// the logic itself - the part worth getting right - isn't entangled
// with the interactive menu around it.
package install

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/autostart"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/outputconfig"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/updater"
)

// How long removals keep trying, which only matters for files a process
// that was just killed hasn't let go of yet - two seconds in total.
const (
	removeAttempts   = 10
	removeRetryDelay = 200 * time.Millisecond
)

// Install downloads the latest release and, if it differs from the
// installed copy in %APPDATA%\AudioOutputSwitcher, puts it there under a
// fixed name and (re)starts it, then brings the Startup folder shortcut in
// line with the config file's autostart setting. Calling it again later
// updates in place: the fixed filename guarantees there is always exactly
// one installed copy, and comparing the download against the installed copy - rather than
// recording the installed version in a file somewhere - is what decides
// whether anything needs replacing.
func Install() error {
	fmt.Println("Checking for the latest release...")

	release, err := updater.LatestRelease()
	if err != nil {
		return err
	}

	exePath := updater.InstalledExePath()
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		return fmt.Errorf("create install folder: %w", err)
	}

	fmt.Printf("Downloading %s...\n", release.TagName)
	downloaded := exePath + ".new"
	if err := updater.Download(updater.AssetDownloadURL(release.TagName, updater.AssetName), downloaded); err != nil {
		return fmt.Errorf("download %s: %w", updater.AssetName, err)
	}
	defer os.Remove(downloaded) // a no-op once it has been renamed into place

	same, err := sameContents(downloaded, exePath)
	if err != nil {
		return fmt.Errorf("compare with the installed copy: %w", err)
	}
	if same {
		fmt.Printf("Already up to date (%s).\n", release.TagName)
		if !updater.IsRunning() {
			fmt.Println("Starting Audio Output Switcher...")
			_ = exec.Command(exePath).Start()
		}
		applyAutostart(exePath)
		return nil
	}

	fmt.Printf("Installing %s...\n", release.TagName)
	updater.KillRunning()

	// A running exe can still be renamed out of the way on Windows even
	// though it can't be overwritten directly. Always deploying under
	// the same fixed name is what keeps there from ever being more than
	// one installed copy.
	oldPath := exePath + ".old"
	_ = removeWithRetry(oldPath)
	_ = os.Rename(exePath, oldPath)
	if err := os.Rename(downloaded, exePath); err != nil {
		_ = os.Rename(oldPath, exePath) // best-effort rollback
		return fmt.Errorf("replace %s: %w", exePath, err)
	}
	_ = removeWithRetry(oldPath)
	removeLegacyInstall()

	fmt.Println("Starting Audio Output Switcher...")
	if err := exec.Command(exePath).Start(); err != nil {
		return fmt.Errorf("start %s: %w", exePath, err)
	}

	fmt.Printf("Done. Installed %s to %s.\n", release.TagName, exePath)
	applyAutostart(exePath)
	return nil
}

// applyAutostart creates or removes the Startup folder shortcut to exePath
// as the config file's autostart setting says, reporting what it did. A
// config file that doesn't parse leaves the shortcut as it is, since
// there's no telling what the user meant it to say.
func applyAutostart(exePath string) {
	cfg, err := outputconfig.Load()
	if err != nil {
		fmt.Printf("warning: %s is invalid, leaving autostart as it is: %v\n", outputconfig.Path(), err)
		return
	}
	enabled := cfg.EffectiveAutostart()
	if err := autostart.Set(enabled, exePath); err != nil {
		fmt.Printf("warning: %v\n", err)
		return
	}
	if enabled {
		fmt.Println("It will start automatically at login.")
	} else {
		fmt.Println("Autostart is off in the config file, so it won't start at login.")
	}
}

// removeLegacyInstall deletes what versions up to v1.0.5 kept in the
// Startup folder - the exe itself, its update leftovers and a version
// marker - now that the exe lives next to the config and Startup only
// holds a shortcut. Their process must already be stopped.
func removeLegacyInstall() {
	legacy := updater.LegacyExePath()
	for _, path := range []string{legacy, legacy + ".old", legacy + ".new", updater.LegacyVersionFilePath()} {
		removeAll(path)
	}
}

// sameContents reports whether both files hold exactly the same bytes. A
// missing second file counts as different rather than an error, since
// that's just a first install.
func sameContents(a, b string) (bool, error) {
	statA, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	statB, err := os.Stat(b)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if statA.Size() != statB.Size() {
		return false, nil
	}

	fileA, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fileA.Close()
	fileB, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fileB.Close()

	bufA := make([]byte, 64*1024)
	bufB := make([]byte, 64*1024)
	for {
		nA, errA := io.ReadFull(fileA, bufA)
		nB, errB := io.ReadFull(fileB, bufB)
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false, nil
		}
		// The sizes match, so both files run out at the same point.
		if errA == io.EOF || errA == io.ErrUnexpectedEOF {
			return true, nil
		}
		if errA != nil {
			return false, errA
		}
		if errB != nil {
			return false, errB
		}
	}
}

// Uninstall stops Audio Output Switcher and removes everything Install
// set up - the Startup folder shortcut, the app's folder with the exe and
// its config, and the leftovers of older versions (the exe and a version
// marker in the Startup folder, a Start Menu shortcut) - leaving no trace
// behind. It does not touch the setup tool
// itself; the caller is responsible for that (see cmd/setup, which
// self-deletes after a successful uninstall).
func Uninstall() error {
	fmt.Println("Stopping Audio Output Switcher...")
	updater.KillRunning()
	time.Sleep(500 * time.Millisecond)

	removeAll(autostart.ShortcutPath())
	removeLegacyInstall()
	removeAll(legacyShortcutPath())
	// Everything under here - the exe (and its .old/.new update
	// leftovers), config.yaml (devices.yaml/outputs.yaml in older
	// versions) and, up to v0.9.3, config.json - lives in this one
	// directory, so removing it wholesale covers every version's files in
	// one go.
	removeAll(outputconfig.Dir())

	fmt.Println("Audio Output Switcher has been removed.")
	return nil
}

// legacyShortcutPath is the Start Menu shortcut v0.9.11-v0.9.12 created to
// register an AppUserModelID for toast notifications; nothing creates it
// anymore, but Uninstall still removes any leftover.
func legacyShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Audio Output Switcher.lnk")
}

func removeAll(path string) {
	if path == "" {
		return
	}
	if err := removeWithRetry(path); err != nil {
		fmt.Printf("warning: could not remove %s: %v\n", path, err)
	}
}

// removeWithRetry deletes a file or directory, retrying briefly while
// Windows refuses. A process that was just killed holds on to its exe
// for a moment longer, and deleting it in that window fails - which
// would leave a copy of the previous version sitting next to the
// installed one until the next update.
func removeWithRetry(path string) error {
	var err error
	for attempt := 0; attempt < removeAttempts; attempt++ {
		if err = os.RemoveAll(path); err == nil {
			return nil
		}
		time.Sleep(removeRetryDelay)
	}
	return err
}
