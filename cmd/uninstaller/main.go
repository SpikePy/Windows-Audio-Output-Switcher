// Command uninstaller stops Audio Output Switcher, removes its Startup
// folder entry and configuration, and then deletes itself, leaving no
// trace behind.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/appstate"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/updater"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

func main() {
	fmt.Println("Audio Output Switcher uninstaller", version)

	fmt.Println("Stopping Audio Output Switcher...")
	updater.KillRunning()
	time.Sleep(500 * time.Millisecond)

	removeAll(updater.InstalledExePath())
	removeAll(updater.InstalledExePath() + ".old")
	removeAll(updater.VersionFilePath())
	removeAll(appstate.Dir())

	fmt.Println("Audio Output Switcher has been removed.")
	selfDelete()
}

func removeAll(path string) {
	if path == "" {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		fmt.Printf("warning: could not remove %s: %v\n", path, err)
	}
}

// selfDelete spawns a detached helper that waits for this process to
// fully exit and release its executable file, then deletes it - so
// running the uninstaller leaves nothing behind, including itself.
func selfDelete() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return
	}
	script := fmt.Sprintf(`timeout /T 1 /NOBREAK >NUL & del /F /Q "%s"`, self)
	_ = exec.Command("cmd", "/C", script).Start()
}
