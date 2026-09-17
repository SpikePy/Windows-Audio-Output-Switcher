package install

import (
	"fmt"
	"time"
)

// Setup's window counts down twice: before installing by itself when
// nobody clicks anything, and before closing after a success. The
// arithmetic lives here, with no OS dependency, so its tests run anywhere.

const (
	AutoInstallAfter = 5 * time.Second
	AutoCloseAfter   = 5 * time.Second
)

// SecondsLeft returns the whole seconds to show once elapsed has passed
// of a countdown of the given length - rounded up, so the full length
// shows for the whole first second - and whether the countdown is over.
// It works from elapsed time rather than counted ticks, so late or
// skipped timer ticks can't stretch it.
func SecondsLeft(length, elapsed time.Duration) (secs int, done bool) {
	elapsed = max(elapsed, 0)
	if elapsed >= length {
		return 0, true
	}
	left := length - elapsed
	return int((left + time.Second - 1) / time.Second), false
}

// AutoInstallText is page one's countdown line.
func AutoInstallText(secs int) string {
	return fmt.Sprintf("Installing/updating automatically in %d s...", secs)
}

// AutoCloseText is the result page's countdown line after a success.
func AutoCloseText(secs int) string {
	return fmt.Sprintf("Closing in %d s...", secs)
}
