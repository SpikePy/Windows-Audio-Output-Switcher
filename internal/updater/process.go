//go:build windows

package updater

import (
	"os/exec"
	"strings"
)

// KillRunning terminates any running instance of the switcher so its exe
// file can be safely replaced. It's a no-op (ignoring the error) if
// nothing was running.
func KillRunning() {
	_ = exec.Command("taskkill", "/F", "/IM", ExeName, "/T").Run()
}

// IsRunning reports whether an instance of the switcher is currently
// running.
func IsRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+ExeName, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(ExeName))
}
