// Package hotkeycfg parses human-friendly hotkey combo strings such as
// "ctrl+alt+f9" into the modifiers and key golang.design/x/hotkey expects.
package hotkeycfg

import (
	"fmt"
	"strings"

	"golang.design/x/hotkey"
)

var namedKeys = map[string]hotkey.Key{
	"space":  hotkey.KeySpace,
	"return": hotkey.KeyReturn,
	"enter":  hotkey.KeyReturn,
	"escape": hotkey.KeyEscape,
	"esc":    hotkey.KeyEscape,
	"delete": hotkey.KeyDelete,
	"del":    hotkey.KeyDelete,
	"tab":    hotkey.KeyTab,
	"left":   hotkey.KeyLeft,
	"right":  hotkey.KeyRight,
	"up":     hotkey.KeyUp,
	"down":   hotkey.KeyDown,
}

// Parse converts a combo string like "ctrl+alt+f9" into the modifier list
// and key that golang.design/x/hotkey.New expects. Recognized modifier
// tokens are ctrl, alt, shift and win; the final token must resolve to a
// single key (a letter, a digit, F1-F20, or one of the namedKeys above).
func Parse(combo string) ([]hotkey.Modifier, hotkey.Key, error) {
	parts := strings.Split(combo, "+")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("hotkey %q needs at least one modifier and a key, e.g. %q", combo, "ctrl+alt+f9")
	}

	mods := make([]hotkey.Modifier, 0, len(parts)-1)
	for _, raw := range parts[:len(parts)-1] {
		mod, err := parseModifier(raw)
		if err != nil {
			return nil, 0, err
		}
		mods = append(mods, mod)
	}

	key, err := parseKey(parts[len(parts)-1])
	if err != nil {
		return nil, 0, err
	}
	return mods, key, nil
}

func parseModifier(raw string) (hotkey.Modifier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ctrl", "control":
		return hotkey.ModCtrl, nil
	case "alt":
		return hotkey.ModAlt, nil
	case "shift":
		return hotkey.ModShift, nil
	case "win", "windows", "super", "cmd":
		return hotkey.ModWin, nil
	default:
		return 0, fmt.Errorf("unknown hotkey modifier %q", raw)
	}
}

func parseKey(raw string) (hotkey.Key, error) {
	key := strings.ToLower(strings.TrimSpace(raw))

	if k, ok := namedKeys[key]; ok {
		return k, nil
	}
	if len(key) == 1 {
		c := key[0]
		switch {
		case c >= 'a' && c <= 'z':
			return hotkey.Key(0x41 + int(c-'a')), nil
		case c >= '0' && c <= '9':
			return hotkey.Key(0x30 + int(c-'0')), nil
		}
	}
	if strings.HasPrefix(key, "f") {
		var n int
		if _, err := fmt.Sscanf(key, "f%d", &n); err == nil && n >= 1 && n <= 20 {
			return hotkey.Key(0x70 + n - 1), nil
		}
	}
	return 0, fmt.Errorf("unknown hotkey key %q", raw)
}
