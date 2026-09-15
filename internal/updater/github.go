package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// ReleaseAsset is one downloadable file attached to a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release is the subset of the GitHub releases API response this tool
// needs.
type Release struct {
	TagName string         `json:"tag_name"`
	Assets  []ReleaseAsset `json:"assets"`
}

// LatestRelease fetches metadata for the newest published release of
// Owner/Repo.
func LatestRelease() (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", Owner, Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AudioOutputSwitcher-Installer")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub returned %s: %s", resp.Status, body)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release info: %w", err)
	}
	return &release, nil
}

// FindAsset returns the asset with the given exact name, if present.
func (r *Release) FindAsset(name string) (*ReleaseAsset, bool) {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i], true
		}
	}
	return nil, false
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
