//go:build windows

package updater

import (
	"fmt"
	"os"
	"strings"
)

// Release identifies the newest published release of Owner/Repo.
type Release struct {
	TagName string
}

// LatestRelease resolves the tag name of the newest published release of
// Owner/Repo via the plain release-page redirect rather than the JSON
// REST API: the API's unauthenticated rate limit (60 requests/hour) is
// per source IP, so it's shared by every install/update behind the same
// NAT/office network and gets exhausted easily, whereas this redirect
// isn't subject to that limit. The tag is read straight off the
// redirect's Location header instead of fetching the release page.
func LatestRelease() (*Release, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", Owner, Repo)
	status, loc, err := redirectLocation(url)
	if err != nil {
		return nil, fmt.Errorf("contact GitHub: %w", err)
	}

	tag := tagFromReleaseURL(loc)
	if tag == "" {
		return nil, fmt.Errorf("could not resolve latest release tag (GitHub returned status %d, Location %q)", status, loc)
	}
	return &Release{TagName: tag}, nil
}

// tagFromReleaseURL extracts the tag name from a
// https://github.com/OWNER/REPO/releases/tag/TAG URL.
func tagFromReleaseURL(loc string) string {
	const marker = "/releases/tag/"
	i := strings.Index(loc, marker)
	if i == -1 {
		return ""
	}
	return loc[i+len(marker):]
}

// AssetDownloadURL returns the direct download URL for a named asset
// attached to release tag - GitHub serves these without going through
// the rate-limited API.
func AssetDownloadURL(tag, assetName string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", Owner, Repo, tag, assetName)
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
	if err := download(url, out); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("download: %w", err)
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
