//go:build windows

// Package osd shows a small on-screen overlay styled after Windows' own
// volume/brightness OSD (a dark rounded pill with an icon and text, near
// the bottom of the screen, auto-dismissing after a couple of seconds) -
// entirely in-process, with no dependency on the Windows toast/Action
// Center notification system.
package osd

import (
	"encoding/binary"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procDestroyIcon                = user32.NewProc("DestroyIcon")
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
	procDrawIconEx                 = user32.NewProc("DrawIconEx")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procCreateIconFromResourceEx   = user32.NewProc("CreateIconFromResourceEx")
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
	iconSize     = 48
	iconMarginX  = 18
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

	dtLeft        = 0x0000
	dtVcenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000
	dtNoPrefix    = 0x0800
	transparentBk = 1
	diNormal      = 0x0003
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
)

// Show displays message in the overlay, replacing whatever it's
// currently showing (if anything) so a burst of rapid switches only
// ever shows the latest one.
func Show(message string) {
	mu.Lock()
	old := hwndCurrent
	mu.Unlock()

	if old != 0 {
		procPostMessageW.Call(old, wmClose, 0, 0)
	}

	go run(message)
}

func run(message string) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	classNamePtr, _ := syscall.UTF16PtrFromString(className)
	msgPtr, _ := syscall.UTF16PtrFromString(message)

	hIcon, err := loadIcon(icons.IconEnabled, iconSize)
	if err != nil {
		hIcon = 0 // draw without an icon rather than not showing anything
	}

	proc := func(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
		switch message {
		case wmPaint:
			paint(hwnd, hIcon, msgPtr)
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
			if hIcon != 0 {
				procDestroyIcon.Call(hIcon)
			}
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
	hwndCurrent = hwnd
	mu.Unlock()

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

func paint(hwnd uintptr, hIcon uintptr, msgPtr *uint16) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	bg, _, _ := procCreateSolidBrush.Call(rgb(32, 32, 32))
	defer procDeleteObject.Call(bg)

	full := rect{0, 0, windowWidth, windowHeight}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), bg)

	if hIcon != 0 {
		iconY := (windowHeight - iconSize) / 2
		procDrawIconEx.Call(hdc, iconMarginX, uintptr(iconY), hIcon, iconSize, iconSize, 0, 0, diNormal)
	}

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

	textRect := rect{Left: iconMarginX + iconSize + 16, Top: 0, Right: windowWidth - 16, Bottom: windowHeight}
	procDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(msgPtr)),
		^uintptr(0), // -1: msgPtr is null-terminated, so DrawTextW should compute its length
		uintptr(unsafe.Pointer(&textRect)),
		uintptr(dtLeft|dtVcenter|dtSingleLine|dtEndEllipsis|dtNoPrefix),
	)
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

// loadIcon converts the image closest to size within a multi-resolution
// .ico byte blob into an HICON.
func loadIcon(icoData []byte, size int) (uintptr, error) {
	imgData, err := extractIconImage(icoData, size)
	if err != nil {
		return 0, err
	}
	hIcon, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&imgData[0])),
		uintptr(len(imgData)),
		1,          // fIcon = TRUE
		0x00030000, // dwVer
		uintptr(size), uintptr(size),
		0,
	)
	if hIcon == 0 {
		return 0, fmt.Errorf("CreateIconFromResourceEx failed")
	}
	return hIcon, nil
}

// extractIconImage returns the raw image bytes for the entry closest to
// wantSize from a standard ICONDIR-format .ico file.
func extractIconImage(icoData []byte, wantSize int) ([]byte, error) {
	if len(icoData) < 6 {
		return nil, fmt.Errorf("icon data too short")
	}
	count := int(binary.LittleEndian.Uint16(icoData[4:6]))

	bestIdx, bestDiff := -1, 1<<30
	for i := 0; i < count; i++ {
		off := 6 + i*16
		if off+16 > len(icoData) {
			break
		}
		w := int(icoData[off])
		if w == 0 {
			w = 256
		}
		diff := w - wantSize
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff, bestIdx = diff, i
		}
	}
	if bestIdx == -1 {
		return nil, fmt.Errorf("no icon entries found")
	}

	off := 6 + bestIdx*16
	size := binary.LittleEndian.Uint32(icoData[off+8 : off+12])
	offset := binary.LittleEndian.Uint32(icoData[off+12 : off+16])
	if int(offset+size) > len(icoData) || size == 0 {
		return nil, fmt.Errorf("icon entry out of range")
	}
	return icoData[offset : offset+size], nil
}
