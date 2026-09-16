package setupmenu

import (
	"errors"
	"testing"
)

func tick(t *testing.T, f *Flow, n int, want Event) {
	t.Helper()
	for i := 1; i <= n; i++ {
		got := f.Tick()
		expected := Nothing
		if i == n {
			expected = want
		}
		if got != expected {
			t.Fatalf("tick %d/%d = %v, want %v", i, n, got, expected)
		}
	}
}

func TestCountdownInstallsAndThenClosesOnItsOwn(t *testing.T) {
	f := New()
	if got := f.InstallLabel(); got != "&Install / update (5)" {
		t.Errorf("InstallLabel() = %q at the start", got)
	}
	tick(t, f, AutoChoiceSeconds, Start)
	if f.Action() != Install || !f.Running() {
		t.Fatalf("after the countdown: action %v, running %v; want Install, true", f.Action(), f.Running())
	}
	if got := f.InstallLabel(); got != "&Install / update" {
		t.Errorf("InstallLabel() = %q once running, want no countdown", got)
	}

	f.Finish(nil)
	if got := f.CloseLabel(); got != "Close (3)" {
		t.Errorf("CloseLabel() = %q after an unattended success", got)
	}
	tick(t, f, AutoCloseSeconds, Close)
}

func TestUnattendedFailureStaysOpen(t *testing.T) {
	f := New()
	tick(t, f, AutoChoiceSeconds, Start)
	f.Finish(errors.New("offline"))

	if f.Succeeded() {
		t.Error("Succeeded() = true after a failure")
	}
	if got := f.CloseLabel(); got != "Close" {
		t.Errorf("CloseLabel() = %q, want no countdown after a failure", got)
	}
	for i := 0; i < 10; i++ {
		if got := f.Tick(); got != Nothing {
			t.Fatalf("tick %d after a failure = %v, want Nothing", i+1, got)
		}
	}
}

func TestUserChoiceStopsTheCountdownAndNeverAutoCloses(t *testing.T) {
	for _, a := range []Action{Install, Uninstall} {
		f := New()
		f.Tick()
		if !f.Choose(a) {
			t.Fatalf("Choose(%v) refused while choosing", a)
		}
		if f.Action() != a {
			t.Errorf("Action() = %v, want %v", f.Action(), a)
		}
		for i := 0; i < 10; i++ {
			if got := f.Tick(); got != Nothing {
				t.Fatalf("%v: tick while running = %v, want Nothing", a, got)
			}
		}
		f.Finish(nil)
		if !f.Succeeded() || f.CloseLabel() != "Close" {
			t.Errorf("%v: succeeded %v, close label %q; want true, %q", a, f.Succeeded(), f.CloseLabel(), "Close")
		}
		if got := f.Tick(); got != Nothing {
			t.Errorf("%v: tick after a chosen action = %v, want Nothing", a, got)
		}
	}
}

func TestChooseOnlyOnce(t *testing.T) {
	f := New()
	if f.Choose(None) {
		t.Error("Choose(None) accepted")
	}
	f.Choose(Uninstall)
	if f.Choose(Install) {
		t.Error("a second Choose was accepted while running")
	}
	f.Finish(nil)
	if f.Choose(Install) {
		t.Error("Choose was accepted after finishing")
	}
	if f.Action() != Uninstall {
		t.Errorf("Action() = %v, want the first choice", f.Action())
	}
}
