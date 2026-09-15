//go:build windows

package audio

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/go-ole/go-ole"
)

// SwitchResult describes the outcome of one "switch to the next device"
// request.
type SwitchResult struct {
	Device   Device
	Switched bool // false when there was nothing to switch to
	Err      error
}

// Worker owns a single OS thread with an initialized COM apartment.
// Every Core Audio COM call must happen on a thread where CoInitializeEx
// has run, so all audio work is funneled through this one goroutine
// instead of initializing COM ad-hoc on whichever goroutine happens to
// need it.
type Worker struct {
	requests chan chan SwitchResult
	quit     chan struct{}
}

// StartWorker launches the worker goroutine and returns immediately.
func StartWorker() *Worker {
	w := &Worker{
		requests: make(chan chan SwitchResult),
		quit:     make(chan struct{}),
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
		case reply := <-w.requests:
			reply <- cycleNext()
		case <-w.quit:
			return
		}
	}
}

// Next asks the worker to switch to the next playback device and blocks
// until it has done so.
func (w *Worker) Next() SwitchResult {
	reply := make(chan SwitchResult, 1)
	w.requests <- reply
	return <-reply
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
