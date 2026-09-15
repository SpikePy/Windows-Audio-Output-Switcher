// Package hotkeycfg parses human-friendly hotkey combo strings such as
// "ctrl+alt+f9" into the modifiers and virtual-key code Windows APIs
// expect.
package hotkeycfg

import (
	"fmt"
	"strings"
)

// Modifiers is the set of modifier keys held down alongside the trigger
// key.
type Modifiers struct {
	Ctrl  bool
	Alt   bool
	Shift bool
	Win   bool
}

// Windows virtual-key codes for the named keys Parse recognizes.
// https://learn.microsoft.com/windows/win32/inputdev/virtual-key-codes
const (
	vkSpace  = 0x20
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkDelete = 0x2E
	vkTab    = 0x09
	vkLeft   = 0x25
	vkRight  = 0x27
	vkUp     = 0x26
	vkDown   = 0x28
)

var namedKeys = map[string]uint32{
	"space":  vkSpace,
	"return": vkReturn,
	"enter":  vkReturn,
	"escape": vkEscape,
	"esc":    vkEscape,
	"delete": vkDelete,
	"del":    vkDelete,
	"tab":    vkTab,
	"left":   vkLeft,
	"right":  vkRight,
	"up":     vkUp,
	"down":   vkDown,
}

// Parse converts a combo string like "ctrl+alt+f9" into its modifiers and
// virtual-key code. Recognized modifier tokens are ctrl, alt, shift and
// win; the final token must resolve to a single key (a letter, a digit,
// F1-F20, or one of the namedKeys above).
func Parse(combo string) (Modifiers, uint32, error) {
	parts := strings.Split(combo, "+")
	if len(parts) < 2 {
		return Modifiers{}, 0, fmt.Errorf("hotkey %q needs at least one modifier and a key, e.g. %q", combo, "ctrl+alt+f9")
	}

	var mods Modifiers
	for _, raw := range parts[:len(parts)-1] {
		if err := applyModifier(&mods, raw); err != nil {
			return Modifiers{}, 0, err
		}
	}

	key, err := parseKey(parts[len(parts)-1])
	if err != nil {
		return Modifiers{}, 0, err
	}
	return mods, key, nil
}

func applyModifier(mods *Modifiers, raw string) error {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ctrl", "control":
		mods.Ctrl = true
	case "alt":
		mods.Alt = true
	case "shift":
		mods.Shift = true
	case "win", "windows", "super", "cmd":
		mods.Win = true
	default:
		return fmt.Errorf("unknown hotkey modifier %q", raw)
	}
	return nil
}

func parseKey(raw string) (uint32, error) {
	key := strings.ToLower(strings.TrimSpace(raw))

	if k, ok := namedKeys[key]; ok {
		return k, nil
	}
	if len(key) == 1 {
		c := key[0]
		switch {
		case c >= 'a' && c <= 'z':
			return 0x41 + uint32(c-'a'), nil // VK_A..VK_Z
		case c >= '0' && c <= '9':
			return 0x30 + uint32(c-'0'), nil // VK_0..VK_9
		}
	}
	if strings.HasPrefix(key, "f") {
		var n int
		if _, err := fmt.Sscanf(key, "f%d", &n); err == nil && n >= 1 && n <= 20 {
			return 0x70 + uint32(n-1), nil // VK_F1..VK_F20
		}
	}
	return 0, fmt.Errorf("unknown hotkey key %q", raw)
}
