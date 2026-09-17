//go:build windows

package main

// Setup's window is a Windows task dialog (TaskDialogIndirect): the
// system's own dialog with push buttons, a progress bar and pages, so
// setup needs no GUI toolkit. Task dialogs live in version 6 of the
// common controls, which assets/setup.manifest - embedded through
// rsrc_windows_amd64.syso - asks for.
//
// Page one offers Install/Update, Uninstall and Close. Unless a button is
// clicked first, Install/Update runs by itself after
// install.AutoInstallAfter. A progress page follows, then a result page
// that closes itself install.AutoCloseAfter after a success and stays
// open after an error.

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/assets/icons"
	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/install"
)

var (
	// comctl32 is loaded by bare name, not from an explicit System32
	// path, so the manifest can redirect it to version 6 - the one with
	// task dialogs.
	modComctl32            = windows.NewLazyDLL("comctl32.dll")
	procTaskDialogIndirect = modComctl32.NewProc("TaskDialogIndirect")

	modUser32                    = windows.NewLazySystemDLL("user32.dll")
	procSendMessageW             = modUser32.NewProc("SendMessageW")
	procCreateIconFromResourceEx = modUser32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = modUser32.NewProc("DestroyIcon")
	procGetDpiForSystem          = modUser32.NewProc("GetDpiForSystem")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmUser                   = 0x0400
	tdmNavigatePage          = wmUser + 101
	tdmClickButton           = wmUser + 102
	tdmSetProgressBarMarquee = wmUser + 107
	tdmSetElementText        = wmUser + 108
	tdmEnableButton          = wmUser + 111

	tdnCreated       = 0
	tdnNavigated     = 1
	tdnButtonClicked = 2
	tdnTimer         = 4

	tdeContent = 0

	tdfUseHIconMain            = 0x0002
	tdfAllowDialogCancellation = 0x0008
	tdfShowMarqueeProgressBar  = 0x0400
	tdfCallbackTimer           = 0x0800

	tdErrorIcon = 0xFFFE // MAKEINTRESOURCE(-2)

	sOK    = 0
	sFalse = 1

	// idCancel is Close on every page, so Escape and the title bar's X,
	// which Windows reports as IDCANCEL, do exactly what Close does.
	idCancel        = 2
	buttonInstall   = 101
	buttonUninstall = 102
)

// Which page is showing; the callback learns it through lpCallbackData.
const (
	pageChoose = iota
	pageProgress
	pageDone
	pageFailed
)

const title = "Audio Output Switcher Setup"

var (
	// Set before the dialog opens and only read afterwards. The callback
	// is created in runDialog rather than here because dialogProc leads
	// back to it (through pack), which Go rejects as an init cycle.
	dialogCallback uintptr
	appIcon        uintptr

	// Only touched on the dialog's thread, in dialogProc and what it calls.
	busy         bool // the progress page is showing
	autoInstall  = true
	ticking      bool // the current page has had its first timer tick
	shownSeconds int

	// Set by the worker, read once the dialog has closed.
	failed      atomic.Bool
	uninstalled atomic.Bool

	// kept holds every page handed to Windows, so the memory its raw
	// pointers refer to stays alive while the dialog may still use it.
	keptMu sync.Mutex
	kept   []*packedPage
)

// page describes one page of the dialog. Every page also gets a Close
// button (idCancel).
type page struct {
	instruction, content string
	countdown            string // the countdown line to start with, "" for none
	flags                uint32
	buttons              []button
	defaultButton        int32
	errorIcon            bool
	kind                 int
}

type button struct {
	id   int32
	text string
}

// pageInfo is what the callback knows about the page on screen.
type pageInfo struct {
	kind    int
	content string // without the countdown line
}

// packedPage is a TASKDIALOGCONFIG as Windows reads it, plus everything
// its raw pointers refer to.
type packedPage struct {
	buf    []byte
	strs   [][]uint16
	arrays [][]byte
	info   *pageInfo
}

// str keeps s alive in p and returns its address as UTF-16, or 0 for "".
func (p *packedPage) str(s string) uint64 {
	if s == "" {
		return 0
	}
	u, err := windows.UTF16FromString(s)
	if err != nil {
		u, _ = windows.UTF16FromString("?")
	}
	p.strs = append(p.strs, u)
	return uint64(uintptr(unsafe.Pointer(&u[0])))
}

// buttonArray lays bs out as TASKDIALOG_BUTTONs (1-byte packed: the int32
// id, then the text pointer) and returns the array's address.
func (p *packedPage) buttonArray(bs []button) uint64 {
	arr := make([]byte, 12*len(bs))
	for i, b := range bs {
		binary.LittleEndian.PutUint32(arr[12*i:], uint32(b.id))
		binary.LittleEndian.PutUint64(arr[12*i+4:], p.str(b.text))
	}
	p.arrays = append(p.arrays, arr)
	return uint64(uintptr(unsafe.Pointer(&arr[0])))
}

func (p *packedPage) addr() uintptr { return uintptr(unsafe.Pointer(&p.buf[0])) }

// pack lays the page out as a TASKDIALOGCONFIG. The Windows headers
// declare it with 1-byte packing, which a Go struct can't express, so the
// fields go in at their packed 64-bit offsets instead.
func (pg page) pack() *packedPage {
	p := &packedPage{buf: make([]byte, 160), info: &pageInfo{kind: pg.kind, content: pg.content}}
	le := binary.LittleEndian

	flags := pg.flags | tdfAllowDialogCancellation
	content := pg.content
	if pg.countdown != "" {
		flags |= tdfCallbackTimer
		content += "\n\n" + pg.countdown
	}
	buttons := append(append([]button{}, pg.buttons...), button{idCancel, "Close"})
	module, _, _ := procGetModuleHandleW.Call(0)

	le.PutUint32(p.buf[0:], 160)             // cbSize
	le.PutUint64(p.buf[12:], uint64(module)) // hInstance
	le.PutUint64(p.buf[28:], p.str(title))   // pszWindowTitle
	switch {
	case pg.errorIcon:
		le.PutUint64(p.buf[36:], tdErrorIcon) // pszMainIcon
	case appIcon != 0:
		flags |= tdfUseHIconMain
		le.PutUint64(p.buf[36:], uint64(appIcon)) // hMainIcon
	}
	footer := fmt.Sprintf("Setup %s - installs for your account only, no administrator rights needed", version)
	le.PutUint32(p.buf[20:], flags)                                    // dwFlags
	le.PutUint64(p.buf[44:], p.str(pg.instruction))                    // pszMainInstruction
	le.PutUint64(p.buf[52:], p.str(content))                           // pszContent
	le.PutUint32(p.buf[60:], uint32(len(buttons)))                     // cButtons
	le.PutUint64(p.buf[64:], p.buttonArray(buttons))                   // pButtons
	le.PutUint32(p.buf[72:], uint32(pg.defaultButton))                 // nDefaultButton
	le.PutUint64(p.buf[132:], p.str(footer))                           // pszFooter
	le.PutUint64(p.buf[140:], uint64(dialogCallback))                  // pfCallback
	le.PutUint64(p.buf[148:], uint64(uintptr(unsafe.Pointer(p.info)))) // lpCallbackData

	keptMu.Lock()
	kept = append(kept, p)
	keptMu.Unlock()
	return p
}

// runDialog shows setup's window and returns the process exit code, and
// whether Audio Output Switcher was uninstalled.
func runDialog() (code int, removed bool) {
	runtime.LockOSThread()
	if err := procTaskDialogIndirect.Find(); err != nil {
		messageBox("Setup can't show its window on this version of Windows:\n\n" + err.Error())
		return 1, false
	}

	dialogCallback = syscall.NewCallback(dialogProc)
	if icon := loadAppIcon(); icon != 0 {
		appIcon = icon
		defer procDestroyIcon.Call(icon)
	}

	first := page{
		instruction: "Install, update or remove Audio Output Switcher?",
		content: "It switches your default audio output with a hotkey (Win+A unless you change it) " +
			"or a click on its icon in the notification area.",
		countdown:     install.AutoInstallText(startSeconds(install.AutoInstallAfter)),
		buttons:       []button{{buttonInstall, "Install/Update"}, {buttonUninstall, "Uninstall"}},
		defaultButton: buttonInstall,
		kind:          pageChoose,
	}.pack()

	if hr, _, _ := procTaskDialogIndirect.Call(first.addr(), 0, 0, 0); hr != 0 {
		messageBox(fmt.Sprintf("Setup couldn't open its window (error 0x%08X).", hr))
		return 1, false
	}
	if failed.Load() {
		return 1, false
	}
	return 0, uninstalled.Load()
}

// dialogProc is the task dialog's callback; Windows calls it on the
// dialog's own thread.
func dialogProc(hwnd, msg, wParam, lParam, refData uintptr) uintptr {
	info := (*pageInfo)(unsafe.Pointer(refData))
	switch msg {
	case tdnCreated, tdnNavigated:
		busy = info.kind == pageProgress
		ticking = false
		shownSeconds = -1
		if busy {
			sendMessage(hwnd, tdmSetProgressBarMarquee, 1, 0)
			sendMessage(hwnd, tdmEnableButton, idCancel, 0)
		}
	case tdnTimer:
		return tick(hwnd, info, time.Duration(wParam)*time.Millisecond)
	case tdnButtonClicked:
		switch wParam {
		case buttonInstall:
			start(hwnd, "install")
			return sFalse // keep the dialog open
		case buttonUninstall:
			start(hwnd, "uninstall")
			return sFalse
		case idCancel:
			if busy {
				return sFalse // no closing halfway through
			}
		}
	}
	return sOK
}

// tick runs the current page's countdown, if it has one. The first tick on
// a page restarts Windows' tick count, so elapsed runs from about then.
func tick(hwnd uintptr, info *pageInfo, elapsed time.Duration) uintptr {
	var length time.Duration
	var text func(int) string
	switch {
	case info.kind == pageChoose && autoInstall:
		length, text = install.AutoInstallAfter, install.AutoInstallText
	case info.kind == pageDone:
		length, text = install.AutoCloseAfter, install.AutoCloseText
	default:
		return sOK
	}
	if !ticking {
		ticking = true
		return sFalse // S_FALSE resets the tick count
	}

	secs, over := install.SecondsLeft(length, elapsed)
	switch {
	case over && info.kind == pageChoose:
		start(hwnd, "install")
	case over:
		sendMessage(hwnd, tdmClickButton, idCancel, 0)
	case secs != shownSeconds:
		shownSeconds = secs
		setContent(hwnd, info.content+"\n\n"+text(secs))
	}
	return sOK
}

func startSeconds(length time.Duration) int {
	secs, _ := install.SecondsLeft(length, 0)
	return secs
}

// start switches to the progress page and runs action on a separate
// goroutine, so the dialog keeps painting meanwhile. The worker only
// talks to the dialog through SendMessage, which Windows hands to the
// dialog's thread.
func start(hwnd uintptr, action string) {
	autoInstall = false
	verb := "Installing"
	if action == "uninstall" {
		verb = "Removing"
	}
	navigate(hwnd, page{
		instruction:   verb + " Audio Output Switcher...",
		content:       "Getting started...",
		flags:         tdfShowMarqueeProgressBar,
		defaultButton: idCancel,
		kind:          pageProgress,
	})

	go func() {
		var last string
		progress := func(step string) {
			last = step
			setContent(hwnd, step)
		}
		var err error
		if action == "install" {
			err = install.Install(progress)
		} else {
			err = install.Uninstall(progress)
			uninstalled.Store(err == nil)
		}
		failed.Store(err != nil)
		navigate(hwnd, resultPage(action, last, err))
	}()
}

// resultPage is the last page: what to do next, or what went wrong.
// summary is the last step install reported, which says what version is
// installed and whether it starts at login.
func resultPage(action, summary string, err error) page {
	if err != nil {
		return page{
			instruction:   "Setup didn't finish",
			content:       err.Error(),
			errorIcon:     true,
			defaultButton: idCancel,
			kind:          pageFailed,
		}
	}
	pg := page{
		countdown:     install.AutoCloseText(startSeconds(install.AutoCloseAfter)),
		defaultButton: idCancel,
		kind:          pageDone,
	}
	if action == "install" {
		pg.instruction = "Audio Output Switcher is running"
		pg.content = summary + "\n\nLook for its icon in the notification area: left-click it (or press the hotkey) " +
			"to switch to the next output, right-click it for every output, Configure and Exit."
	} else {
		pg.instruction = "Audio Output Switcher has been removed"
		pg.content = "Its program, Startup shortcut and settings are gone."
	}
	return pg
}

// loadAppIcon makes the app's icon, at the size the dialog shows it, from
// the .ico the tray uses too.
func loadAppIcon() uintptr {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	size := int(32 * dpi / 96)
	data, ok := icons.Image(icons.Icon, size)
	if !ok {
		return 0
	}
	const resourceVersion = 0x00030000
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1,
		resourceVersion, uintptr(size), uintptr(size), 0)
	return h
}

func navigate(hwnd uintptr, pg page) {
	sendMessage(hwnd, tdmNavigatePage, 0, pg.pack().addr())
}

func setContent(hwnd uintptr, text string) {
	u, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	sendMessage(hwnd, tdmSetElementText, tdeContent, uintptr(unsafe.Pointer(u)))
	runtime.KeepAlive(u)
}

func sendMessage(hwnd, msg, wParam, lParam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, msg, wParam, lParam)
	return r
}

func messageBox(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	windows.MessageBox(0, t, c, windows.MB_OK|windows.MB_ICONERROR)
}
