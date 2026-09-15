package hotkeycfg

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		combo    string
		wantMods Modifiers
		wantKey  uint32
	}{
		{"win+s", Modifiers{Win: true}, 'S'},
		{"ctrl+alt+f9", Modifiers{Ctrl: true, Alt: true}, 0x78},
		{"Control + Shift + 1", Modifiers{Ctrl: true, Shift: true}, '1'},
		{"super+space", Modifiers{Win: true}, 0x20},
		{"alt+F20", Modifiers{Alt: true}, 0x83},
		{"ctrl+esc", Modifiers{Ctrl: true}, 0x1B},
	}
	for _, tt := range tests {
		mods, key, err := Parse(tt.combo)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tt.combo, err)
			continue
		}
		if mods != tt.wantMods || key != tt.wantKey {
			t.Errorf("Parse(%q) = %+v, %#x; want %+v, %#x", tt.combo, mods, key, tt.wantMods, tt.wantKey)
		}
	}
}

func TestParseRejectsInvalidCombos(t *testing.T) {
	for _, combo := range []string{"", "s", "win+", "hyper+s", "ctrl+f0", "ctrl+f21", "ctrl+ab"} {
		if _, _, err := Parse(combo); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", combo)
		}
	}
}
