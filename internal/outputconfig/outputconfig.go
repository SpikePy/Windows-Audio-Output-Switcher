// Package outputconfig persists the app's hand-editable settings - the
// switch hotkey, and per-device output settings (whether a device is
// skipped when cycling outputs, and the display name/alias shown for it
// in the tray menu) - in a YAML file under the current user's profile.
// The file is meant to be hand-edited: Sync writes it out with every
// known device listed explicitly (rather than just an exclude-list with
// no context of what else exists), and Load reads back whatever the
// user last saved.
package outputconfig

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// Entry is one device's row in the config file.
type Entry struct {
	Name string `yaml:"name"`
	// Alias is the name shown for this device in the tray's right-click
	// menu. Sync always fills it in (defaulting to Name) so it's visible
	// and discoverable to edit; an empty value read back from a
	// hand-edited file (or from before this field existed) falls back to
	// Name too.
	Alias string `yaml:"alias"`
	Skip  bool   `yaml:"skip"`
	// LastSeen is when this device was last found in the currently
	// active device list by a Sync call - the zero value means never
	// (e.g. a row added by hand). Sync only ever advances it forward for
	// devices it's told are currently present; it's never used to decide
	// whether to keep or drop an entry - Sync never removes one.
	LastSeen time.Time `yaml:"last_seen"`
}

// DisplayName returns the name to show in the tray menu for the device
// named name: entries[name]'s alias if it has one, otherwise name
// itself - name is always the fallback, whether because there's no
// entry for it at all (e.g. a device seen for the first time, before
// the next Sync) or because its entry's alias is empty.
func DisplayName(entries map[string]Entry, name string) string {
	if e, ok := entries[name]; ok && e.Alias != "" {
		return e.Alias
	}
	return name
}

type file struct {
	Hotkey      string  `yaml:"hotkey"`
	PollSeconds int     `yaml:"poll_seconds"`
	Outputs     []Entry `yaml:"outputs"`
}

// DefaultHotkey is used whenever the config file has no (or an empty)
// hotkey set - a fresh install, or a hand-edit that cleared it.
const DefaultHotkey = "win+s"

// DefaultPollSeconds is used whenever the config file has no (or a
// non-positive) poll_seconds set.
const DefaultPollSeconds = 60

const header = `# Audio Output Switcher - configuration
#
# hotkey - the global shortcut that cycles the active output, parsed by
#          internal/hotkeycfg: at least one modifier (ctrl, alt, shift,
#          win) plus a key (a letter, digit, F1-F20, or one of space,
#          return/enter, escape/esc, delete/del, tab, left, right, up,
#          down), joined with "+" - e.g. "ctrl+alt+f9". Defaults to
#          "win+s" if left blank.
#
# poll_seconds - how often, in seconds, to check for plugged/unplugged
#                devices and pick up hand-edits to this file in the
#                background. Switching outputs (hotkey, tray icon, or
#                the menu) always re-checks immediately regardless of
#                this interval. Defaults to 60 if left blank or set to
#                0 or less.
#
# outputs - one entry per playback device. A device that's currently
#           disconnected keeps its row (and its skip/alias settings)
#           rather than being removed automatically - delete its row by
#           hand if you want it gone for good.
#
#   alias      - the name shown for this device in the tray's
#                right-click menu; defaults to the real device name,
#                edit freely.
#   skip       - set to true to leave this device out when cycling
#                outputs (hotkey, left-click tray icon, or "Next"); it
#                stays fully clickable in the tray menu for a direct,
#                one-off switch either way.
#   last_seen  - when this device was last detected as active; updated
#                automatically, not meant to be hand-edited.
#
# Edits are picked up automatically after saving - no need to restart
# the app.

`

// fileName is deliberately not "outputs.yaml" (its name before the
// alias field existed) - "devices.yaml" better reflects that it holds
// per-device settings in general, not just which outputs to skip.
const fileName = "devices.yaml"

// Path returns where the config file lives:
// %APPDATA%\AudioOutputSwitcher\devices.yaml.
func Path() string {
	return filepath.Join(os.Getenv("APPDATA"), "AudioOutputSwitcher", fileName)
}

// Config is everything Load reads back from the config file.
type Config struct {
	// Hotkey is the raw value saved in the file, which may be empty
	// (nothing customized yet) - see EffectiveHotkey.
	Hotkey string
	// PollSeconds is the raw value saved in the file, which may be
	// non-positive (nothing customized yet) - see EffectivePollInterval.
	PollSeconds int
	Devices     map[string]Entry
}

// EffectiveHotkey returns c.Hotkey, or DefaultHotkey if it's empty.
func (c Config) EffectiveHotkey() string {
	if c.Hotkey == "" {
		return DefaultHotkey
	}
	return c.Hotkey
}

// EffectivePollInterval returns c.PollSeconds as a Duration, or
// DefaultPollSeconds if it's zero or negative.
func (c Config) EffectivePollInterval() time.Duration {
	if c.PollSeconds <= 0 {
		return DefaultPollSeconds * time.Second
	}
	return time.Duration(c.PollSeconds) * time.Second
}

// Load returns the last-saved settings. A missing or unreadable file
// just means nothing has been customized yet - Sync creates it the
// first time the user opens the config from the tray menu, or a new
// device is seen.
func Load() Config {
	data, err := os.ReadFile(Path())
	if err != nil {
		return Config{Devices: map[string]Entry{}}
	}

	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Config{Devices: map[string]Entry{}}
	}
	entries := make(map[string]Entry, len(f.Outputs))
	for _, e := range f.Outputs {
		entries[e.Name] = e
	}
	return Config{Hotkey: f.Hotkey, PollSeconds: f.PollSeconds, Devices: entries}
}

// Sync writes devices.yaml with hotkey, pollSeconds, and listing every
// name in names (the currently active devices) plus every device
// already in current, so a device that's disconnected - momentarily or
// for good - keeps its saved settings and never gets removed
// automatically; only a hand-edit of the file ever drops a row. A name
// with no prior entry gets one with its alias defaulted to the name
// itself. Every entry for a name in names has its LastSeen stamped with
// the current time; entries only carried over from current (not
// currently active) keep their old LastSeen. It returns the resulting
// device settings (equal to current, just normalized to what was
// actually written).
func Sync(hotkey string, pollSeconds int, names []string, current map[string]Entry) (map[string]Entry, error) {
	now := time.Now().Truncate(time.Second) // sub-second precision is just noise in a hand-edited file
	active := make(map[string]bool, len(names))

	seen := make(map[string]bool, len(names)+len(current))
	all := make([]string, 0, len(names)+len(current))
	for _, name := range names {
		active[name] = true
		if !seen[name] {
			seen[name] = true
			all = append(all, name)
		}
	}
	for name := range current {
		if !seen[name] {
			seen[name] = true
			all = append(all, name)
		}
	}
	sort.Strings(all)

	entries := make([]Entry, len(all))
	result := make(map[string]Entry, len(all))
	for i, name := range all {
		e, ok := current[name]
		if !ok {
			e = Entry{Name: name, Alias: name}
		}
		e.Name = name
		if e.Alias == "" {
			e.Alias = name
		}
		if active[name] {
			e.LastSeen = now
		}
		entries[i] = e
		result[name] = e
	}

	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(file{Hotkey: hotkey, PollSeconds: pollSeconds, Outputs: entries})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(Path(), append([]byte(header), data...), 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

// ModTime returns the config file's last-modified time, so callers can
// detect an external edit (e.g. by the user's editor) without re-reading
// the file on every check. It returns the zero time if the file doesn't
// exist or can't be stat'd.
func ModTime() time.Time {
	info, err := os.Stat(Path())
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
