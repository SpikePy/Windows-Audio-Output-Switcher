package install

import (
	"testing"
	"time"
)

func TestSecondsLeft(t *testing.T) {
	const five = 5 * time.Second
	ms := time.Millisecond
	tests := []struct {
		elapsed  time.Duration
		wantSecs int
		wantDone bool
	}{
		{-time.Second, 5, false}, // a clock that went backwards counts as not started
		{0, 5, false},
		{1 * ms, 5, false},
		{999 * ms, 5, false},
		{time.Second, 4, false},
		{2500 * ms, 3, false},
		{4 * time.Second, 1, false},
		{4999 * ms, 1, false},
		{five, 0, true},
		{five + 1*ms, 0, true},
		{time.Minute, 0, true}, // a long-stalled dialog still ends the countdown
	}
	for _, tt := range tests {
		secs, done := SecondsLeft(five, tt.elapsed)
		if secs != tt.wantSecs || done != tt.wantDone {
			t.Errorf("SecondsLeft(5s, %v) = %d, %t; want %d, %t", tt.elapsed, secs, done, tt.wantSecs, tt.wantDone)
		}
	}
}

func TestCountdownTexts(t *testing.T) {
	if got, want := AutoInstallText(5), "Installing/updating automatically in 5 s..."; got != want {
		t.Errorf("AutoInstallText(5) = %q, want %q", got, want)
	}
	if got, want := AutoCloseText(1), "Closing in 1 s..."; got != want {
		t.Errorf("AutoCloseText(1) = %q, want %q", got, want)
	}
	if secs, _ := SecondsLeft(AutoInstallAfter, 0); AutoInstallText(secs) != "Installing/updating automatically in 5 s..." {
		t.Errorf("the auto-install countdown doesn't start at 5 s")
	}
	if secs, _ := SecondsLeft(AutoCloseAfter, 0); AutoCloseText(secs) != "Closing in 5 s..." {
		t.Errorf("the auto-close countdown doesn't start at 5 s")
	}
}
