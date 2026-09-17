package updater

import (
	"fmt"
	"strings"
)

// The release links below are GitHub's plain web URLs, not the GitHub
// API: the API's unauthenticated rate limit (60 requests an hour per
// address) makes installs fail on shared or busy networks. This file has
// no OS dependency, so its tests run anywhere.

const (
	// Owner and Repo identify the GitHub repository releases are
	// fetched from.
	Owner = "SpikePy"
	Repo  = "Windows-Audio-Output-Switcher"

	// AssetName is the exact release asset name the release build
	// publishes (see .github/workflows/build.yml).
	AssetName = "AudioOutputSwitcher.exe"
)

// latestPageURL redirects to the newest release's tag page, which is how
// setup learns the version it is about to install.
func latestPageURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest", Owner, Repo)
}

// LatestAssetURL redirects to asset in the newest release.
func LatestAssetURL(asset string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", Owner, Repo, asset)
}

// tagFromLocation returns the release tag from the address latestPageURL
// redirects to, which ends in "/releases/tag/<tag>".
func tagFromLocation(location string) (string, error) {
	loc, _, _ := strings.Cut(location, "#")
	loc, _, _ = strings.Cut(loc, "?")
	loc = strings.TrimRight(loc, "/")
	const marker = "/releases/tag/"
	i := strings.LastIndex(loc, marker)
	if i < 0 {
		return "", fmt.Errorf("unexpected redirect to %q: not a release page", location)
	}
	tag := loc[i+len(marker):]
	if tag == "" || strings.Contains(tag, "/") {
		return "", fmt.Errorf("unexpected redirect to %q: no tag in it", location)
	}
	return tag, nil
}
