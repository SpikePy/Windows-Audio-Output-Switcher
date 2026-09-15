package updater

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Release identifies the newest published release of Owner/Repo.
type Release struct {
	TagName string
}

// LatestRelease resolves the tag name of the newest published release of
// Owner/Repo via the plain release-page redirect rather than the JSON
// REST API: the API's unauthenticated rate limit (60 requests/hour) is
// per source IP, so it's shared by every install/update behind the same
// NAT/office network and gets exhausted easily, whereas this redirect
// isn't subject to that limit.
func LatestRelease() (*Release, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", Owner, Repo)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AudioOutputSwitcher-Installer")

	// Don't follow the redirect - the tag name is read straight off its
	// Location header instead of fetching the (large, HTML) release page.
	noRedirect := &http.Client{
		Timeout: httpClient.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact GitHub: %w", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	tag := tagFromReleaseURL(loc)
	if tag == "" {
		return nil, fmt.Errorf("could not resolve latest release tag (GitHub returned %s, Location %q)", resp.Status, loc)
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
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AudioOutputSwitcher-Installer")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %s", resp.Status)
	}

	tmp := destPath + ".download"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("save download: %w", err)
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
