//go:build windows

package updater

import (
	"fmt"
	"os"
)

// userAgent identifies setup's requests to GitHub.
const userAgent = "AudioOutputSwitcher-Setup"

// LatestTag returns the tag of the newest published release, read from
// the redirect GitHub answers releases/latest with rather than from the
// rate-limited API.
func LatestTag() (string, error) {
	loc, err := redirectTarget(latestPageURL())
	if err != nil {
		return "", fmt.Errorf("contact GitHub: %w", err)
	}
	return tagFromLocation(loc)
}

// Download saves the file at url to destPath, writing to a temporary
// file first so a failed or interrupted download never leaves a
// half-written file at destPath.
func Download(url, destPath string) error {
	tmp := destPath + ".download"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := httpGet(url, nil, out); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
