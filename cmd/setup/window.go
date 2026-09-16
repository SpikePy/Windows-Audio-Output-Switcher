//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/install"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/setupmenu"
)

// Setup's window: plain Win32 controls on one small, fixed-size, centred
// window - the app's icon, name and version, a message line, a marquee
// progress bar while working, and the Install/update, Uninstall and
// Close buttons. What happens when is decided by internal/setupmenu; this
// file only draws it. The install/uninstall itself runs on a separate
// goroutine that posts its progress back, so the window never freezes.

var (
	user32                         = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procSendMessageW               = user32.NewProc("SendMessageW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procIsDialogMessageW           = user32.NewProc("IsDialogMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetWindowTextW             = user32.NewProc("SetWindowTextW")
	procSetFocus                   = user32.NewProc("SetFocus")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procGetSysColor                = user32.NewProc("GetSysColor")
	procGetSysColorBrush           = user32.NewProc("GetSysColorBrush")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procDrawIconEx                 = user32.NewProc("DrawIconEx")
	procCreateIconFromResourceEx   = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon                = user32.NewProc("DestroyIcon")
	procGetDpiForWindow            = user32.NewProc("GetDpiForWindow")
	procAdjustWindowRectExForDpi   = user32.NewProc("AdjustWindowRectExForDpi")
	procSystemParametersInfoForDpi = user32.NewProc("SystemParametersInfoForDpi")
	procSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	procMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	procMessageBoxW                = user32.NewProc("MessageBoxW")
	gdi32                          = windows.NewLazySystemDLL("gdi32.dll")
	procCreateFontIndirectW        = gdi32.NewProc("CreateFontIndirectW")
	procDeleteObject               = gdi32.NewProc("DeleteObject")
	procSetBkColor                 = gdi32.NewProc("SetBkColor")
	procSetTextColor               = gdi32.NewProc("SetTextColor")
	comctl32                       = windows.NewLazySystemDLL("comctl32.dll")
	procInitCommonControlsEx       = comctl32.NewProc("InitCommonControlsEx")
)

const (
	wmDestroy                = 0x0002
	wmPaint                  = 0x000F
	wmClose                  = 0x0010
	wmSetFont                = 0x0030
	wmSetIcon                = 0x0080
	wmCommand                = 0x0111
	wmTimer                  = 0x0113
	wmCtlColorStatic         = 0x0138
	wmDpiChanged             = 0x02E0
	wmApp                    = 0x8000
	wmProgress               = wmApp + 1
	wmFinished               = wmApp + 2
	bmSetStyle               = 0x00F4
	pbmSetMarquee            = 0x0400 + 10
	wsOverlapped             = 0x00000000
	wsCaption                = 0x00C00000
	wsSysMenu                = 0x00080000
	wsMinimizeBox            = 0x00020000
	wsChild                  = 0x40000000
	wsVisible                = 0x10000000
	wsTabStop                = 0x00010000
	wsExControlParent        = 0x00010000
	bsPushButton             = 0x0
	bsDefPushButton          = 0x1
	ssNoPrefix               = 0x80
	pbsMarquee               = 0x08
	swHide                   = 0
	swShow                   = 5
	swpNoSize                = 0x0001
	swpNoMove                = 0x0002
	swpNoZOrder              = 0x0004
	swpNoActivate            = 0x0010
	cwUseDefault             = 0x80000000
	idOK                     = 1
	idCancel                 = 2
	idInstall                = 101
	idUninstall              = 102
	idClose                  = 103
	timerCountdown           = 1
	colorWindow              = 5
	colorWindowText          = 8
	colorGrayText            = 17
	idcArrow                 = 32512
	iconSmall                = 0
	iconBig                  = 1
	diNormal                 = 0x0003
	lrDefaultColor           = 0
	spiGetNonClientMetrics   = 0x0029
	monitorDefaultToPrimary  = 1
	iccStandardClasses       = 0x00004000
	iccProgressClass         = 0x00000020
	dpiAwarenessPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	mbIconError              = 0x10

	windowStyle   = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox
	windowExStyle = wsExControlParent
)

// Layout, in pixels at 96 DPI; scale converts to the window's DPI.
const (
	clientWidth  = 440
	margin       = 20
	iconSize     = 40
	textGap      = 14
	messageTop   = margin + iconSize + 16
	messageLines = 3
	progressGap  = 8
	progressH    = 4
	buttonGap    = 18
	buttonH      = 30
	installW     = 160
	otherW       = 110
	buttonSpace  = 8
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32
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

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

type nonClientMetrics struct {
	CbSize                            uint32
	BorderWidth, ScrollWidth          int32
	ScrollHeight                      int32
	CaptionWidth, CaptionHeight       int32
	CaptionFont                       logFont
	SmCaptionWidth, SmCaptionHeight   int32
	SmCaptionFont                     logFont
	MenuWidth, MenuHeight             int32
	MenuFont, StatusFont, MessageFont logFont
	PaddedBorderWidth                 int32
}

type monitorInfo struct {
	CbSize   uint32
	Monitor  rect
	WorkArea rect
	Flags    uint32
}

type initCommonControlsEx struct {
	Size, ICC uint32
}

// ui holds the window's state. It's only touched on the window's own
// thread, except for progress, which the worker goroutine hands over.
var ui struct {
	hwnd                                 uintptr
	title, version, message, progressBar uintptr
	installBtn, uninstallBtn, closeBtn   uintptr
	font, titleFont                      uintptr
	icon, iconSmall, iconBig             uintptr
	dpi                                  uint32
	lineHeight                           int // one line of the message font, in pixels
	flow                                 *setupmenu.Flow
	uninstalled                          bool

	mu       sync.Mutex
	progress string
	result   error
}

const introText = "Install or update to the latest release from GitHub, or remove Audio Output Switcher from this PC."

// runWindow shows setup's window until it's closed, and reports whether
// Audio Output Switcher was uninstalled.
func runWindow() (uninstalled bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	procSetProcessDpiAwarenessCtx.Call(dpiAwarenessPerMonitorV2) // the manifest says so too
	icc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: iccStandardClasses | iccProgressClass}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	ui.flow = setupmenu.New()
	if err := createWindow(); err != nil {
		messageBox("Audio Output Switcher setup could not open its window: " + err.Error())
		return false
	}

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		// Gives Tab, Enter and Escape their usual dialog behaviour.
		if r, _, _ := procIsDialogMessageW.Call(ui.hwnd, uintptr(unsafe.Pointer(&m))); r != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return ui.uninstalled
}

func createWindow() error {
	className := utf16("AudioOutputSwitcherSetup")
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	background, _, _ := procGetSysColorBrush.Call(colorWindow)
	wc := wndClassExW{
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HCursor:       cursor,
		HbrBackground: background,
		LpszClassName: className,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("register window class: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(windowExStyle, uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16("Audio Output Switcher setup"))), windowStyle,
		cwUseDefault, cwUseDefault, 400, 300, 0, 0, 0, 0)
	if hwnd == 0 {
		return fmt.Errorf("create window: %w", err)
	}
	ui.hwnd = hwnd

	ui.title = child("STATIC", "Audio Output Switcher", ssNoPrefix, 0)
	ui.version = child("STATIC", "Setup "+version, ssNoPrefix, 0)
	ui.message = child("STATIC", introText, ssNoPrefix, 0)
	ui.progressBar = child("msctls_progress32", "", pbsMarquee, 0)
	ui.installBtn = child("BUTTON", ui.flow.InstallLabel(), bsDefPushButton|wsTabStop, idInstall)
	ui.uninstallBtn = child("BUTTON", "&Uninstall", bsPushButton|wsTabStop, idUninstall)
	ui.closeBtn = child("BUTTON", "Close", bsPushButton|wsTabStop, idClose)
	show(ui.progressBar, false)
	show(ui.closeBtn, false)

	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	applyDpi(uint32(dpi))
	centre()

	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(ui.installBtn)
	procSetTimer.Call(hwnd, timerCountdown, 1000, 0)
	return nil
}

func child(class, text string, style, id uintptr) uintptr {
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))),
		wsChild|wsVisible|style, 0, 0, 0, 0, ui.hwnd, id, 0, 0)
	return h
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTimer:
		switch ui.flow.Tick() {
		case setupmenu.Start:
			start()
		case setupmenu.Close:
			procDestroyWindow.Call(hwnd)
		}
		setText(ui.installBtn, ui.flow.InstallLabel())
		setText(ui.closeBtn, ui.flow.CloseLabel())
		return 0

	case wmCommand:
		switch wParam & 0xFFFF {
		case idInstall:
			choose(setupmenu.Install)
		case idUninstall:
			choose(setupmenu.Uninstall)
		case idOK: // Enter
			if ui.flow.Choosing() {
				choose(setupmenu.Install)
			} else if ui.flow.Done() {
				procDestroyWindow.Call(hwnd)
			}
		case idClose, idCancel: // Close button, Escape
			if !ui.flow.Running() {
				procDestroyWindow.Call(hwnd)
			}
		}
		return 0

	case wmProgress:
		ui.mu.Lock()
		text := ui.progress
		ui.mu.Unlock()
		setText(ui.message, text)
		return 0

	case wmFinished:
		finish()
		return 0

	case wmClose:
		// Stopping half-way through replacing files would leave a broken
		// install behind.
		if !ui.flow.Running() {
			procDestroyWindow.Call(hwnd)
		}
		return 0

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		size := scale(iconSize)
		procDrawIconEx.Call(hdc, uintptr(scale(margin)), uintptr(scale(margin)), ui.icon, uintptr(size), uintptr(size), 0, 0, diNormal)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0

	case wmCtlColorStatic:
		textColor := uintptr(colorWindowText)
		if lParam == ui.version {
			textColor = colorGrayText
		}
		bg, _, _ := procGetSysColor.Call(colorWindow)
		fg, _, _ := procGetSysColor.Call(textColor)
		procSetBkColor.Call(wParam, bg)
		procSetTextColor.Call(wParam, fg)
		brush, _, _ := procGetSysColorBrush.Call(colorWindow)
		return brush

	case wmDpiChanged:
		applyDpi(uint32(wParam & 0xFFFF))
		suggested := (*rect)(unsafe.Pointer(lParam))
		procSetWindowPos.Call(hwnd, 0, uintptr(suggested.Left), uintptr(suggested.Top),
			uintptr(suggested.Right-suggested.Left), uintptr(suggested.Bottom-suggested.Top), swpNoZOrder|swpNoActivate)
		return 0

	case wmDestroy:
		procKillTimer.Call(hwnd, timerCountdown)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func choose(a setupmenu.Action) {
	if ui.flow.Choose(a) {
		start()
	}
}

// start switches the window to its working look and runs the chosen
// action on a goroutine of its own.
func start() {
	show(ui.installBtn, false)
	show(ui.uninstallBtn, false)
	show(ui.progressBar, true)
	procSendMessageW.Call(ui.progressBar, pbmSetMarquee, 1, 30)
	setText(ui.installBtn, ui.flow.InstallLabel())

	action := ui.flow.Action()
	hwnd := ui.hwnd
	go func() {
		progress := func(msg string) {
			ui.mu.Lock()
			ui.progress = msg
			ui.mu.Unlock()
			procPostMessageW.Call(hwnd, wmProgress, 0, 0)
		}
		var err error
		if action == setupmenu.Install {
			err = install.Install(progress)
		} else {
			err = install.Uninstall(progress)
		}
		ui.mu.Lock()
		ui.result = err
		ui.mu.Unlock()
		procPostMessageW.Call(hwnd, wmFinished, 0, 0)
	}()
}

// finish shows how the action ended and offers the Close button.
func finish() {
	ui.mu.Lock()
	err := ui.result
	ui.mu.Unlock()

	ui.flow.Finish(err)
	procSendMessageW.Call(ui.progressBar, pbmSetMarquee, 0, 0)
	show(ui.progressBar, false)
	if err != nil {
		what := "Install"
		if ui.flow.Action() == setupmenu.Uninstall {
			what = "Uninstall"
		}
		setText(ui.message, fmt.Sprintf("%s failed: %v", what, err))
	} else if ui.flow.Action() == setupmenu.Uninstall {
		ui.uninstalled = true
	}

	setText(ui.closeBtn, ui.flow.CloseLabel())
	procSendMessageW.Call(ui.closeBtn, bmSetStyle, bsDefPushButton, 1)
	show(ui.closeBtn, true)
	procSetFocus.Call(ui.closeBtn)
}

// applyDpi (re)creates the DPI-dependent fonts and icons and lays the
// controls out for dpi.
func applyDpi(dpi uint32) {
	if dpi == 0 {
		dpi = 96
	}
	ui.dpi = dpi

	var ncm nonClientMetrics
	ncm.CbSize = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoForDpi.Call(spiGetNonClientMetrics, uintptr(ncm.CbSize), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(dpi))
	if ncm.MessageFont.Height == 0 { // not available before Windows 10 1607
		ncm.MessageFont.Height = -int32(scale(12))
		copy(ncm.MessageFont.FaceName[:], windows.StringToUTF16("Segoe UI"))
	}
	// The font's character height (a negative lfHeight) plus the usual
	// leading.
	height := int(ncm.MessageFont.Height)
	if height < 0 {
		height = -height
	}
	ui.lineHeight = height * 4 / 3
	titleLF := ncm.MessageFont
	titleLF.Height = titleLF.Height * 4 / 3
	titleLF.Weight = 600

	oldFont, oldTitleFont := ui.font, ui.titleFont
	ui.font, _, _ = procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&ncm.MessageFont)))
	ui.titleFont, _, _ = procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&titleLF)))
	for _, h := range []uintptr{ui.version, ui.message, ui.installBtn, ui.uninstallBtn, ui.closeBtn} {
		procSendMessageW.Call(h, wmSetFont, ui.font, 1)
	}
	procSendMessageW.Call(ui.title, wmSetFont, ui.titleFont, 1)
	for _, h := range []uintptr{oldFont, oldTitleFont} {
		if h != 0 {
			procDeleteObject.Call(h)
		}
	}

	oldIcons := []uintptr{ui.icon, ui.iconSmall, ui.iconBig}
	ui.icon = loadIcon(scale(iconSize))
	ui.iconSmall = loadIcon(scale(16))
	ui.iconBig = loadIcon(scale(32))
	procSendMessageW.Call(ui.hwnd, wmSetIcon, iconSmall, ui.iconSmall)
	procSendMessageW.Call(ui.hwnd, wmSetIcon, iconBig, ui.iconBig)
	for _, h := range oldIcons {
		if h != 0 {
			procDestroyIcon.Call(h)
		}
	}

	layout()
}

func layout() {
	width := scale(clientWidth)
	textLeft := scale(margin + iconSize + textGap)
	textWidth := width - textLeft - scale(margin)
	lineH := ui.lineHeight

	move(ui.title, textLeft, scale(margin), textWidth, lineH*4/3+scale(2))
	move(ui.version, textLeft, scale(margin)+lineH*4/3+scale(4), textWidth, lineH)

	top := scale(messageTop)
	messageH := lineH * messageLines
	move(ui.message, scale(margin), top, width-2*scale(margin), messageH)
	top += messageH + scale(progressGap)
	move(ui.progressBar, scale(margin), top, width-2*scale(margin), scale(progressH))
	top += scale(progressH) + scale(buttonGap)

	right := width - scale(margin)
	move(ui.uninstallBtn, right-scale(otherW), top, scale(otherW), scale(buttonH))
	move(ui.installBtn, right-scale(otherW)-scale(buttonSpace)-scale(installW), top, scale(installW), scale(buttonH))
	move(ui.closeBtn, right-scale(otherW), top, scale(otherW), scale(buttonH))
	height := top + scale(buttonH) + scale(margin)

	r := rect{Right: int32(width), Bottom: int32(height)}
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), windowStyle, 0, windowExStyle, uintptr(ui.dpi))
	procSetWindowPos.Call(ui.hwnd, 0, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoMove|swpNoZOrder|swpNoActivate)
}

// centre puts the window in the middle of the primary monitor's work area.
func centre() {
	var wr rect
	mon, _, _ := procMonitorFromWindow.Call(ui.hwnd, monitorDefaultToPrimary)
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	procGetWindowRect.Call(ui.hwnd, uintptr(unsafe.Pointer(&wr)))
	w, h := wr.Right-wr.Left, wr.Bottom-wr.Top
	x := mi.WorkArea.Left + (mi.WorkArea.Right-mi.WorkArea.Left-w)/2
	y := mi.WorkArea.Top + (mi.WorkArea.Bottom-mi.WorkArea.Top-h)/2
	procSetWindowPos.Call(ui.hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func loadIcon(size int) uintptr {
	data, ok := icons.Image(icons.Icon, size)
	if !ok {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, uintptr(size), uintptr(size), lrDefaultColor)
	return h
}

func scale(v int) int {
	return v * int(ui.dpi) / 96
}

func move(h uintptr, x, y, w, height int) {
	procSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(height), swpNoZOrder|swpNoActivate)
}

func show(h uintptr, visible bool) {
	cmd := uintptr(swHide)
	if visible {
		cmd = swShow
	}
	procShowWindow.Call(h, cmd)
}

func setText(h uintptr, text string) {
	procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(utf16(text))))
}

func messageBox(text string) {
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("Audio Output Switcher setup"))), mbIconError)
}

func utf16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("?")
	}
	return p
}
