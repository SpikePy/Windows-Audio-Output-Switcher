//go:build windows

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
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/install"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

// autoChoice is what running setup with no input at all - e.g.
// double-clicked and then left alone - does after autoChoiceDelay: the
// common case (install/update) rather than doing nothing.
const (
	autoChoice      = "1"
	autoChoiceDelay = 5 * time.Second
)

// exitDelay is how long the final "done" screen waits for a keypress
// before closing on its own, so a fully automated run (auto-chosen
// install included) doesn't need any interaction at all to finish.
const exitDelay = 3 * time.Second

func main() {
	fmt.Println("Audio Output Switcher setup", version)
	fmt.Println()
	fmt.Println("1) Install or update Audio Output Switcher")
	fmt.Println("2) Uninstall Audio Output Switcher")
	fmt.Println()

	uninstalled := false
	label := fmt.Sprintf("Choose an option (1 or 2) - installing/updating automatically in %s if you don't: ", autoChoiceDelay)
	switch promptWithDefault(label, autoChoice, autoChoiceDelay) {
	case "1":
		if err := install.Install(); err != nil {
			fmt.Fprintln(os.Stderr, "install failed:", err)
		}
	case "2":
		if err := install.Uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall failed:", err)
		} else {
			uninstalled = true
		}
	default:
		fmt.Println("Cancelled.")
		return
	}

	waitOrTimeout(fmt.Sprintf("Press Enter to exit (closing automatically in %s)...", exitDelay), exitDelay)

	if uninstalled {
		// Uninstall doesn't remove this exe itself - do that here, same
		// as the old dedicated uninstaller did, so running setup leaves
		// nothing behind once you've chosen to uninstall. Done last, so
		// the file isn't gone out from under a user re-reading the
		// output above before dismissing the prompt.
		selfDelete()
	}
}

// readLineWithTimeout prints label, then reads one line of input,
// trimmed of surrounding whitespace. If nothing arrives within timeout
// it gives up and reports ok = false instead of waiting forever - this
// is a console app launched by double-clicking in Explorer as often as
// from a terminal, and there's nobody at the keyboard to finish a
// prompt in that case.
func readLineWithTimeout(label string, timeout time.Duration) (line string, ok bool) {
	fmt.Print(label)

	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		lines <- strings.TrimSpace(line)
	}()

	select {
	case line := <-lines:
		return line, true
	case <-time.After(timeout):
		return "", false
	}
}

// promptWithDefault reads one line via readLineWithTimeout, returning
// def in its place on timeout - e.g. setup double-clicked and left
// alone defaults to installing/updating rather than sitting there.
func promptWithDefault(label, def string, timeout time.Duration) string {
	line, ok := readLineWithTimeout(label, timeout)
	if !ok {
		fmt.Println(def)
		return def
	}
	return line
}

// waitOrTimeout prints label and blocks until either a keypress or
// timeout, whichever comes first - used for the final "done" pause so
// a fully unattended run still closes on its own.
func waitOrTimeout(label string, timeout time.Duration) {
	if _, ok := readLineWithTimeout(label, timeout); !ok {
		fmt.Println()
	}
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
