// Package outputconfig persists the app's hand-editable settings - the
// switch hotkey, the poll interval, and per-device settings (whether a
// device is skipped when cycling outputs, and the alias shown for it) -
// in a YAML file under the current user's profile. Devices are keyed by
// their Windows endpoint ID, so renaming a device, or having two with the
// same name, doesn't mix up their settings.
package outputconfig

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Device is a currently active output as Windows reports it.
type Device struct {
	ID   string
	Name string
}

// Entry is one device's row in the config file. Its field order is the
// order the keys are written in.
type Entry struct {
	// ID is the Windows endpoint ID the entry belongs to.
	ID string `yaml:"id"`
	// Alias is the name shown for the device in the tray menu and OSD.
	// Sync fills in the device's Windows name while it's blank.
	Alias string `yaml:"alias"`
	// LastSeen is the day Sync last found the device active; the zero value
	// means never. It's informational only - Sync never removes an entry.
	LastSeen Date `yaml:"last_seen"`
	Skip     bool `yaml:"skip"`
}

// Date is a calendar day, written to the config file as YYYY-MM-DD.
// Reading also accepts a full timestamp, keeping just its date.
type Date struct{ time.Time }

func dateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d Date) MarshalYAML() (interface{}, error) {
	// Tagged as a timestamp so it's written unquoted, like a date should be.
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: d.Format(time.DateOnly)}, nil
}

func (d *Date) UnmarshalYAML(n *yaml.Node) error {
	var t time.Time
	if err := n.Decode(&t); err != nil {
		return err
	}
	*d = dateOf(t)
	return nil
}

// DisplayName returns the alias configured for the device with the given
// ID, or windowsName if it has none (including when it has no entry yet).
func DisplayName(entries map[string]Entry, id, windowsName string) string {
	if e, ok := entries[id]; ok && e.Alias != "" {
		return e.Alias
	}
	return windowsName
}

type file struct {
	Hotkey      string  `yaml:"hotkey"`
	PollSeconds int     `yaml:"poll_seconds"`
	Outputs     []Entry `yaml:"outputs"`
}

// DefaultHotkey is what a config file that doesn't exist yet gets, so a
// fresh install cycles outputs out of the box.
const DefaultHotkey = "win+a"

// HotkeyDisabled turns the global hotkey off when the config file's hotkey
// is set to it; an empty value, or no hotkey line at all, does the same.
const HotkeyDisabled = "disabled"

// DefaultPollSeconds is used whenever the config file has no (or a
// non-positive) poll_seconds set.
const DefaultPollSeconds = 60

// HotkeyEnabled reports whether combo asks for a hotkey at all - that is,
// whether it's neither empty nor HotkeyDisabled.
func HotkeyEnabled(combo string) bool {
	trimmed := strings.TrimSpace(combo)
	return trimmed != "" && !strings.EqualFold(trimmed, HotkeyDisabled)
}

const header = `# Audio Output Switcher - configuration
#
# hotkey - the global shortcut that cycles the active output, parsed by
#          internal/hotkeycfg: at least one modifier (ctrl, alt, shift,
#          win) plus a key (a letter, digit, F1-F20, or one of space,
#          return/enter, escape/esc, delete/del, tab, left, right, up,
#          down), joined with "+" - e.g. "ctrl+alt+f9". Leave it empty,
#          set it to "disabled", or delete the line to switch the hotkey
#          off entirely; the tray icon and its menu keep working either
#          way. A config file created from scratch starts out with
#          "win+a".
#
# poll_seconds - how often, in seconds, to check this file for hand-edits
#                (and re-check devices as a fallback - device changes are
#                normally picked up instantly). Defaults to 60 if left
#                blank or set to 0 or less.
#
# outputs - one entry per playback device, matched by its Windows ID, so
#           renaming a device or having two with the same name keeps
#           their settings apart. A disconnected device keeps its row
#           rather than being removed automatically - delete it by hand
#           if you want it gone for good.
#
#   id         - the Windows endpoint ID this row belongs to; don't edit.
#   alias      - the name shown for this device in the tray menu and the
#                on-screen notification; filled in with the device's
#                Windows name if left blank.
#   last_seen  - the date this device was last detected as active;
#                updated automatically, not meant to be hand-edited.
#   skip       - set to true to leave this device out when cycling
#                outputs (hotkey or left-click tray icon); it stays fully
#                clickable in the tray menu for a direct, one-off switch
#                either way.
#
# Edits are picked up automatically after saving - no need to restart
# the app. If a save leaves this file invalid, the app keeps its previous
# settings and doesn't touch the file until it's fixed.

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
	// Exists is false when there's no readable config file yet, which is
	// what tells a fresh install apart from a file whose hotkey was
	// deliberately emptied out - see EffectiveHotkey.
	Exists bool
	// Hotkey is the raw value saved in the file, which may be empty or
	// HotkeyDisabled - see EffectiveHotkey and HotkeyEnabled.
	Hotkey string
	// PollSeconds is the raw value saved in the file, which may be
	// non-positive (nothing customized yet) - see EffectivePollInterval.
	PollSeconds int
	// Devices holds each device's entry, keyed by its Windows ID.
	Devices map[string]Entry
}

// EffectiveHotkey returns the hotkey value to act on: whatever the file
// says - including an empty one, which means the hotkey is switched off -
// or DefaultHotkey when there's no config file yet. Check the result with
// HotkeyEnabled before trying to register it.
func (c Config) EffectiveHotkey() string {
	if !c.Exists {
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

// Load returns the last-saved settings. A missing file isn't an error - it
// just means nothing has been customized yet, and Sync creates it. A file
// that can't be read or parsed is: the returned Config then holds
// defaults, and the caller must not Sync over the file, or the user's
// hand-edits would be replaced with those defaults.
func Load() (Config, error) {
	empty := Config{Devices: map[string]Entry{}}

	data, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}

	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return empty, err
	}
	entries := make(map[string]Entry, len(f.Outputs))
	for _, e := range f.Outputs {
		if e.ID != "" { // a row without an ID can't be matched to any device
			entries[e.ID] = e
		}
	}
	return Config{Exists: true, Hotkey: f.Hotkey, PollSeconds: f.PollSeconds, Devices: entries}, nil
}

// Sync writes the config file with hotkey, pollSeconds, an entry for every
// device in active - with today's date as LastSeen, and given the device's
// Windows name as its alias if it has none yet - and every entry in
// current for a device that isn't active, unchanged. Entries are never
// dropped; only a hand-edit removes one. It returns the device entries as
// written, keyed by ID.
func Sync(hotkey string, pollSeconds int, active []Device, current map[string]Entry) (map[string]Entry, error) {
	today := dateOf(time.Now())

	result := make(map[string]Entry, len(current)+len(active))
	for id, e := range current {
		e.ID = id
		result[id] = e
	}
	for _, d := range active {
		e := result[d.ID]
		e.ID = d.ID
		if e.Alias == "" {
			e.Alias = d.Name
		}
		e.LastSeen = today
		result[d.ID] = e
	}

	entries := make([]Entry, 0, len(result))
	for _, e := range result {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		ai, aj := strings.ToLower(entries[i].Alias), strings.ToLower(entries[j].Alias)
		if ai != aj {
			return ai < aj
		}
		return entries[i].ID < entries[j].ID
	})

	data, err := yaml.Marshal(file{Hotkey: hotkey, PollSeconds: pollSeconds, Outputs: entries})
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(Path(), append([]byte(header), data...)); err != nil {
		return nil, err
	}
	return result, nil
}

// writeFileAtomic writes data to a temporary file next to path and renames
// it into place, so a reader (the app's own reload, or an editor) never
// sees a half-written file.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
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
