//go:build windows

// Package osd shows a small on-screen overlay styled after Windows' own
// volume/brightness OSD (a dark rounded pill with centered text, near
// the bottom of the screen, auto-dismissing after a couple of seconds) -
// entirely in-process, with no dependency on the Windows toast/Action
// Center notification system.
package osd

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procSetWindowRgn               = user32.NewProc("SetWindowRgn")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procFillRect                   = user32.NewProc("FillRect")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")

	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
)

const (
	className = "AudioOutputSwitcherOSD"

	windowWidth  = 340
	windowHeight = 84
	textMarginX  = 24
	cornerRadius = 20
	bottomMargin = 140 // clears the taskbar with room to spare
	displayMs    = 1800
	timerID      = 1

	wsPopup = 0x80000000

	wsExLayered    = 0x00080000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000

	lwaAlpha = 0x2

	smCxscreen = 0
	smCyscreen = 1

	swHide = 0

	swpNoSize     = 0x0001
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	hwndTopmost   = ^uintptr(0) // (HWND)-1

	wmPaint   = 0x000F
	wmTimer   = 0x0113
	wmShowOSD = 0x8000 + 1 // WM_APP+1, posted by Show

	dtCenter       = 0x0001
	dtVcenter      = 0x0004
	dtSingleLine   = 0x0020
	dtEndEllipsis  = 0x8000
	dtNoPrefix     = 0x0800
	transparentBk  = 1
	defaultCharset = 1
)

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

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

type rect struct{ Left, Top, Right, Bottom int32 }

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

var (
	startOnce sync.Once
	ready     = make(chan struct{})
	// hwnd is the overlay window, or 0 if it couldn't be created. run sets
	// it once before closing ready, so reading it after <-ready is safe.
	hwnd uintptr

	// font and background are created with the window and, like it, live
	// for the rest of the process.
	font, background uintptr

	textMu sync.Mutex
	text   []uint16 // NUL-terminated text to show; replaced, never mutated
)

// Show displays message in the overlay for a couple of seconds. Calling it
// again while the overlay is up replaces the text and restarts the
// countdown, so a burst of rapid switches only ever shows the latest one.
func Show(message string) {
	startOnce.Do(func() { go run() })
	<-ready
	if hwnd == 0 {
		return
	}

	u, err := windows.UTF16FromString(message)
	if err != nil {
		return
	}
	textMu.Lock()
	text = u
	textMu.Unlock()
	procPostMessageW.Call(hwnd, wmShowOSD, 0, 0)
}

// run owns the overlay window for the whole process. Win32 delivers a
// window's messages only to the thread that created it, so creating,
// painting, showing and hiding it all happen here, on one pinned OS
// thread, driven by the messages Show posts.
func run() {
	runtime.LockOSThread()

	hwnd = createWindow()
	close(ready)
	if hwnd == 0 {
		return
	}

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

// createWindow creates the (initially hidden) overlay window and the GDI
// objects it paints with, returning 0 on failure.
func createWindow() uintptr {
	var hInstance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hInstance); err != nil {
		return 0
	}
	classNamePtr, _ := windows.UTF16PtrFromString(className)
	wc := wndClassExW{
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     uintptr(hInstance),
		LpszClassName: classNamePtr,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	h, _, _ := procCreateWindowExW.Call(
		wsExLayered|wsExTopmost|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(classNamePtr)),
		0,
		wsPopup,
		0, 0, windowWidth, windowHeight,
		0, 0, uintptr(hInstance), 0,
	)
	if h == 0 {
		return 0
	}

	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, windowWidth+1, windowHeight+1, cornerRadius, cornerRadius)
	procSetWindowRgn.Call(h, rgn, 1)
	procSetLayeredWindowAttributes.Call(h, 0, 235, lwaAlpha)

	face, _ := windows.UTF16PtrFromString("Segoe UI")
	font, _, _ = procCreateFontW.Call(20, 0, 0, 0, 400, 0, 0, 0, defaultCharset, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	background, _, _ = procCreateSolidBrush.Call(rgb(32, 32, 32))
	return h
}

func wndProc(h uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmShowOSD:
		// Placed on every show, in case the screen resolution changed.
		screenW, _, _ := procGetSystemMetrics.Call(smCxscreen)
		screenH, _, _ := procGetSystemMetrics.Call(smCyscreen)
		x := (int32(screenW) - windowWidth) / 2
		y := int32(screenH) - windowHeight - bottomMargin
		procSetWindowPos.Call(h, hwndTopmost, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoActivate|swpShowWindow)
		procInvalidateRect.Call(h, 0, 1)
		procUpdateWindow.Call(h)
		procSetTimer.Call(h, timerID, displayMs, 0) // restarts the countdown if it's already running
		return 0
	case wmTimer:
		procKillTimer.Call(h, timerID)
		procShowWindow.Call(h, swHide)
		return 0
	case wmPaint:
		paint(h)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(h, uintptr(message), wParam, lParam)
	return r
}

func paint(h uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(h, uintptr(unsafe.Pointer(&ps)))

	full := rect{0, 0, windowWidth, windowHeight}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), background)

	textMu.Lock()
	t := text
	textMu.Unlock()
	if len(t) == 0 {
		return
	}

	oldFont, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, oldFont)
	procSetTextColor.Call(hdc, rgb(255, 255, 255))
	procSetBkMode.Call(hdc, transparentBk)

	textRect := rect{Left: textMarginX, Top: 0, Right: windowWidth - textMarginX, Bottom: windowHeight}
	procDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(&t[0])),
		^uintptr(0), // -1: t is NUL-terminated, so DrawTextW computes its length
		uintptr(unsafe.Pointer(&textRect)),
		dtCenter|dtVcenter|dtSingleLine|dtEndEllipsis|dtNoPrefix,
	)
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}
