//go:build windows

package updater

import "testing"

func TestTagFromReleaseURL(t *testing.T) {
	tests := map[string]string{
		"https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases/tag/v1.0.0": "v1.0.0",
		"https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases":            "",
		"": "",
	}
	for loc, want := range tests {
		if got := tagFromReleaseURL(loc); got != want {
			t.Errorf("tagFromReleaseURL(%q) = %q, want %q", loc, got, want)
		}
	}
}

func TestAssetDownloadURL(t *testing.T) {
	got := AssetDownloadURL("v1.0.0", AssetName)
	want := "https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases/download/v1.0.0/AudioOutputSwitcher.exe"
	if got != want {
		t.Errorf("AssetDownloadURL() = %q, want %q", got, want)
	}
}

func TestSplitHTTPSURL(t *testing.T) {
	host, path, err := splitHTTPSURL("https://github.com/SpikePy/Repo/releases/latest")
	if err != nil || host != "github.com" || path != "/SpikePy/Repo/releases/latest" {
		t.Errorf("splitHTTPSURL = %q, %q, %v", host, path, err)
	}
	if host, path, err := splitHTTPSURL("https://example.com"); err != nil || host != "example.com" || path != "/" {
		t.Errorf("splitHTTPSURL(no path) = %q, %q, %v", host, path, err)
	}
	if _, _, err := splitHTTPSURL("http://example.com/x"); err == nil {
		t.Error("splitHTTPSURL accepted a plain http URL")
	}
}
