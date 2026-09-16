//go:build windows

// Package autostart keeps the shortcut in the current user's Startup
// folder that launches Audio Output Switcher at login in line with the
// config file's autostart setting. It's shared by cmd/setup (on install)
// and the app itself (on start, and whenever the config file changes).
package autostart

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// shortcutName is the fixed filename of the Startup folder entry, so
// re-creating it replaces the one shortcut rather than adding another.
const shortcutName = "Audio Output Switcher.lnk"

// StartupDir returns the current user's Startup folder; anything placed
// there is launched automatically at login.
func StartupDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

// ShortcutPath is where the autostart shortcut lives while autostart is on.
func ShortcutPath() string {
	return filepath.Join(StartupDir(), shortcutName)
}

// Set creates (or replaces) the Startup folder shortcut to target when
// enabled is true, and removes it when false - a shortcut that's already
// gone isn't an error.
func Set(enabled bool, target string) error {
	if !enabled {
		if err := os.Remove(ShortcutPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove autostart shortcut: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(StartupDir(), 0o755); err != nil {
		return fmt.Errorf("create startup folder: %w", err)
	}
	if err := createShortcut(ShortcutPath(), target); err != nil {
		return fmt.Errorf("create autostart shortcut: %w", err)
	}
	return nil
}

var (
	clsidShellLink  = ole.NewGUID("{00021401-0000-0000-C000-000000000046}")
	iidIShellLinkW  = ole.NewGUID("{000214F9-0000-0000-C000-000000000046}")
	iidIPersistFile = ole.NewGUID("{0000010B-0000-0000-C000-000000000046}")
)

const sFalse = 1

// iShellLinkWVtbl and iPersistFileVtbl list their methods in the order
// shobjidl_core.h / objidl.h declare them.
type iShellLinkWVtbl struct {
	ole.IUnknownVtbl
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type iPersistFileVtbl struct {
	ole.IUnknownVtbl
	GetClassID uintptr
	IsDirty    uintptr
	Load       uintptr
	Save       uintptr
}

// createShortcut writes a .lnk file at path pointing to target, through
// the shell's own IShellLink object. COM is initialized on a thread of
// its own, which is discarded afterwards, so this is safe to call from
// any goroutine regardless of what COM state its thread has.
func createShortcut(path, target string) error {
	done := make(chan error, 1)
	go func() {
		// Never unlocked: the thread exits with the goroutine, taking its
		// COM apartment with it.
		runtime.LockOSThread()
		// S_FALSE (already initialized) still has to be balanced with
		// CoUninitialize, same as S_OK.
		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			if oleErr, ok := err.(*ole.OleError); !ok || oleErr.Code() != sFalse {
				done <- err
				return
			}
		}
		defer ole.CoUninitialize()
		done <- writeShortcut(path, target)
	}()
	return <-done
}

func writeShortcut(path, target string) error {
	link, err := ole.CreateInstance(clsidShellLink, iidIShellLinkW)
	if err != nil {
		return err
	}
	defer link.Release()
	linkVtbl := (*iShellLinkWVtbl)(unsafe.Pointer(link.RawVTable))

	if err := callWithString(linkVtbl.SetPath, link, target); err != nil {
		return fmt.Errorf("set target: %w", err)
	}
	if err := callWithString(linkVtbl.SetWorkingDirectory, link, filepath.Dir(target)); err != nil {
		return fmt.Errorf("set working directory: %w", err)
	}

	pf, err := link.QueryInterface(iidIPersistFile)
	if err != nil {
		return err
	}
	defer pf.Release()
	pfVtbl := (*iPersistFileVtbl)(unsafe.Pointer(pf.RawVTable))

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(pfVtbl.Save, uintptr(unsafe.Pointer(pf)), uintptr(unsafe.Pointer(p)), 1)
	if hr != 0 {
		return fmt.Errorf("save: %w", ole.NewError(hr))
	}
	return nil
}

func callWithString(method uintptr, obj *ole.IUnknown, s string) error {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return err
	}
	if hr, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(obj)), uintptr(unsafe.Pointer(p))); hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}
