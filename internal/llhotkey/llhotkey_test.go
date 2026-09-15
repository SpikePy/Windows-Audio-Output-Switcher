//go:build windows

package llhotkey

import "testing"

const vkS = 0x53

// withHotkeys makes hks the registered hotkeys, with no modifiers held,
// for the rest of the test - without installing the real keyboard hook.
func withHotkeys(t *testing.T, hks ...*Hotkey) {
	t.Helper()
	mu.Lock()
	prevHotkeys, prevState := hotkeys, state
	hotkeys, state = hks, modifierState{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		hotkeys, state = prevHotkeys, prevState
		mu.Unlock()
	})
}

func TestModifierState(t *testing.T) {
	var s modifierState
	s.set(vkLWin, true)
	s.set(vkRControl, true)
	s.set(vkS, true) // not a modifier: ignored
	if s != (modifierState{win: true, ctrl: true}) {
		t.Fatalf("after pressing Win, Ctrl and S: %+v", s)
	}
	s.set(vkLWin, false)
	if s != (modifierState{ctrl: true}) {
		t.Fatalf("after releasing Win: %+v", s)
	}
}

func TestDispatchKeydownNeedsExactModifiers(t *testing.T) {
	hk := New(false, false, false, true, vkS) // win+s
	withHotkeys(t, hk)

	if dispatchKeydown(vkS) {
		t.Error("plain S was swallowed, want it passed through")
	}

	setModifierState(vkLWin, true)
	if !dispatchKeydown(vkS) {
		t.Fatal("Win+S was not swallowed")
	}
	select {
	case <-hk.Keydown():
	default:
		t.Fatal("Win+S didn't signal the hotkey")
	}

	setModifierState(vkLShift, true)
	if dispatchKeydown(vkS) {
		t.Error("Win+Shift+S matched a Win+S hotkey")
	}
}

func TestDispatchKeydownNeverBlocksOrAllocates(t *testing.T) {
	hk := New(false, false, false, true, vkS)
	withHotkeys(t, hk)
	setModifierState(vkLWin, true)

	// Nothing consumes the presses, so after the first one fills the
	// channel's buffer every further press must be dropped rather than
	// block - and none may allocate.
	allocs := testing.AllocsPerRun(100, func() { dispatchKeydown(vkS) })
	if allocs != 0 {
		t.Errorf("dispatchKeydown allocates %v times per call, want 0", allocs)
	}
}
