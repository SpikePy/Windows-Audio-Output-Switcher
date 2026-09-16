// Package setupmenu is the decision logic behind setup's window, kept
// free of Win32 so it can be tested anywhere: which action runs, the
// countdown that picks install/update when nobody chooses, and the
// countdown that closes the window after an unattended success.
package setupmenu

import "fmt"

// Action is what setup was asked to do.
type Action int

const (
	None Action = iota
	Install
	Uninstall
)

const (
	// AutoChoiceSeconds is how long the window waits for a choice before
	// installing/updating on its own - the common case, rather than
	// doing nothing when setup is double-clicked and left alone.
	AutoChoiceSeconds = 5
	// AutoCloseSeconds is how long the window stays open after an
	// unattended install succeeded, so a fully automated run needs no
	// interaction at all.
	AutoCloseSeconds = 3
)

type state int

const (
	choosing state = iota
	running
	done
)

// Event is what the window has to do after a Tick.
type Event int

const (
	Nothing Event = iota
	Start         // run Flow.Action()
	Close         // close the window
)

// Flow tracks setup's window from choosing through running to done.
// Its zero value isn't usable; start with New.
type Flow struct {
	state     state
	action    Action
	auto      bool // the action was picked by the countdown, not the user
	failed    bool
	remaining int // seconds left on whichever countdown is running
}

// New returns a Flow waiting for a choice, its countdown running.
func New() *Flow {
	return &Flow{remaining: AutoChoiceSeconds}
}

// Tick advances the running countdown by one second.
func (f *Flow) Tick() Event {
	switch {
	case f.state == choosing:
		f.remaining--
		if f.remaining <= 0 {
			f.start(Install, true)
			return Start
		}
	case f.state == done && f.closing():
		f.remaining--
		if f.remaining <= 0 {
			return Close
		}
	}
	return Nothing
}

// Choose starts a when the user picks it. It reports false, doing
// nothing, once an action is already under way.
func (f *Flow) Choose(a Action) bool {
	if f.state != choosing || a == None {
		return false
	}
	f.start(a, false)
	return true
}

func (f *Flow) start(a Action, auto bool) {
	f.state, f.action, f.auto, f.remaining = running, a, auto, 0
}

// Finish records how the action ended. After an unattended success the
// close countdown starts; a failure keeps the window open so the error
// stays readable.
func (f *Flow) Finish(err error) {
	f.state, f.failed = done, err != nil
	if f.closing() {
		f.remaining = AutoCloseSeconds
	}
}

func (f *Flow) closing() bool {
	return f.auto && !f.failed
}

// Action is the action chosen, or None while still choosing.
func (f *Flow) Action() Action { return f.action }

// Choosing reports whether no action has been picked yet.
func (f *Flow) Choosing() bool { return f.state == choosing }

// Running reports whether the action is under way.
func (f *Flow) Running() bool { return f.state == running }

// Done reports whether the action has finished.
func (f *Flow) Done() bool { return f.state == done }

// Succeeded reports whether the action finished without an error.
func (f *Flow) Succeeded() bool { return f.state == done && !f.failed }

// InstallLabel is the text of the default button, carrying the
// countdown while it runs.
func (f *Flow) InstallLabel() string {
	if f.state == choosing {
		return fmt.Sprintf("&Install / update (%d)", f.remaining)
	}
	return "&Install / update"
}

// CloseLabel is the text of the Close button, carrying the close
// countdown while it runs.
func (f *Flow) CloseLabel() string {
	if f.state == done && f.closing() {
		return fmt.Sprintf("Close (%d)", f.remaining)
	}
	return "Close"
}
