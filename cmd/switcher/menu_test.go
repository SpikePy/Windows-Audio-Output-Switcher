//go:build windows

package main

import "testing"

func TestStrikeThrough(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"every character gets a stroke", "abc", "a\u0336b\u0336c\u0336"},
		{"spaces are struck too, so the line doesn't break up", "a b", "a\u0336 \u0336b\u0336"},
		{"multi-byte characters count once", "Kopfhörer", "K\u0336o\u0336p\u0336f\u0336h\u0336ö\u0336r\u0336e\u0336r\u0336"},
		{"a combining mark rides along with its base character", "e\u0301x", "e\u0301\u0336x\u0336"},
	}
	for _, tt := range tests {
		if got := strikeThrough(tt.in); got != tt.want {
			t.Errorf("%s: strikeThrough(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
