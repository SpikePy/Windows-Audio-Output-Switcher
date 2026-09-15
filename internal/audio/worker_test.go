//go:build windows

package audio

import (
	"slices"
	"testing"
)

func ids(devices []Device) []string {
	out := make([]string, len(devices))
	for i, d := range devices {
		out[i] = d.ID
	}
	return out
}

func TestIncluded(t *testing.T) {
	all := []Device{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	if got, want := ids(included(all, map[string]bool{"b": true})), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("included(skip b) = %v, want %v", got, want)
	}
	if got := ids(included(all, nil)); !slices.Equal(got, ids(all)) {
		t.Errorf("included(nothing skipped) = %v, want all devices in order", got)
	}
}

func TestNextAfter(t *testing.T) {
	devices := []Device{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	tests := []struct{ current, want string }{
		{"a", "b"},
		{"b", "c"},
		{"c", "a"},       // wraps around
		{"skipped", "a"}, // current output isn't in the cycle: start over
		{"", "a"},
	}
	for _, tt := range tests {
		if got := nextAfter(devices, tt.current).ID; got != tt.want {
			t.Errorf("nextAfter(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}
