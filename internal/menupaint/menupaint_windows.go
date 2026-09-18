//go:build windows

package menupaint

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	uxtheme = windows.NewLazySystemDLL("uxtheme.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW          = user32.NewProc("CallWindowProcW")
	procSetMenuItemInfoW         = user32.NewProc("SetMenuItemInfoW")
	procGetMenuStringW           = user32.NewProc("GetMenuStringW")
	procGetMenuItemCount         = user32.NewProc("GetMenuItemCount")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfoW    = user32.NewProc("SystemParametersInfoW")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procFillRect                 = user32.NewProc("FillRect")
	procDrawTextW                = user32.NewProc("DrawTextW")
	procDrawFrameControl         = user32.NewProc("DrawFrameControl")
	procGetSysColor              = user32.NewProc("GetSysColor")
	procGetSysColorBrush         = user32.NewProc("GetSysColorBrush")

	procCreateFontIndirectW   = gdi32.NewProc("CreateFontIndirectW")
	procSelectObject          = gdi32.NewProc("SelectObject")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	procSetTextColor          = gdi32.NewProc("SetTextColor")
	procSetBkMode             = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn    = gdi32.NewProc("CreateRoundRectRgn")
	procFillRgn               = gdi32.NewProc("FillRgn")
	procGetPixel              = gdi32.NewProc("GetPixel")

	procOpenThemeData       = uxtheme.NewProc("OpenThemeData")
	procCloseThemeData      = uxtheme.NewProc("CloseThemeData")
	procDrawThemeBackground = uxtheme.NewProc("DrawThemeBackground")
	procGetThemeColor       = uxtheme.NewProc("GetThemeColor")
	procIsAppThemed         = uxtheme.NewProc("IsAppThemed")
)

const (
	// The tray library's message window, which owns the menu.
	trayClassName = "SystrayClass"

	wmInitMenuPopup = 0x0117
	wmDrawItem      = 0x002B
	wmMeasureItem   = 0x002C
	wmSettingChange = 0x001A
	wmThemeChanged  = 0x031A

	gwlpWndProc = ^uintptr(3) // -4

	miimFType = 0x0100
	miimData  = 0x0020

	mftOwnerDraw = 0x0100
	mfByPosition = 0x0400

	odtMenu = 1

	odsSelected = 0x0001
	odsChecked  = 0x0008

	smCxMenuCheck = 71
	smCyMenuCheck = 72

	colorMenu          = 4
	colorMenuText      = 7
	colorHighlight     = 13
	colorHighlightText = 14
	colorGrayText      = 17
	colorMenuHilight   = 29

	spiGetNonClientMetrics = 0x0029

	dtLeft       = 0x0000
	dtVCenter    = 0x0004
	dtSingleLine = 0x0020
	dtNoPrefix   = 0x0800

	bkTransparent = 1

	dfcMenu       = 2
	dfcsMenuCheck = 0x0001

	// Theme parts and states of the "Menu" class, and the text-colour
	// property, as declared in vssym32.h.
	menuPopupItem   = 14
	mpiNormal       = 1
	mpiHot          = 2
	mpiDisabled     = 3
	mpiDisabledHot  = 4
	menuPopupCheck  = 11
	mcCheckNormal   = 1
	mcCheckDisabled = 2
	tmtTextColor    = 3803

	clrInvalid = 0xFFFFFFFF

	// How far a skipped entry's label is washed out toward the selection
	// bar while the pointer is on it.
	fadedOnSelection = 40
)

// Entry is one device row of the tray menu, in menu order: the label the
// tray library was given for it, and whether it should be drawn faded
// because the device is skipped when cycling.
type Entry struct {
	Text string
	Grey bool
}

var state struct {
	mu      sync.Mutex
	entries []Entry
	oldProc uintptr

	// Set while a popup is open, so the menu's real background colour is
	// sampled once per popup rather than on every repaint.
	bgSampled bool
	bgColor   uint32

	font    windows.Handle
	metrics metrics // the parts of it that don't depend on the text

	colors   menuColors
	colorsOK bool
}

// Attach takes over drawing the tray menu's device entries. It must be
// called once the tray library has created its window - from the
// systray "ready" callback - and from then on SetEntries decides which
// entries are drawn faded.
func Attach() error {
	hwnd, err := findTrayWindow()
	if err != nil {
		return err
	}
	old, _, err := procSetWindowLongPtrW.Call(uintptr(hwnd), gwlpWndProc, wndProcCallback)
	if old == 0 {
		return fmt.Errorf("subclass tray window: %w", err)
	}
	state.mu.Lock()
	state.oldProc = old
	state.mu.Unlock()
	themeColors() // read them now rather than mid-popup
	return nil
}

// SetEntries records the device entries currently in the menu, in the
// order they appear in it. Entries beyond what the menu actually holds
// are ignored, so a stale call can only ever draw less than it should.
func SetEntries(entries []Entry) {
	state.mu.Lock()
	state.entries = append(state.entries[:0:0], entries...)
	state.mu.Unlock()
}

func entryAt(i int) (Entry, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if i < 0 || i >= len(state.entries) {
		return Entry{}, false
	}
	return state.entries[i], true
}

func snapshot() []Entry {
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]Entry(nil), state.entries...)
}

// findTrayWindow looks for the tray library's message window in this
// process. Other tray programs built on the same library have a window
// of the same class, so the process ID is what tells ours apart.
func findTrayWindow() (windows.Handle, error) {
	var found windows.Handle
	self := uint32(windows.Getpid())
	cb := windows.NewCallback(func(hwnd windows.Handle, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
		if pid != self {
			return 1 // keep going
		}
		buf := make([]uint16, len(trayClassName)+2)
		n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 || windows.UTF16ToString(buf[:n]) != trayClassName {
			return 1
		}
		found = hwnd
		return 0 // stop
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return 0, errors.New("tray window not found")
	}
	return found, nil
}

var wndProcCallback = windows.NewCallback(wndProc)

func wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInitMenuPopup:
		markOwnerDrawn(windows.Handle(wParam))
	case wmMeasureItem:
		if measureItem(lParam) {
			return 1
		}
	case wmDrawItem:
		if drawItem(lParam) {
			return 1
		}
	case wmSettingChange, wmThemeChanged:
		forgetCached()
	}

	state.mu.Lock()
	old := state.oldProc
	state.mu.Unlock()
	ret, _, _ := procCallWindowProcW.Call(old, uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

// menuItemInfoW is MENUITEMINFOW; the padding Go inserts between the
// 32-bit fields and the pointer-sized ones matches the C layout on
// amd64, so unsafe.Sizeof gives the cbSize Windows expects.
type menuItemInfoW struct {
	Size      uint32
	Mask      uint32
	Type      uint32
	State     uint32
	ID        uint32
	SubMenu   windows.Handle
	Checked   windows.Handle
	Unchecked windows.Handle
	ItemData  uintptr
	TypeData  *uint16
	Cch       uint32
	BmpItem   windows.Handle
}

// markOwnerDrawn turns the menu's device entries into owner-draw items,
// stashing each one's index in dwItemData so the draw messages can find
// it again. The tray library resets an item to a plain string whenever
// it sets its title, so this runs every time the menu opens.
//
// An entry is only taken over when the text in the menu is still the one
// SetEntries recorded for that position: if the two ever drift apart,
// the item is left to Windows rather than drawn with the wrong label.
func markOwnerDrawn(menu windows.Handle) {
	state.mu.Lock()
	state.bgSampled = false
	state.mu.Unlock()

	entries := snapshot()
	count, _, _ := procGetMenuItemCount.Call(uintptr(menu))
	for i, e := range entries {
		if int32(count) >= 0 && i >= int(int32(count)) {
			break
		}
		if menuItemText(menu, i) != e.Text {
			continue
		}
		mi := menuItemInfoW{
			Mask:     miimFType | miimData,
			Type:     mftOwnerDraw,
			ItemData: uintptr(i + 1), // 0 means "not ours"
		}
		mi.Size = uint32(unsafe.Sizeof(mi))
		procSetMenuItemInfoW.Call(uintptr(menu), uintptr(i), 1, uintptr(unsafe.Pointer(&mi)))
	}
}

func menuItemText(menu windows.Handle, pos int) string {
	n, _, _ := procGetMenuStringW.Call(uintptr(menu), uintptr(pos), 0, 0, mfByPosition)
	if int32(n) <= 0 {
		return ""
	}
	buf := make([]uint16, int32(n)+1)
	got, _, _ := procGetMenuStringW.Call(uintptr(menu), uintptr(pos),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mfByPosition)
	if int32(got) <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
}

type measureItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemWidth  uint32
	ItemHeight uint32
	ItemData   uintptr
}

func measureItem(lParam uintptr) bool {
	mis := (*measureItemStruct)(unsafe.Pointer(lParam))
	if mis.CtlType != odtMenu {
		return false
	}
	e, ok := entryAt(int(mis.ItemData) - 1)
	if !ok {
		return false
	}
	m := systemMetrics()
	m.textWidth, m.textHeight = textExtent(e.Text)
	w, h := itemSize(m)
	mis.ItemWidth, mis.ItemHeight = uint32(w), uint32(h)
	return true
}

type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   windows.Handle
	HDC        windows.Handle
	RcItem     rect
	ItemData   uintptr
}

func drawItem(lParam uintptr) bool {
	dis := (*drawItemStruct)(unsafe.Pointer(lParam))
	if dis.CtlType != odtMenu {
		return false
	}
	e, ok := entryAt(int(dis.ItemData) - 1)
	if !ok {
		return false
	}
	paint(dis.HDC, dis.RcItem, e,
		dis.ItemState&odsSelected != 0,
		dis.ItemState&odsChecked != 0)
	return true
}

// paint draws one device entry: the menu background, the highlight if
// the pointer is on it, a check mark if it's the active output, and the
// label - faded when the device is skipped.
func paint(hdc windows.Handle, item rect, e Entry, selected, checked bool) {
	theme := openMenuTheme()
	if theme != 0 {
		defer procCloseThemeData.Call(uintptr(theme))
	}

	m := systemMetrics()
	check, text := layout(item, m)

	fillBackground(hdc, item, selected, theme)

	// The colour is set before the check mark is drawn, not just before
	// the label: without a theme the mark is a monochrome bitmap blitted
	// in the current text colour, so this is what keeps it in step with
	// the label - faded with it, and light on the selection bar.
	procSetBkMode.Call(uintptr(hdc), bkTransparent)
	procSetTextColor.Call(uintptr(hdc), uintptr(textColor(e.Grey, selected)))

	if checked {
		drawCheck(hdc, check, e.Grey, theme)
	}

	var old uintptr
	if font := menuFont(); font != 0 {
		old, _, _ = procSelectObject.Call(uintptr(hdc), uintptr(font))
	}
	if utf16, err := windows.UTF16FromString(e.Text); err == nil && len(utf16) > 1 {
		rc := text
		procDrawTextW.Call(uintptr(hdc), uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1),
			uintptr(unsafe.Pointer(&rc)), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	if old != 0 {
		procSelectObject.Call(uintptr(hdc), old)
	}
}

// fillBackground paints the item's background: the menu's own colour, or
// the selection when the pointer is on the entry. Windows draws the popup
// before handing us the item, so the plain colour is sampled straight off
// the menu rather than guessed from a system colour - that keeps the
// entry invisible against whatever the current theme paints.
func fillBackground(hdc windows.Handle, item rect, selected bool, theme windows.Handle) {
	rc := item
	if selected && theme != 0 {
		if ret, _, _ := procDrawThemeBackground.Call(uintptr(theme), uintptr(hdc),
			menuPopupItem, mpiHot, uintptr(unsafe.Pointer(&rc)), 0); ret == 0 {
			return
		}
	}
	var brush uintptr
	if selected {
		// The entries Windows draws itself fill the whole row, so this
		// one does too.
		if brush, _, _ = procGetSysColorBrush.Call(colorMenuHilight); brush == 0 {
			brush, _, _ = procGetSysColorBrush.Call(colorHighlight)
		}
		procFillRect.Call(uintptr(hdc), uintptr(unsafe.Pointer(&rc)), brush)
		return
	}
	if brush, _, _ = procCreateSolidBrush.Call(uintptr(sampleBackground(hdc, item))); brush == 0 {
		return
	}
	procFillRect.Call(uintptr(hdc), uintptr(unsafe.Pointer(&rc)), brush)
	procDeleteObject.Call(brush)
}

// sampleBackground reads the menu's own background colour off a pixel
// Windows has already painted, once per popup, so COLOR_MENU is only
// fallen back on if that fails.
func sampleBackground(hdc windows.Handle, item rect) uint32 {
	state.mu.Lock()
	sampled, color := state.bgSampled, state.bgColor
	state.mu.Unlock()
	if sampled {
		return color
	}
	if item.width() > 4 {
		px, _, _ := procGetPixel.Call(uintptr(hdc),
			uintptr(item.right-2), uintptr(item.top+item.height()/2))
		if uint32(px) != clrInvalid {
			state.mu.Lock()
			state.bgSampled, state.bgColor = true, uint32(px)
			state.mu.Unlock()
			return uint32(px)
		}
	}
	sys, _, _ := procGetSysColor.Call(colorMenu)
	return uint32(sys)
}

func drawCheck(hdc windows.Handle, check rect, grey bool, theme windows.Handle) {
	rc := check
	if theme != 0 {
		st := uintptr(mcCheckNormal)
		if grey {
			st = mcCheckDisabled
		}
		if ret, _, _ := procDrawThemeBackground.Call(uintptr(theme), uintptr(hdc),
			menuPopupCheck, st, uintptr(unsafe.Pointer(&rc)), 0); ret == 0 {
			return
		}
	}
	procDrawFrameControl.Call(uintptr(hdc), uintptr(unsafe.Pointer(&rc)), dfcMenu, dfcsMenuCheck)
}

// textColor is the whole point of drawing the entry ourselves: a skipped
// device gets the colour the current theme uses for text that isn't
// available, the same grey Windows would put on a disabled item.
func textColor(grey, selected bool) uint32 {
	c := themeColors()
	switch {
	case grey && selected:
		return c.greyHot
	case grey:
		return c.grey
	case selected:
		return c.hot
	default:
		return c.normal
	}
}

// menuColors are the four label colours a menu entry can be drawn in.
type menuColors struct {
	normal, hot, grey, greyHot uint32
}

// themeColors reads those colours from the theme once and keeps them
// until Windows reports a theme or settings change. They are deliberately
// not read while the menu is on screen: asking uxtheme for them from
// inside WM_DRAWITEM makes the popup disappear halfway through opening.
func themeColors() menuColors {
	state.mu.Lock()
	c, ok := state.colors, state.colorsOK
	state.mu.Unlock()
	if ok {
		return c
	}
	c = readColors()
	state.mu.Lock()
	state.colors, state.colorsOK = c, true
	state.mu.Unlock()
	return c
}

func readColors() menuColors {
	c := menuColors{
		normal: sysColor(colorMenuText),
		hot:    sysColor(colorHighlightText),
		grey:   sysColor(colorGrayText),
	}
	c.greyHot = fade(c.hot, sysColor(colorMenuHilight), fadedOnSelection)

	theme := openMenuTheme()
	if theme == 0 {
		return c
	}
	defer procCloseThemeData.Call(uintptr(theme))
	for _, f := range []struct {
		state uintptr
		into  *uint32
	}{
		{mpiNormal, &c.normal},
		{mpiHot, &c.hot},
		{mpiDisabled, &c.grey},
		{mpiDisabledHot, &c.greyHot},
	} {
		var got uint32
		if ret, _, _ := procGetThemeColor.Call(uintptr(theme), menuPopupItem, f.state,
			tmtTextColor, uintptr(unsafe.Pointer(&got))); ret == 0 {
			*f.into = got
		}
	}
	return c
}

func sysColor(index uintptr) uint32 {
	c, _, _ := procGetSysColor.Call(index)
	return uint32(c)
}

func openMenuTheme() windows.Handle {
	if themed, _, _ := procIsAppThemed.Call(); themed == 0 {
		return 0
	}
	name, err := windows.UTF16PtrFromString("Menu")
	if err != nil {
		return 0
	}
	h, _, _ := procOpenThemeData.Call(0, uintptr(unsafe.Pointer(name)))
	return windows.Handle(h)
}

// logFontW and nonClientMetricsW are only needed for lfMenuFont, the
// font Windows draws menu labels in.
type logFontW struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

type nonClientMetricsW struct {
	Size              uint32
	BorderWidth       int32
	ScrollWidth       int32
	ScrollHeight      int32
	CaptionWidth      int32
	CaptionHeight     int32
	CaptionFont       logFontW
	SmCaptionWidth    int32
	SmCaptionHeight   int32
	SmCaptionFont     logFontW
	MenuWidth         int32
	MenuHeight        int32
	MenuFont          logFontW
	StatusFont        logFontW
	MessageFont       logFontW
	PaddedBorderWidth int32
}

// menuFont returns the menu font, created once and kept until Windows
// says the display settings or the theme changed.
func menuFont() windows.Handle {
	state.mu.Lock()
	f := state.font
	state.mu.Unlock()
	if f != 0 {
		return f
	}
	var ncm nonClientMetricsW
	ncm.Size = uint32(unsafe.Sizeof(ncm))
	ret, _, _ := procSystemParametersInfoW.Call(spiGetNonClientMetrics,
		uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0)
	if ret == 0 {
		return 0
	}
	h, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&ncm.MenuFont)))
	state.mu.Lock()
	state.font = windows.Handle(h)
	state.mu.Unlock()
	return windows.Handle(h)
}

// forgetCached drops everything read from the system, so a theme or
// display change is picked up the next time the menu opens.
func forgetCached() {
	state.mu.Lock()
	f := state.font
	state.font = 0
	state.metrics = metrics{}
	state.colorsOK = false
	state.mu.Unlock()
	if f != 0 {
		procDeleteObject.Call(uintptr(f))
	}
}

func systemMetrics() metrics {
	state.mu.Lock()
	m := state.metrics
	state.mu.Unlock()
	if m.checkHeight != 0 {
		return m
	}
	cx, _, _ := procGetSystemMetrics.Call(smCxMenuCheck)
	cy, _, _ := procGetSystemMetrics.Call(smCyMenuCheck)
	m = metrics{checkWidth: int32(cx), checkHeight: int32(cy)}
	state.mu.Lock()
	state.metrics = m
	state.mu.Unlock()
	return m
}

type size struct{ cx, cy int32 }

func textExtent(s string) (width, height int32) {
	utf16, err := windows.UTF16FromString(s)
	if err != nil || len(utf16) < 2 {
		return 0, 0
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return 0, 0
	}
	defer procReleaseDC.Call(0, hdc)

	var old uintptr
	if f := menuFont(); f != 0 {
		old, _, _ = procSelectObject.Call(hdc, uintptr(f))
	}
	var sz size
	procGetTextExtentPoint32W.Call(hdc, uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(len(utf16)-1), uintptr(unsafe.Pointer(&sz)))
	if old != 0 {
		procSelectObject.Call(hdc, old)
	}
	return sz.cx, sz.cy
}
