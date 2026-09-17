//go:build windows

// Package install implements installing/updating and uninstalling Audio
// Output Switcher, shared by cmd/setup. Kept separate from cmd/setup so
// the logic itself - the part worth getting right - isn't entangled
// with the window around it.
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

// Progress receives a one-line status message whenever Install or
// Uninstall moves on to the next step, or has something to warn about.
type Progress func(msg string)

// Install downloads the latest release and, if it differs from the
// installed copy in %LOCALAPPDATA%\AudioOutputSwitcher, puts it there
// under a fixed name and (re)starts it, then brings the Startup folder
// shortcut in line with the config file's autostart setting. Calling it
// again later updates in place: the fixed filename guarantees there is
// always exactly one installed copy, and comparing the download against
// the installed copy - rather than recording the installed version in a
// file somewhere - is what decides whether anything needs replacing.
// Older installs, which lived in the Startup folder or in %APPDATA%, are
// moved over along the way.
func Install(progress Progress) error {
	progress("Checking for the latest release...")

	tag, err := updater.LatestTag()
	if err != nil {
		return err
	}

	exePath := updater.InstalledExePath()
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		return fmt.Errorf("create install folder: %w", err)
	}

	progress(fmt.Sprintf("Downloading %s...", tag))
	downloaded := exePath + ".new"
	if err := updater.Download(updater.LatestAssetURL(updater.AssetName), downloaded); err != nil {
		return fmt.Errorf("download %s: %w", updater.AssetName, err)
	}
	defer os.Remove(downloaded) // a no-op once it has been renamed into place

	same, err := sameContents(downloaded, exePath)
	if err != nil {
		return fmt.Errorf("compare with the installed copy: %w", err)
	}
	if same {
		migrate(progress)
		if !updater.IsRunning() {
			progress("Starting Audio Output Switcher...")
			_ = start(exePath)
		}
		progress(fmt.Sprintf("%s is already installed. %s", tag, applyAutostart(exePath, progress)))
		return nil
	}

	progress(fmt.Sprintf("Installing %s...", tag))
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
	// Only now that every running copy is stopped can an old install's
	// files be removed.
	migrate(progress)

	progress("Starting Audio Output Switcher...")
	if err := start(exePath); err != nil {
		return fmt.Errorf("start %s: %w", exePath, err)
	}

	progress(fmt.Sprintf("%s is installed and running. %s", tag, applyAutostart(exePath, progress)))
	return nil
}

// start launches the installed app from its own folder. Otherwise it
// would inherit setup's working directory - typically Downloads - and
// keep that folder in use for as long as it runs.
func start(exePath string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	return cmd.Start()
}

// applyAutostart creates or removes the Startup folder shortcut to exePath
// as the config file's autostart setting says, and describes the result
// in a sentence. A config file that doesn't parse leaves the shortcut as
// it is, since there's no telling what the user meant it to say.
func applyAutostart(exePath string, progress Progress) string {
	cfg, err := outputconfig.Load()
	if err != nil {
		progress(fmt.Sprintf("Warning: %s is invalid, leaving autostart as it is: %v", outputconfig.Path(), err))
		return "Autostart was left unchanged."
	}
	enabled := cfg.EffectiveAutostart()
	if err := autostart.Set(enabled, exePath); err != nil {
		progress(fmt.Sprintf("Warning: %v", err))
		return "Autostart could not be updated."
	}
	if enabled {
		return "It starts automatically at login."
	}
	return "Autostart is off in the config file."
}

// migrate moves an older install's config file to the current folder and
// deletes the rest of it: the exe versions up to v1.0.5 kept in the
// Startup folder (with its update leftovers and a version marker), and
// the %APPDATA% folder v1.0.6 used. Their process must already be
// stopped. The old folder is only removed once its config file is safe.
func migrate(progress Progress) {
	if err := outputconfig.MigrateLegacy(); err != nil {
		progress(fmt.Sprintf("Warning: could not move the config file from %s: %v", outputconfig.LegacyDir(), err))
		removeLegacyExes(progress)
		return
	}
	removeLegacyExes(progress)
	removeAll(outputconfig.LegacyDir(), progress)
}

func removeLegacyExes(progress Progress) {
	for _, legacy := range updater.LegacyExePaths() {
		for _, path := range []string{legacy, legacy + ".old", legacy + ".new"} {
			removeAll(path, progress)
		}
	}
	removeAll(updater.LegacyVersionFilePath(), progress)
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
// its config, and the leftovers of older versions (the exe in the Startup
// folder or %APPDATA%, a version marker, a Start Menu shortcut, a log
// file in %TEMP%) - leaving no trace behind. It does not touch the setup
// tool itself; the caller is responsible for that (see cmd/setup, which
// self-deletes after a successful uninstall).
func Uninstall(progress Progress) error {
	progress("Stopping Audio Output Switcher...")
	updater.KillRunning()
	time.Sleep(500 * time.Millisecond)

	progress("Removing files...")
	removeAll(autostart.ShortcutPath(), progress)
	removeLegacyExes(progress)
	removeAll(legacyShortcutPath(), progress)
	removeAll(filepath.Join(os.TempDir(), "AudioOutputSwitcher.log"), progress)
	// Everything else - the exe (and its .old/.new update leftovers),
	// config.yaml (devices.yaml/outputs.yaml in older versions), an
	// opt-in log file and, up to v0.9.3, config.json - lives in one of
	// these two folders, so removing them wholesale covers every
	// version's files in one go.
	removeAll(outputconfig.LegacyDir(), progress)
	removeAll(outputconfig.Dir(), progress)

	progress("Audio Output Switcher has been removed.")
	return nil
}

// legacyShortcutPath is the Start Menu shortcut v0.9.11-v0.9.12 created to
// register an AppUserModelID for toast notifications; nothing creates it
// anymore, but Uninstall still removes any leftover.
func legacyShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Audio Output Switcher.lnk")
}

func removeAll(path string, progress Progress) {
	if path == "" {
		return
	}
	if err := removeWithRetry(path); err != nil {
		progress(fmt.Sprintf("Warning: could not remove %s: %v", path, err))
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
