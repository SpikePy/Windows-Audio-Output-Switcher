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
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procShowWindow                 = user32.NewProc("ShowWindow")
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
	procGetModuleHandleW           = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")

	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
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

	swShowNoActivate = 4

	wmPaint   = 0x000F
	wmTimer   = 0x0113
	wmClose   = 0x0010
	wmDestroy = 0x0002

	dtCenter      = 0x0001
	dtVcenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000
	dtNoPrefix    = 0x0800
	transparentBk = 1
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
	mu          sync.Mutex
	hwndCurrent uintptr
	// generation increments on every Show call; each run goroutine
	// checks it against the value it was launched with right after
	// creating its window (see run) so that if a newer Show call has
	// since come in - which can happen before hwndCurrent even exists
	// yet to close, on a burst of rapid switches - the newer one always
	// wins the race to be displayed, never an older one that happened
	// to finish CreateWindowExW later.
	generation uint64
)

// Show displays message in the overlay, replacing whatever it's
// currently showing (if anything) so a burst of rapid switches only
// ever shows the latest one.
func Show(message string) {
	mu.Lock()
	generation++
	myGeneration := generation
	old := hwndCurrent
	mu.Unlock()

	if old != 0 {
		procPostMessageW.Call(old, wmClose, 0, 0)
	}

	go run(message, myGeneration)
}

func run(message string, myGeneration uint64) {
	// A window's message queue belongs to the OS thread that created
	// it - GetMessageW/DispatchMessageW below must run on that same
	// thread, which Go doesn't otherwise guarantee across the blocking
	// syscalls in between unless the goroutine is pinned.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	classNamePtr, _ := syscall.UTF16PtrFromString(className)
	msgPtr, _ := syscall.UTF16PtrFromString(message)

	proc := func(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
		switch message {
		case wmPaint:
			paint(hwnd, msgPtr)
			return 0
		case wmTimer:
			procKillTimer.Call(hwnd, timerID)
			procDestroyWindow.Call(hwnd)
			return 0
		case wmClose:
			procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			mu.Lock()
			if hwndCurrent == hwnd {
				hwndCurrent = 0
			}
			mu.Unlock()
			procPostQuitMessage.Call(0)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	}

	wc := wndClassExW{
		LpfnWndProc:   syscall.NewCallback(proc),
		HInstance:     hInstance,
		LpszClassName: classNamePtr,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	// The return value is ignored: if this fails only because the class
	// is already registered from a previous overlay, CreateWindowExW
	// below still succeeds using that existing class.
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	screenW, _, _ := procGetSystemMetrics.Call(smCxscreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyscreen)
	x := (int32(screenW) - windowWidth) / 2
	y := int32(screenH) - windowHeight - bottomMargin

	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTopmost|wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(classNamePtr)),
		0,
		uintptr(wsPopup),
		uintptr(x), uintptr(y), uintptr(windowWidth), uintptr(windowHeight),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}

	mu.Lock()
	superseded := generation != myGeneration
	if !superseded {
		hwndCurrent = hwnd
	}
	mu.Unlock()
	if superseded {
		// A newer Show call already came and went (or is about to)
		// while this one was still creating its window - never publish
		// or display stale content over whatever it showed.
		procDestroyWindow.Call(hwnd)
		return
	}

	hRgn, _, _ := procCreateRoundRectRgn.Call(0, 0, windowWidth+1, windowHeight+1, cornerRadius, cornerRadius)
	procSetWindowRgn.Call(hwnd, hRgn, 1)
	procSetLayeredWindowAttributes.Call(hwnd, 0, 235, lwaAlpha)

	procShowWindow.Call(hwnd, swShowNoActivate)
	procUpdateWindow.Call(hwnd)
	procSetTimer.Call(hwnd, timerID, displayMs, 0)

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

func paint(hwnd uintptr, msgPtr *uint16) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	bg, _, _ := procCreateSolidBrush.Call(rgb(32, 32, 32))
	defer procDeleteObject.Call(bg)

	full := rect{0, 0, windowWidth, windowHeight}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), bg)

	facePtr, _ := syscall.UTF16PtrFromString("Segoe UI")
	font, _, _ := procCreateFontW.Call(
		20, 0, 0, 0, 400,
		0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(facePtr)),
	)
	defer procDeleteObject.Call(font)
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, oldFont)

	procSetTextColor.Call(hdc, rgb(255, 255, 255))
	procSetBkMode.Call(hdc, transparentBk)

	textRect := rect{Left: textMarginX, Top: 0, Right: windowWidth - textMarginX, Bottom: windowHeight}
	procDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(msgPtr)),
		^uintptr(0), // -1: msgPtr is null-terminated, so DrawTextW should compute its length
		uintptr(unsafe.Pointer(&textRect)),
		uintptr(dtCenter|dtVcenter|dtSingleLine|dtEndEllipsis|dtNoPrefix),
	)
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}
