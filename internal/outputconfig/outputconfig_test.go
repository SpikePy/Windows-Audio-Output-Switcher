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
	t.Setenv("LOCALAPPDATA", t.TempDir())
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
	if c.Exists {
		t.Error("Exists = true, want false without a config file")
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

func writeLegacyConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(LegacyDir(), "config.yaml")
	if err := os.MkdirAll(LegacyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateLegacyMovesTheOldConfig(t *testing.T) {
	withTempAppData(t)
	legacy := writeLegacyConfig(t, "hotkey: ctrl+f9\n")

	if err := MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t); got != "hotkey: ctrl+f9\n" {
		t.Errorf("migrated config = %q, want the old file's contents", got)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("old config still there: %v", err)
	}
}

func TestMigrateLegacyKeepsAnExistingConfig(t *testing.T) {
	withTempAppData(t)
	writeConfig(t, "hotkey: win+a\n")
	writeLegacyConfig(t, "hotkey: ctrl+f9\n")

	if err := MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t); got != "hotkey: win+a\n" {
		t.Errorf("config = %q, want the current file kept", got)
	}
}

func TestMigrateLegacyWithoutOldConfig(t *testing.T) {
	withTempAppData(t)

	if err := MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Errorf("MigrateLegacy created a config out of nothing: %v", err)
	}
}

func TestHotkeyEnabled(t *testing.T) {
	tests := map[string]bool{
		"win+a":       true,
		"ctrl+alt+f9": true,
		"":            false,
		"   ":         false,
		"disabled":    false,
		"Disabled":    false,
		"  DISABLED ": false,
	}
	for combo, want := range tests {
		if got := HotkeyEnabled(combo); got != want {
			t.Errorf("HotkeyEnabled(%q) = %v, want %v", combo, got, want)
		}
	}
}

func TestEffectiveHotkey(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		want        string
		wantEnabled bool
	}{
		{"no config file yet", Config{}, DefaultHotkey, true},
		{"custom hotkey", Config{Exists: true, Hotkey: "ctrl+alt+f9"}, "ctrl+alt+f9", true},
		{"emptied out", Config{Exists: true}, "", false},
		{"switched off by name", Config{Exists: true, Hotkey: HotkeyDisabled}, HotkeyDisabled, false},
	}
	for _, tt := range tests {
		got := tt.cfg.EffectiveHotkey()
		if got != tt.want {
			t.Errorf("%s: EffectiveHotkey() = %q, want %q", tt.name, got, tt.want)
		}
		if enabled := HotkeyEnabled(got); enabled != tt.wantEnabled {
			t.Errorf("%s: HotkeyEnabled(%q) = %v, want %v", tt.name, got, enabled, tt.wantEnabled)
		}
	}
}

func TestLoadTreatsAMissingHotkeyLineAsOff(t *testing.T) {
	withTempAppData(t)
	writeConfig(t, "poll_seconds: 30\noutputs:\n  - id: dev-1\n    alias: Desk\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Exists {
		t.Fatal("Exists = false, want true for a file that parsed")
	}
	if got := c.EffectiveHotkey(); HotkeyEnabled(got) {
		t.Errorf("EffectiveHotkey() = %q, want a value that switches the hotkey off", got)
	}
}

func TestEffectiveAutostart(t *testing.T) {
	off, on := false, true
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"no config file yet", Config{}, true},
		{"no autostart line", Config{Exists: true}, true},
		{"switched on", Config{Exists: true, Autostart: &on}, true},
		{"switched off", Config{Exists: true, Autostart: &off}, false},
	}
	for _, tt := range tests {
		if got := tt.cfg.EffectiveAutostart(); got != tt.want {
			t.Errorf("%s: EffectiveAutostart() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLoadReadsAutostart(t *testing.T) {
	withTempAppData(t)
	writeConfig(t, "hotkey: win+a\nautostart: false\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.EffectiveAutostart() {
		t.Error("EffectiveAutostart() = true, want false as written in the file")
	}
}

func TestEffectivePollInterval(t *testing.T) {
	tests := []struct {
		cfg  Config
		want time.Duration
	}{
		{Config{}, DefaultPollSeconds * time.Second},
		{Config{PollSeconds: 15}, 15 * time.Second},
		{Config{PollSeconds: -3}, DefaultPollSeconds * time.Second},
	}
	for _, tt := range tests {
		if got := tt.cfg.EffectivePollInterval(); got != tt.want {
			t.Errorf("%+v.EffectivePollInterval() = %v, want %v", tt.cfg, got, tt.want)
		}
	}
}

func TestSettingsWithOverrides(t *testing.T) {
	file := Settings{Hotkey: "win+a", Autostart: true, PollSeconds: 60}
	hotkey, autostart, poll := "ctrl+f9", false, 5

	if got := file.With(Overrides{}); got != file {
		t.Errorf("With(no overrides) = %+v, want %+v", got, file)
	}
	want := Settings{Hotkey: "ctrl+f9", Autostart: false, PollSeconds: 5}
	if got := file.With(Overrides{Hotkey: &hotkey, Autostart: &autostart, PollSeconds: &poll}); got != want {
		t.Errorf("With(all overrides) = %+v, want %+v", got, want)
	}
	if got := file.With(Overrides{Autostart: &autostart}); got.Hotkey != "win+a" || got.Autostart || got.PollSeconds != 60 {
		t.Errorf("With(autostart only) = %+v, want only autostart changed", got)
	}
}

func TestConfigSettingsFillsDefaults(t *testing.T) {
	want := Settings{Hotkey: DefaultHotkey, Autostart: true, PollSeconds: DefaultPollSeconds}
	if got := (Config{}).Settings(); got != want {
		t.Errorf("Config{}.Settings() = %+v, want %+v", got, want)
	}
}

func TestSyncRoundTrip(t *testing.T) {
	withTempAppData(t)

	active := []Device{{ID: "dev-1", Name: "Speakers"}, {ID: "dev-2", Name: "Headphones"}}
	written, err := Sync(Settings{Hotkey: "ctrl+alt+f9", Autostart: false, PollSeconds: 15}, active, nil)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveAutostart() {
		t.Error("reloaded autostart = true, want the false that was written")
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

func TestSyncKeepsAnOffHotkeyAsWritten(t *testing.T) {
	withTempAppData(t)

	for _, off := range []string{"", HotkeyDisabled} {
		if _, err := Sync(Settings{Hotkey: off, Autostart: true, PollSeconds: 0}, []Device{{ID: "dev-1", Name: "Speakers"}}, nil); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Hotkey != off {
			t.Errorf("reloaded hotkey = %q, want %q written back unchanged", loaded.Hotkey, off)
		}
		if got := loaded.EffectiveHotkey(); HotkeyEnabled(got) {
			t.Errorf("reloaded %q counts as an enabled hotkey", got)
		}
	}
}

func TestSyncWritesLastSeenAsADate(t *testing.T) {
	withTempAppData(t)
	if _, err := Sync(Settings{Hotkey: "", Autostart: true, PollSeconds: 0}, []Device{{ID: "dev-1", Name: "Speakers"}}, nil); err != nil {
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

	got, err := Sync(Settings{Hotkey: "", Autostart: true, PollSeconds: 0}, []Device{{ID: "new", Name: "Headset"}}, current)
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

	got, err := Sync(Settings{Hotkey: "", Autostart: true, PollSeconds: 0}, active, current)
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

	got, err := Sync(Settings{Hotkey: "", Autostart: true, PollSeconds: 0}, active, current)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Alias != "Desk" || got["b"].Alias != "Headset" {
		t.Errorf("aliases = %q/%q, want the custom one kept and the blank one filled", got["a"].Alias, got["b"].Alias)
	}
}

func TestSyncWritesEntryKeysInOrder(t *testing.T) {
	withTempAppData(t)
	if _, err := Sync(Settings{Hotkey: "", Autostart: true, PollSeconds: 0}, []Device{{ID: "dev-1", Name: "Speakers"}}, nil); err != nil {
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
