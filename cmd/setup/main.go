//go:build windows

// Command setup is the single entry point for installing, updating, and
// uninstalling Audio Output Switcher. Double-clicked, it shows a small
// window asking which of the two you want (see window.go); with
// -mode install|uninstall it does that straight away, for scripting.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/install"
)

// version is set via -ldflags "-X main.version=..." during the release
// build; it stays "dev" for local builds.
var version = "dev"

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS

func main() {
	// Setup is built as a GUI program, so it has no console of its own;
	// anything it prints on the command line goes to the one it was
	// started from.
	if len(os.Args) > 1 {
		attachConsole()
	}
	mode := flag.String("mode", "", "install or uninstall straight away, without showing the window")
	flag.Parse()

	switch *mode {
	case "":
		if runWindow() {
			// Uninstall doesn't remove this exe itself - do that last,
			// once the window is gone, so running setup leaves nothing
			// behind once you've chosen to uninstall.
			selfDelete()
		}
	case "install", "uninstall":
		os.Exit(runScripted(*mode))
	default:
		fmt.Fprintf(os.Stderr, "unknown -mode %q, want install or uninstall\n", *mode)
		os.Exit(2)
	}
}

// runScripted runs one action with its progress on the console, and
// returns the process exit code.
func runScripted(mode string) int {
	fmt.Println("Audio Output Switcher setup", version)
	progress := func(msg string) { fmt.Println(msg) }

	var err error
	if mode == "install" {
		err = install.Install(progress)
	} else {
		err = install.Uninstall(progress)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", mode, err)
		return 1
	}
	return 0
}

// attachConsole points stdout and stderr at the console of the process
// that started setup, unless they already lead somewhere usable (a pipe
// or file it was started with).
func attachConsole() {
	if usable(os.Stdout) {
		return
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}

func usable(f *os.File) bool {
	if f == nil {
		return false
	}
	h := windows.Handle(f.Fd())
	if h == 0 || h == windows.InvalidHandle {
		return false
	}
	t, err := windows.GetFileType(h)
	return err == nil && t != windows.FILE_TYPE_UNKNOWN
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
	cmd := exec.Command("cmd", "/C", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	_ = cmd.Start()
}
