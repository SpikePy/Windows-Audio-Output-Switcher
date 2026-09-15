package outputconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTempAppData(t *testing.T) {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
}

func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	withTempAppData(t)

	c := Load()
	if len(c.Devices) != 0 {
		t.Errorf("Devices = %v, want none", c.Devices)
	}
	if got := c.EffectiveHotkey(); got != DefaultHotkey {
		t.Errorf("EffectiveHotkey() = %q, want %q", got, DefaultHotkey)
	}
	if got, want := c.EffectivePollInterval(), DefaultPollSeconds*time.Second; got != want {
		t.Errorf("EffectivePollInterval() = %v, want %v", got, want)
	}
}

func TestLoadMalformedFileUsesDefaults(t *testing.T) {
	withTempAppData(t)
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("outputs: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}

	if c := Load(); len(c.Devices) != 0 || c.Hotkey != "" || c.PollSeconds != 0 {
		t.Errorf("Load() = %+v, want an empty config", c)
	}
}

func TestEffectiveSettings(t *testing.T) {
	tests := []struct {
		cfg        Config
		wantHotkey string
		wantPoll   time.Duration
	}{
		{Config{}, DefaultHotkey, DefaultPollSeconds * time.Second},
		{Config{Hotkey: "ctrl+alt+f9", PollSeconds: 15}, "ctrl+alt+f9", 15 * time.Second},
		{Config{PollSeconds: -3}, DefaultHotkey, DefaultPollSeconds * time.Second},
	}
	for _, tt := range tests {
		if got := tt.cfg.EffectiveHotkey(); got != tt.wantHotkey {
			t.Errorf("%+v.EffectiveHotkey() = %q, want %q", tt.cfg, got, tt.wantHotkey)
		}
		if got := tt.cfg.EffectivePollInterval(); got != tt.wantPoll {
			t.Errorf("%+v.EffectivePollInterval() = %v, want %v", tt.cfg, got, tt.wantPoll)
		}
	}
}

func TestSyncRoundTrip(t *testing.T) {
	withTempAppData(t)

	written, err := Sync("ctrl+alt+f9", 15, []string{"Speakers", "Headphones"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	loaded := Load()
	if loaded.Hotkey != "ctrl+alt+f9" || loaded.PollSeconds != 15 {
		t.Errorf("reloaded hotkey/poll = %q/%d, want %q/%d", loaded.Hotkey, loaded.PollSeconds, "ctrl+alt+f9", 15)
	}
	if len(loaded.Devices) != len(written) {
		t.Fatalf("reloaded %d devices, want %d", len(loaded.Devices), len(written))
	}
	for name, want := range written {
		got := loaded.Devices[name]
		if got.Name != want.Name || got.Alias != want.Alias || got.Skip != want.Skip || !got.LastSeen.Equal(want.LastSeen) {
			t.Errorf("reloaded %q = %+v, want %+v", name, got, want)
		}
	}

	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Audio Output Switcher") {
		t.Error("written file is missing its explanatory header")
	}
}

func TestSyncAddsNewDevicesAndKeepsDisconnectedOnes(t *testing.T) {
	withTempAppData(t)
	past := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	current := map[string]Entry{
		"Old TV": {Name: "Old TV", Alias: "Living room", Skip: true, LastSeen: past},
	}

	got, err := Sync("", 0, []string{"Headset"}, current)
	if err != nil {
		t.Fatal(err)
	}

	if old := got["Old TV"]; old.Alias != "Living room" || !old.Skip || !old.LastSeen.Equal(past) {
		t.Errorf("disconnected device = %+v, want its settings and LastSeen kept", old)
	}
	added, ok := got["Headset"]
	if !ok {
		t.Fatal("newly seen device was not added")
	}
	if added.Alias != "Headset" || added.Skip || !added.LastSeen.After(past) {
		t.Errorf("new device = %+v, want alias defaulted to its name, not skipped, LastSeen stamped", added)
	}
}

func TestSyncFillsEmptyAlias(t *testing.T) {
	withTempAppData(t)

	got, err := Sync("", 0, []string{"Speakers"}, map[string]Entry{"Speakers": {Name: "Speakers"}})
	if err != nil {
		t.Fatal(err)
	}
	if alias := got["Speakers"].Alias; alias != "Speakers" {
		t.Errorf("alias = %q, want it defaulted to the device name", alias)
	}
}

func TestDisplayName(t *testing.T) {
	entries := map[string]Entry{
		"Speakers": {Name: "Speakers", Alias: "Desk"},
		"Headset":  {Name: "Headset"},
	}
	for name, want := range map[string]string{
		"Speakers": "Desk",
		"Headset":  "Headset",
		"Unknown":  "Unknown",
	} {
		if got := DisplayName(entries, name); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", name, got, want)
		}
	}
}
