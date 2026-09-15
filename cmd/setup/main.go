// Command setup is the single entry point for installing, updating, and
// uninstalling Audio Output Switcher - it replaces what used to be two
// separate Install_/Uninstall_ executables with one that asks which of
// the two you want.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/install"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

func main() {
	fmt.Println("Audio Output Switcher setup", version)
	fmt.Println()
	fmt.Println("1) Install or update Audio Output Switcher")
	fmt.Println("2) Uninstall Audio Output Switcher")
	fmt.Println()

	uninstalled := false
	switch prompt("Choose an option (1 or 2), or press Enter to cancel: ") {
	case "1":
		if err := install.Install(version); err != nil {
			fmt.Fprintln(os.Stderr, "install failed:", err)
		}
	case "2":
		if err := install.Uninstall(version); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall failed:", err)
		} else {
			uninstalled = true
		}
	default:
		fmt.Println("Cancelled.")
		return
	}

	prompt("Press Enter to exit...")

	if uninstalled {
		// Uninstall doesn't remove this exe itself - do that here, same
		// as the old dedicated uninstaller did, so running setup leaves
		// nothing behind once you've chosen to uninstall. Done last, so
		// the file isn't gone out from under a user re-reading the
		// output above before dismissing the prompt.
		selfDelete()
	}
}

// prompt prints label, reads one line of input, and returns it with
// surrounding whitespace trimmed. This is a console app launched by
// double-clicking in Explorer as often as from a terminal, so every
// path through main waits for a keypress before exiting - otherwise the
// window would just flash and close before showing its result.
func prompt(label string) string {
	fmt.Print(label)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

// selfDelete spawns a detached helper that waits for this process to
// fully exit and release its executable file, then deletes it.
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
