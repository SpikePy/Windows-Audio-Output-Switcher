package updater

import "testing"

func TestTagFromLocation(t *testing.T) {
	good := []struct{ location, want string }{
		{"https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases/tag/v1.1.1", "v1.1.1"},
		{"https://github.com/o/r/releases/tag/v2.0.0/", "v2.0.0"},
		{"https://github.com/o/r/releases/tag/v2.0.0?expanded=true#assets", "v2.0.0"},
		{"/o/r/releases/tag/v0.0.1", "v0.0.1"},
		{"https://github.com/o/r/releases/tag/2026.09-rc1", "2026.09-rc1"},
	}
	for _, tt := range good {
		got, err := tagFromLocation(tt.location)
		if err != nil || got != tt.want {
			t.Errorf("tagFromLocation(%q) = %q, %v; want %q", tt.location, got, err, tt.want)
		}
	}

	bad := []string{
		"",
		"https://github.com/o/r/releases",      // a repository without releases
		"https://github.com/o/r/releases/tag/", // no tag
		"https://github.com/o/r/releases/tag/v1/extra",
		"https://github.com/login?return_to=%2Freleases%2Ftag%2Fv1",
	}
	for _, location := range bad {
		if got, err := tagFromLocation(location); err == nil {
			t.Errorf("tagFromLocation(%q) = %q, want an error", location, got)
		}
	}
}

func TestLatestURLs(t *testing.T) {
	if got, want := latestPageURL(), "https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases/latest"; got != want {
		t.Errorf("latestPageURL() = %q, want %q", got, want)
	}
	if got, want := LatestAssetURL(AssetName),
		"https://github.com/SpikePy/Windows-Audio-Output-Switcher/releases/latest/download/AudioOutputSwitcher.exe"; got != want {
		t.Errorf("LatestAssetURL() = %q, want %q", got, want)
	}
}
