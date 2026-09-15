//go:build windows

// Package notifier shows native Windows toast notifications.
package notifier

import "github.com/go-toast/toast"

const appID = "Audio Output Switcher"

// Show displays a toast notification with the given title and message.
func Show(title, message string) error {
	n := toast.Notification{
		AppID:    appID,
		Title:    title,
		Message:  message,
		Audio:    toast.Default,
		Duration: toast.Short,
	}
	return n.Push()
}
