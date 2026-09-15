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

func writeConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	withTempAppData(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
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

func TestLoadMalformedFileReportsError(t *testing.T) {
	withTempAppData(t)
	writeConfig(t, "outputs: [unterminated")

	c, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded on a malformed file, want an error")
	}
	if len(c.Devices) != 0 || c.EffectiveHotkey() != DefaultHotkey {
		t.Errorf("Load() = %+v, want defaults alongside the error", c)
	}
}

func TestLoadSkipsRowsWithoutID(t *testing.T) {
	withTempAppData(t)
	writeConfig(t, "outputs:\n  - alias: No ID\n  - alias: Desk\n    id: dev-1\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Devices) != 1 || c.Devices["dev-1"].Alias != "Desk" {
		t.Errorf("Devices = %+v, want only the row with an ID", c.Devices)
	}
}

func TestLoadKeepsOnlyTheDateOfATimestamp(t *testing.T) {
	withTempAppData(t)
	// Just after midnight in +02:00 is still the previous day in UTC; the
	// date as written is the one that counts.
	writeConfig(t, "outputs:\n  - id: dev-1\n    last_seen: 2026-09-16T00:30:00+02:00\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Devices["dev-1"].LastSeen.Format(time.DateOnly); got != "2026-09-16" {
		t.Errorf("LastSeen = %s, want 2026-09-16", got)
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

	active := []Device{{ID: "dev-1", Name: "Speakers"}, {ID: "dev-2", Name: "Headphones"}}
	written, err := Sync("ctrl+alt+f9", 15, active, nil)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Hotkey != "ctrl+alt+f9" || loaded.PollSeconds != 15 {
		t.Errorf("reloaded hotkey/poll = %q/%d, want %q/%d", loaded.Hotkey, loaded.PollSeconds, "ctrl+alt+f9", 15)
	}
	if len(loaded.Devices) != len(written) {
		t.Fatalf("reloaded %d devices, want %d", len(loaded.Devices), len(written))
	}
	for id, want := range written {
		got := loaded.Devices[id]
		if got.ID != id || got.Alias != want.Alias || got.Skip != want.Skip || !got.LastSeen.Equal(want.LastSeen.Time) {
			t.Errorf("reloaded %q = %+v, want %+v", id, got, want)
		}
	}

	data := readConfig(t)
	if !strings.HasPrefix(data, "# Audio Output Switcher") {
		t.Error("written file is missing its explanatory header")
	}
	if strings.Contains(data, "name:") {
		t.Error("written file still has a name field")
	}
	if _, err := os.Stat(Path() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}

func TestSyncWritesLastSeenAsADate(t *testing.T) {
	withTempAppData(t)
	if _, err := Sync("", 0, []Device{{ID: "dev-1", Name: "Speakers"}}, nil); err != nil {
		t.Fatal(err)
	}

	data := readConfig(t)
	if want := "last_seen: " + time.Now().Format(time.DateOnly) + "\n"; !strings.Contains(data, want) {
		t.Errorf("file doesn't contain %q:\n%s", want, data)
	}
}

func TestSyncAddsNewDevicesAndKeepsDisconnectedOnes(t *testing.T) {
	withTempAppData(t)
	past := dateOf(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC))
	current := map[string]Entry{
		"old": {ID: "old", Alias: "Living room", Skip: true, LastSeen: past},
	}

	got, err := Sync("", 0, []Device{{ID: "new", Name: "Headset"}}, current)
	if err != nil {
		t.Fatal(err)
	}

	if old := got["old"]; old.Alias != "Living room" || !old.Skip || !old.LastSeen.Equal(past.Time) {
		t.Errorf("disconnected device = %+v, want its settings and LastSeen kept", old)
	}
	added, ok := got["new"]
	if !ok {
		t.Fatal("newly seen device was not added")
	}
	if added.Alias != "Headset" || added.Skip || !added.LastSeen.After(past.Time) {
		t.Errorf("new device = %+v, want alias defaulted to its Windows name, not skipped, LastSeen stamped", added)
	}
}

func TestSyncKeepsSameNamedDevicesApart(t *testing.T) {
	withTempAppData(t)
	current := map[string]Entry{"a": {ID: "a", Alias: "Front", Skip: true}}
	active := []Device{{ID: "a", Name: "Speakers"}, {ID: "b", Name: "Speakers"}}

	got, err := Sync("", 0, active, current)
	if err != nil {
		t.Fatal(err)
	}
	if a := got["a"]; a.Alias != "Front" || !a.Skip {
		t.Errorf("device a = %+v, want its own alias and skip kept", a)
	}
	if b := got["b"]; b.Alias != "Speakers" || b.Skip {
		t.Errorf("device b = %+v, want a fresh entry of its own", b)
	}
}

func TestSyncFillsOnlyBlankAliases(t *testing.T) {
	withTempAppData(t)
	current := map[string]Entry{"a": {Alias: "Desk"}, "b": {}}
	active := []Device{{ID: "a", Name: "Speakers"}, {ID: "b", Name: "Headset"}}

	got, err := Sync("", 0, active, current)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Alias != "Desk" || got["b"].Alias != "Headset" {
		t.Errorf("aliases = %q/%q, want the custom one kept and the blank one filled", got["a"].Alias, got["b"].Alias)
	}
}

func TestSyncWritesEntryKeysInOrder(t *testing.T) {
	withTempAppData(t)
	if _, err := Sync("", 0, []Device{{ID: "dev-1", Name: "Speakers"}}, nil); err != nil {
		t.Fatal(err)
	}

	data := readConfig(t)
	start := strings.Index(data, "\noutputs:")
	if start < 0 {
		t.Fatalf("no outputs section in:\n%s", data)
	}
	outputs := data[start:]
	last := -1
	for _, key := range []string{"id:", "alias:", "last_seen:", "skip:"} {
		i := strings.Index(outputs, key)
		if i <= last {
			t.Fatalf("key %q missing or out of order in:\n%s", key, outputs)
		}
		last = i
	}
}

func TestDisplayName(t *testing.T) {
	entries := map[string]Entry{
		"a": {ID: "a", Alias: "Desk"},
		"b": {ID: "b"},
	}
	tests := []struct{ id, windowsName, want string }{
		{"a", "Speakers", "Desk"},
		{"b", "Headset", "Headset"},
		{"c", "Unknown", "Unknown"},
	}
	for _, tt := range tests {
		if got := DisplayName(entries, tt.id, tt.windowsName); got != tt.want {
			t.Errorf("DisplayName(%q, %q) = %q, want %q", tt.id, tt.windowsName, got, tt.want)
		}
	}
}
