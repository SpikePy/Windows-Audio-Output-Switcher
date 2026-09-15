//go:build windows

package audio

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/go-ole/go-ole"
)

// SwitchResult describes the outcome of one "switch the active device"
// request.
type SwitchResult struct {
	Device   Device
	Switched bool // false when there was nothing to switch to, or the requested device was already active
	Err      error
}

// Worker owns a single OS thread with an initialized COM apartment.
// Every Core Audio COM call must happen on a thread where CoInitializeEx
// has run, so all audio work is funneled through this one goroutine
// instead of initializing COM ad-hoc on whichever goroutine happens to
// need it.
type Worker struct {
	tasks chan func()
	quit  chan struct{}
}

// StartWorker launches the worker goroutine and returns immediately.
func StartWorker() *Worker {
	w := &Worker{
		tasks: make(chan func()),
		quit:  make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *Worker) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Errors here are intentionally ignored: if COM is already
	// initialized on this thread (unexpected but harmless), the calls
	// below still work fine and will surface their own errors.
	_ = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	for {
		select {
		case task := <-w.tasks:
			task()
		case <-w.quit:
			return
		}
	}
}

// do runs task on the worker's COM thread and blocks until it's done.
func (w *Worker) do(task func()) {
	done := make(chan struct{})
	w.tasks <- func() {
		task()
		close(done)
	}
	<-done
}

// Next switches to the next playback device in the list, wrapping
// around after the last one.
func (w *Worker) Next() SwitchResult {
	var result SwitchResult
	w.do(func() { result = cycleNext() })
	return result
}

// SwitchTo makes the device identified by id the active one directly.
func (w *Worker) SwitchTo(id string) SwitchResult {
	var result SwitchResult
	w.do(func() { result = switchToID(id) })
	return result
}

// List returns every currently active playback device.
func (w *Worker) List() ([]Device, error) {
	var devices []Device
	var err error
	w.do(func() { devices, err = List() })
	return devices, err
}

// Current returns the currently active playback device.
func (w *Worker) Current() (Device, error) {
	var current Device
	var err error
	w.do(func() { current, err = Current() })
	return current, err
}

// Stop shuts the worker goroutine down.
func (w *Worker) Stop() {
	close(w.quit)
}

func cycleNext() SwitchResult {
	devices, err := List()
	if err != nil {
		return SwitchResult{Err: fmt.Errorf("list devices: %w", err)}
	}
	if len(devices) == 0 {
		return SwitchResult{Err: errors.New("no active playback devices found")}
	}
	if len(devices) == 1 {
		return SwitchResult{Device: devices[0]}
	}

	current, err := Current()
	if err != nil {
		return SwitchResult{Err: fmt.Errorf("get current device: %w", err)}
	}

	nextIndex := 0
	for i, d := range devices {
		if d.ID == current.ID {
			nextIndex = (i + 1) % len(devices)
			break
		}
	}

	next := devices[nextIndex]
	if err := SetDefault(next.ID); err != nil {
		return SwitchResult{Err: fmt.Errorf("set default device: %w", err)}
	}
	return SwitchResult{Device: next, Switched: true}
}

func switchToID(id string) SwitchResult {
	devices, err := List()
	if err != nil {
		return SwitchResult{Err: fmt.Errorf("list devices: %w", err)}
	}

	var target *Device
	for i := range devices {
		if devices[i].ID == id {
			target = &devices[i]
			break
		}
	}
	if target == nil {
		return SwitchResult{Err: errors.New("selected device is no longer available")}
	}

	if current, err := Current(); err == nil && current.ID == target.ID {
		return SwitchResult{Device: *target}
	}

	if err := SetDefault(target.ID); err != nil {
		return SwitchResult{Err: fmt.Errorf("set default device: %w", err)}
	}
	return SwitchResult{Device: *target, Switched: true}
}
