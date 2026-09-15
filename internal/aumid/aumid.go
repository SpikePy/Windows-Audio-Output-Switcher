//go:build windows

// Package aumid locates the Start Menu shortcut that older versions of
// this app (which registered an AppUserModelID to support Windows toast
// notifications) created, so the uninstaller can clean it up.
package aumid

import (
	"os"
	"path/filepath"
)

// ShortcutPath is where the registering Start Menu shortcut used to
// live. Exported so the uninstaller can remove any leftover from a
// previous install.
func ShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Audio Output Switcher.lnk")
}
