//go:build windows

// Package llhotkey implements global hotkeys using a low-level keyboard
// hook (WH_KEYBOARD_LL) instead of RegisterHotKey.
//
// RegisterHotKey cannot reliably claim combinations Windows itself
// reserves for the shell, such as Win+S (Search) or Win+A (Quick
// Settings/Action Center): the shell's own global shortcut handling sees
// the keystroke first and RegisterHotKey's WM_HOTKEY message never fires.
// A low-level keyboard hook runs earlier in the input pipeline, so it can
// see the keystroke before the shell does and "swallow" it (by returning
// from the hook without calling CallNextHookEx) to stop it from also
// triggering the reserved shell action. This is the same technique tools
// like AutoHotkey use to remap Win+<key> shortcuts.
package llhotkey

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")

	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

const (
	whKeyboardLL = 13

	wmKeydown    = 0x0100
	wmKeyup      = 0x0101
	wmSyskeydown = 0x0104
	wmSyskeyup   = 0x0105
	wmQuit       = 0x0012

	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4 // left Alt
	vkRMenu    = 0xA5 // right Alt
	vkLWin     = 0x5B
	vkRWin     = 0x5C
)

type kbdllhookstruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
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

// Hotkey is one registered global shortcut. Construct with New and start
// receiving presses via Keydown after calling Register.
type Hotkey struct {
	ctrl, alt, shift, win bool
	vkCode                uint32
	downCh                chan struct{}
}

// New creates a hotkey for the given modifiers and virtual-key code. It
// does nothing until Register is called.
func New(ctrl, alt, shift, win bool, vkCode uint32) *Hotkey {
	return &Hotkey{
		ctrl: ctrl, alt: alt, shift: shift, win: win,
		vkCode: vkCode,
		downCh: make(chan struct{}, 1),
	}
}

// Keydown returns a channel that receives an event each time the hotkey
// is pressed.
func (h *Hotkey) Keydown() <-chan struct{} { return h.downCh }

func (h *Hotkey) matches(s modifierState, vkCode uint32) bool {
	return vkCode == h.vkCode &&
		s.ctrl == h.ctrl && s.alt == h.alt && s.shift == h.shift && s.win == h.win
}

type modifierState struct {
	ctrl, alt, shift, win bool
}

var (
	mu       sync.Mutex
	hotkeys  []*Hotkey
	state    modifierState
	started  bool
	threadID uint32
	readyErr error
	ready    = make(chan struct{})
)

// Register starts listening for h's combination, installing the shared
// keyboard hook on its first call.
func Register(h *Hotkey) error {
	mu.Lock()
	hotkeys = append(hotkeys, h)
	needStart := !started
	if needStart {
		started = true
	}
	mu.Unlock()

	if needStart {
		go run()
	}
	<-ready
	return readyErr
}

// Unregister stops listening for h's combination.
func Unregister(h *Hotkey) {
	mu.Lock()
	defer mu.Unlock()
	for i, existing := range hotkeys {
		if existing == h {
			hotkeys = append(hotkeys[:i], hotkeys[i+1:]...)
			return
		}
	}
}

// Stop uninstalls the shared keyboard hook and stops its message loop.
// Safe to call even if Register was never called.
func Stop() {
	mu.Lock()
	tid := threadID
	mu.Unlock()
	if tid != 0 {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}
}

func run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hMod, _, _ := procGetModuleHandleW.Call(0)
	callback := syscall.NewCallback(hookProc)
	hHook, _, callErr := procSetWindowsHookExW.Call(uintptr(whKeyboardLL), callback, hMod, 0)
	if hHook == 0 {
		mu.Lock()
		readyErr = fmt.Errorf("SetWindowsHookExW failed: %v", callErr)
		mu.Unlock()
		close(ready)
		return
	}
	defer procUnhookWindowsHookEx.Call(hHook)

	tid, _, _ := procGetCurrentThreadID.Call()
	mu.Lock()
	threadID = uint32(tid)
	mu.Unlock()
	close(ready)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// hookProc is the WH_KEYBOARD_LL callback. All parameters and the return
// value must be uintptr-sized for syscall.NewCallback.
func hookProc(nCode uintptr, wParam uintptr, lParam uintptr) uintptr {
	if int32(nCode) >= 0 {
		// Reinterpreting through &lParam (rather than converting lParam
		// directly) is the go vet-clean way to turn an OS-supplied address
		// into a pointer.
		kb := *(**kbdllhookstruct)(unsafe.Pointer(&lParam))

		switch wParam {
		case wmKeydown, wmSyskeydown:
			setModifierState(kb.VkCode, true)
			if dispatchKeydown(kb.VkCode) {
				// Swallow the keystroke: don't call CallNextHookEx, so
				// neither the shell nor any other app sees it.
				return 1
			}
		case wmKeyup, wmSyskeyup:
			setModifierState(kb.VkCode, false)
		}
	}

	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

func setModifierState(vkCode uint32, down bool) {
	mu.Lock()
	defer mu.Unlock()
	switch vkCode {
	case vkLWin, vkRWin:
		state.win = down
	case vkLControl, vkRControl:
		state.ctrl = down
	case vkLMenu, vkRMenu:
		state.alt = down
	case vkLShift, vkRShift:
		state.shift = down
	}
}

func dispatchKeydown(vkCode uint32) (swallow bool) {
	mu.Lock()
	s := state
	var matched []*Hotkey
	for _, h := range hotkeys {
		if h.matches(s, vkCode) {
			matched = append(matched, h)
			swallow = true
		}
	}
	mu.Unlock()

	for _, h := range matched {
		select {
		case h.downCh <- struct{}{}:
		default:
			// A previous press hasn't been consumed yet; drop this one
			// rather than blocking the hook (which would stall all
			// keyboard input system-wide).
		}
	}
	return swallow
}
