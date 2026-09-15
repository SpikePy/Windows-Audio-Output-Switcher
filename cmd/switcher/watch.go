//go:build windows

package main

import (
	"log"
	"time"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/audio"
)

// deviceChangeSettle is how long to wait after a device notification
// before refreshing, so a burst of them (e.g. one default-changed per
// audio role) causes a single refresh.
const deviceChangeSettle = 300 * time.Millisecond

// watchDeviceChanges refreshes the device menu as soon as Windows reports
// a device change (plugged in, unplugged, enabled/disabled, or the default
// output changed elsewhere), and every a.pollInterval re-checks the config
// file for hand-edits - plus the devices again, as a fallback in case
// notifications aren't available.
func (a *app) watchDeviceChanges() {
	changes, err := a.worker.WatchChanges()
	if err != nil {
		log.Printf("device change notifications unavailable, relying on polling: %v", err)
	}

	interval := a.currentPollInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-changes:
			time.Sleep(deviceChangeSettle)
			select {
			case <-changes:
			default:
			}
			// Switching an output makes Windows report the change too, but
			// switchOutput/switchTo already refreshed the menu right after
			// their own switch - only refresh if nothing has since the
			// latest change.
			if a.refreshedSince(audio.LastChange()) {
				continue
			}
			a.syncDeviceMenu()
		case <-ticker.C:
			a.reloadConfigIfChanged()
			a.syncDeviceMenu()

			if newInterval := a.currentPollInterval(); newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
			}
		}
	}
}

// refreshedSince reports whether syncDeviceMenu has started since t.
func (a *app) refreshedSince(t time.Time) bool {
	return a.lastRefresh.Load() > t.UnixNano()
}
