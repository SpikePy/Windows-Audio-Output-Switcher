//go:build windows && visual

// This is the eyeball test for the drawing code: it builds a popup menu
// shaped like the tray's own - a few device entries, one of them skipped,
// followed by Configure and Exit, which Windows still draws itself - and
// leaves it on screen long enough to be looked at or photographed. That
// is the only way to tell whether our entries line up with Windows' own.
//
// It is behind a build tag because it takes over the screen and the
// keyboard for a few seconds. Run it from a Windows shell:
//
//	go test -tags visual -run TestVisualMenu ./internal/menupaint -args -stay 5s
package menupaint

import (
	"flag"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	stay   = flag.Duration("stay", 4*time.Second, "how long to leave the menu up")
	attach = flag.Bool("attach", true, "take over drawing the device entries (false shows the plain Windows menu)")
)

const (
	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = ^uintptr(0x7FFFFFFF) // (int)0x80000000

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfChecked   = 0x0008

	tpmLeftAlign   = 0x0000
	tpmTopAlign    = 0x0000
	tpmRightButton = 0x0002

	wmTimer = 0x0113

	popupX = 120
	popupY = 120
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

func TestVisualMenu(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd := createOwnerWindow(t)
	menu := createMenu(t)

	SetEntries([]Entry{
		{Text: "Speakers (Realtek(R) Audio)"},
		{Text: "Headphones (WH-1000XM4)", Grey: true},
		{Text: "LG HDR 4K (NVIDIA High Definition Audio)"},
	})
	if *attach {
		if err := Attach(); err != nil {
			t.Fatalf("Attach: %v", err)
		}
	}

	dumpMenu = func() {
		var r rect
		for i := 0; i < 7; i++ {
			ok, _, _ := user32.NewProc("GetMenuItemRect").Call(hwnd, menu, uintptr(i), uintptr(unsafe.Pointer(&r)))
			t.Logf("item %d: ok=%d y %d..%d (height %d), x %d..%d", i, ok, r.top, r.bottom, r.bottom-r.top, r.left, r.right)
		}
	}
	if r, _, err := user32.NewProc("SetTimer").Call(hwnd, 2, 800, 0); r == 0 {
		t.Fatalf("SetTimer: %v", err)
	}

	// The menu runs its own message loop, so the only way out from here
	// is a message it dispatches to the window: the timer below fires
	// inside that loop, and endMenuOnTimer closes the menu.
	procSetTimer := user32.NewProc("SetTimer")
	if r, _, err := procSetTimer.Call(hwnd, 1, uintptr(stay.Milliseconds()), 0); r == 0 {
		t.Fatalf("SetTimer: %v", err)
	}
	user32.NewProc("SetForegroundWindow").Call(hwnd)

	t.Logf("menu up at %d,%d for %s", popupX, popupY, *stay)
	r, _, err := user32.NewProc("TrackPopupMenu").Call(
		menu, tpmLeftAlign|tpmTopAlign|tpmRightButton, popupX, popupY, 0, hwnd, 0)
	if r == 0 {
		// 0 is also returned when the menu is dismissed without a pick,
		// which is exactly what the timer does, so only a real error counts.
		if e, ok := err.(windows.Errno); ok && e != 0 {
			t.Logf("TrackPopupMenu: %v", err)
		}
	}
}

// dumpMenu is set by the test so that the timer can report where Windows
// put each item - the only way to compare our entries' size with the ones
// it lays out itself.
var dumpMenu func()

// ownerProc closes the menu when the timer fires, and otherwise behaves
// like any window. menupaint subclasses it, so its own messages arrive
// here after menupaint has had its look at them.
func ownerProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == wmTimer && wParam == 2 {
		user32.NewProc("KillTimer").Call(uintptr(hwnd), 2)
		if dumpMenu != nil {
			dumpMenu()
		}
		return 0
	}
	if msg == wmTimer {
		user32.NewProc("KillTimer").Call(uintptr(hwnd), 1)
		user32.NewProc("EndMenu").Call()
		return 0
	}
	ret, _, _ := user32.NewProc("DefWindowProcW").Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

// createOwnerWindow makes a window of the class menupaint looks for, so
// Attach finds and subclasses it the same way it does the real tray's.
func createOwnerWindow(t *testing.T) uintptr {
	t.Helper()
	class, err := windows.UTF16PtrFromString(trayClassName)
	if err != nil {
		t.Fatal(err)
	}
	instance, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	wc := wndClassExW{
		Style:         0x0003, // CS_HREDRAW | CS_VREDRAW
		LpfnWndProc:   windows.NewCallback(ownerProc),
		HInstance:     instance,
		LpszClassName: class,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatalf("RegisterClassEx: %v", err)
	}
	title, _ := windows.UTF16PtrFromString("menupaint visual test")
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(
		0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		60, 60, 640, 480, 0, 0, instance, 0)
	if hwnd == 0 {
		t.Fatalf("CreateWindowEx: %v", err)
	}
	// The menu only stays up for a window that owns the foreground, which
	// a hidden window never does - the tray's own window is put in the
	// foreground by the click that opens its menu.
	user32.NewProc("ShowWindow").Call(hwnd, 5 /* SW_SHOW */)
	t.Cleanup(func() { user32.NewProc("DestroyWindow").Call(hwnd) })
	return hwnd
}

// createMenu builds the same shape as the tray menu: the device entries
// first (the second one checked, as the active output would be), then a
// separator, Configure, another separator and Exit.
func createMenu(t *testing.T) uintptr {
	t.Helper()
	menu, _, err := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		t.Fatalf("CreatePopupMenu: %v", err)
	}
	t.Cleanup(func() { user32.NewProc("DestroyMenu").Call(menu) })

	append := user32.NewProc("AppendMenuW")
	add := func(flags uintptr, id uintptr, text string) {
		p, _ := windows.UTF16PtrFromString(text)
		append.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
	}
	add(mfString, 1, "Speakers (Realtek(R) Audio)")
	add(mfString, 2, "Headphones (WH-1000XM4)")
	add(mfString, 3, "LG HDR 4K (NVIDIA High Definition Audio)")
	append.Call(menu, mfSeparator, 0, 0)
	add(mfString, 4, "Configure")
	append.Call(menu, mfSeparator, 0, 0)
	add(mfString, 5, "Exit")

	user32.NewProc("CheckMenuItem").Call(menu, 1, mfByPosition|mfChecked)
	return menu
}
