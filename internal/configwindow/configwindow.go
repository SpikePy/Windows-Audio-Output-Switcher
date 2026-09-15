//go:build windows

// Package configwindow shows a small native window letting the user pick
// which playback devices should be skipped when cycling through outputs.
// It's built directly on raw Win32 (RegisterClassExW/CreateWindowExW and
// plain BS_AUTOCHECKBOX buttons) rather than a GUI toolkit, to keep the
// binary dependency-free.
package configwindow

import (
	"runtime"
	"sort"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsTabStop     = 0x00010000

	wsFixedWindow = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox

	bsAutoCheckbox  = 0x00000003
	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001

	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1
	bstChecked = 1

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmCommand = 0x0111

	smCxscreen = 0
	smCyscreen = 1

	swShow = 5

	idcArrow = 32512

	colorBtnfaceBrush = uintptr(15 + 1) // COLOR_BTNFACE+1: the documented trick for a stock-color class background

	className = "AudioOutputSwitcherConfigWindow"

	idSave   = 1
	idCancel = 2

	windowWidth  = 380
	marginX      = 20
	marginTop    = 44
	rowHeight    = 26
	rowSpacing   = 4
	buttonWidth  = 90
	buttonHeight = 28
	minHeight    = 200
	maxHeight    = 560
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

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type windowState struct {
	mu     sync.Mutex
	hwnd   uintptr
	checks map[string]uintptr // device name -> checkbox HWND
	onSave func(map[string]bool)
}

var win windowState

// Open shows the Configure Outputs window listing every name in names
// with a checkbox (checked unless the name is in skip), or brings the
// window to the foreground if it's already open. onSave receives the set
// of names left unchecked once the user clicks Save; it is not called on
// Cancel or if the window is simply closed.
func Open(names []string, skip map[string]bool, onSave func(map[string]bool)) {
	win.mu.Lock()
	if win.hwnd != 0 {
		hwnd := win.hwnd
		win.mu.Unlock()
		procSetForegroundWindow.Call(hwnd)
		return
	}
	win.mu.Unlock()

	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	skipCopy := make(map[string]bool, len(skip))
	for k, v := range skip {
		skipCopy[k] = v
	}

	go run(sorted, skipCopy, onSave)
}

func run(names []string, skip map[string]bool, onSave func(map[string]bool)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	classNamePtr, _ := syscall.UTF16PtrFromString(className)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := wndClassExW{
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hInstance,
		HCursor:       cursor,
		HbrBackground: colorBtnfaceBrush,
		LpszClassName: classNamePtr,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	// The return value is ignored: if this fails only because the class
	// is already registered from a previous time this window was opened,
	// CreateWindowExW below still succeeds using that existing class.
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	windowHeight := marginTop + len(names)*(rowHeight+rowSpacing) + 20 + buttonHeight + 40
	if windowHeight < minHeight {
		windowHeight = minHeight
	}
	if windowHeight > maxHeight {
		windowHeight = maxHeight
	}

	screenW, _, _ := procGetSystemMetrics.Call(smCxscreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyscreen)
	x := (int32(screenW) - windowWidth) / 2
	y := (int32(screenH) - int32(windowHeight)) / 2

	titlePtr, _ := syscall.UTF16PtrFromString("Configure Outputs - Audio Output Switcher")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsFixedWindow),
		uintptr(x), uintptr(y), uintptr(windowWidth), uintptr(windowHeight),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}

	win.mu.Lock()
	win.hwnd = hwnd
	win.checks = make(map[string]uintptr, len(names))
	win.onSave = onSave
	win.mu.Unlock()

	labelPtr, _ := syscall.UTF16PtrFromString("Included when switching (hotkey, left-click, or Next):")
	createStatic(hwnd, hInstance, labelPtr, marginX, 12, windowWidth-2*marginX, 20)

	rowY := int32(marginTop)
	for _, name := range names {
		hCheck := createCheckbox(hwnd, hInstance, name, marginX, rowY, windowWidth-2*marginX, rowHeight, !skip[name])
		win.mu.Lock()
		win.checks[name] = hCheck
		win.mu.Unlock()
		rowY += rowHeight + rowSpacing
	}

	buttonY := int32(windowHeight) - buttonHeight - 52
	saveX := int32(windowWidth) - 2*buttonWidth - marginX - 10
	cancelX := int32(windowWidth) - buttonWidth - marginX

	createButton(hwnd, hInstance, "Save", saveX, buttonY, buttonWidth, buttonHeight, idSave, true)
	createButton(hwnd, hInstance, "Cancel", cancelX, buttonY, buttonWidth, buttonHeight, idCancel, false)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createStatic(parent, hInstance uintptr, text *uint16, x, y, w, h int32) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString("STATIC")
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(text)),
		uintptr(wsChild|wsVisible),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, 0, hInstance, 0,
	)
	return hwnd
}

func createCheckbox(parent, hInstance uintptr, label string, x, y, w, h int32, checked bool) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString("BUTTON")
	textPtr, _ := syscall.UTF16PtrFromString(label)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)),
		uintptr(wsChild|wsVisible|wsTabStop|bsAutoCheckbox),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, 0, hInstance, 0,
	)
	if checked {
		procSendMessageW.Call(hwnd, bmSetCheck, bstChecked, 0)
	}
	return hwnd
}

func createButton(parent, hInstance uintptr, label string, x, y, w, h int32, id uintptr, isDefault bool) uintptr {
	style := uintptr(wsChild | wsVisible | wsTabStop | bsPushButton)
	if isDefault {
		style = uintptr(wsChild | wsVisible | wsTabStop | bsDefPushButton)
	}
	classPtr, _ := syscall.UTF16PtrFromString("BUTTON")
	textPtr, _ := syscall.UTF16PtrFromString(label)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, hInstance, 0,
	)
	return hwnd
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		switch wParam & 0xFFFF {
		case idSave:
			save()
			procDestroyWindow.Call(hwnd)
			return 0
		case idCancel:
			procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		win.mu.Lock()
		win.hwnd = 0
		win.checks = nil
		win.onSave = nil
		win.mu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func save() {
	win.mu.Lock()
	checks := win.checks
	onSave := win.onSave
	win.mu.Unlock()

	skip := make(map[string]bool)
	for name, hCheck := range checks {
		state, _, _ := procSendMessageW.Call(hCheck, bmGetCheck, 0, 0)
		if state != bstChecked {
			skip[name] = true
		}
	}
	if onSave != nil {
		onSave(skip)
	}
}
