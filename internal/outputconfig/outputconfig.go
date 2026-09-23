// Package outputconfig persists the app's hand-editable settings - the
// switch hotkey, whether to start at login, the poll interval, and per-device settings (whether a
// device is skipped when cycling outputs, and the alias shown for it) -
// in a YAML file under the current user's profile. Devices are keyed by
// their Windows endpoint ID, so renaming a device, or having two with the
// same name, doesn't mix up their settings.
package outputconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/SpikePy/Windows-Audio-Output-Switcher/internal/hotkeycfg"
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
	// WindowsName is the device's name in Windows as of the last time Sync
	// found it active. It's informational only - the device is matched by
	// ID, and Alias is what's shown.
	WindowsName string `yaml:"windows_name"`
	// Alias is the name shown for the device in the tray menu and OSD.
	// Sync fills in the device's Windows name while it's blank.
	Alias string `yaml:"alias"`
	// LastSeen is the day Sync last found the device active; the zero value
	// means never. It's informational only - Sync never removes an entry.
	LastSeen Date `yaml:"last_seen"`
	// Active is true for the device that was the default output when Sync
	// last ran; informational only, editing it switches nothing.
	Active bool `yaml:"active"`
	Skip   bool `yaml:"skip"`
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
	Autostart   *bool   `yaml:"autostart"`
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
# autostart - true to start the app automatically at login (a shortcut in
#             your Startup folder), false to not. Defaults to true if
#             left blank.
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
#   id           - the Windows endpoint ID this row belongs to; don't edit.
#   windows_name - the device's current name in Windows; updated
#                  automatically, not meant to be hand-edited.
#   alias        - the name shown for this device in the tray menu and the
#                  on-screen notification; filled in with the device's
#                  Windows name if left blank.
#   last_seen    - the date this device was last detected as connected;
#                  updated automatically, not meant to be hand-edited.
#   active       - true for the device that is currently the default
#                  output; updated automatically, editing it switches
#                  nothing.
#   skip         - set to true to leave this device out when cycling
#                  outputs (hotkey or left-click tray icon); it stays fully
#                  clickable in the tray menu for a direct, one-off switch
#                  either way.
#
# Edits are picked up automatically after saving - no need to restart
# the app. If a save leaves this file invalid, the app keeps its previous
# settings and doesn't touch the file until it's fixed.

`

const fileName = "config.yaml"

const dirName = "AudioOutputSwitcher"

// Dir returns the folder holding everything this app keeps in the user's
// profile - the config file, and the installed exe next to it:
// %LOCALAPPDATA%\AudioOutputSwitcher.
func Dir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), dirName)
}

// Path returns where the config file lives:
// %LOCALAPPDATA%\AudioOutputSwitcher\config.yaml.
func Path() string {
	return filepath.Join(Dir(), fileName)
}

// LegacyDir is where versions up to v1.0.6 kept the config file (and, in
// v1.0.6, the exe): %APPDATA%\AudioOutputSwitcher.
func LegacyDir() string {
	return filepath.Join(os.Getenv("APPDATA"), dirName)
}

// MigrateLegacy moves a config file an older version left in LegacyDir
// to Path, unless Path already has one - which then wins, and the old
// file is left for the caller to clean up along with the rest of
// LegacyDir. Having nothing to move isn't an error.
func MigrateLegacy() error {
	legacy := filepath.Join(LegacyDir(), fileName)
	if _, err := os.Stat(Path()); err == nil {
		return nil
	}
	data, err := os.ReadFile(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := writeFileAtomic(Path(), data); err != nil {
		return err
	}
	return os.Remove(legacy)
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
	// Autostart is the raw value saved in the file, nil if it has none -
	// see EffectiveAutostart.
	Autostart *bool
	// PollSeconds is the raw value saved in the file, which may be
	// non-positive (nothing customized yet) - see EffectivePollInterval.
	PollSeconds int
	// Devices holds each device's entry, keyed by its Windows ID.
	Devices map[string]Entry
	// Problems describes each value Load couldn't use, which then took
	// its default instead. While there are any, the caller must not Sync
	// over the file, so the user's text stays for them to fix.
	Problems []string
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

// Settings are the app-wide values the app runs with, all defaults filled
// in - as opposed to Config's raw values.
type Settings struct {
	Hotkey      string
	Autostart   bool
	PollSeconds int
}

// PollInterval returns PollSeconds as a Duration, or DefaultPollSeconds if
// it's zero or negative.
func (s Settings) PollInterval() time.Duration {
	if s.PollSeconds <= 0 {
		return DefaultPollSeconds * time.Second
	}
	return time.Duration(s.PollSeconds) * time.Second
}

// Overrides are settings given on the command line, which win over the
// config file for that run; a nil field leaves the file's value alone.
type Overrides struct {
	Hotkey      *string
	Autostart   *bool
	PollSeconds *int
}

// With returns s with every value o sets replaced.
func (s Settings) With(o Overrides) Settings {
	if o.Hotkey != nil {
		s.Hotkey = *o.Hotkey
	}
	if o.Autostart != nil {
		s.Autostart = *o.Autostart
	}
	if o.PollSeconds != nil {
		s.PollSeconds = *o.PollSeconds
	}
	return s
}

// Settings returns the file's app-wide values with defaults filled in -
// what Sync should write back so a file's own values survive.
func (c Config) Settings() Settings {
	return Settings{
		Hotkey:      c.EffectiveHotkey(),
		Autostart:   c.EffectiveAutostart(),
		PollSeconds: int(c.EffectivePollInterval() / time.Second),
	}
}

// EffectiveAutostart reports whether the app should start at login: what
// the file says, or true if it doesn't say (including when there's no
// config file yet).
func (c Config) EffectiveAutostart() bool {
	return c.Autostart == nil || *c.Autostart
}

// EffectivePollInterval returns c.PollSeconds as a Duration, or
// DefaultPollSeconds if it's zero or negative.
func (c Config) EffectivePollInterval() time.Duration {
	return Settings{PollSeconds: c.PollSeconds}.PollInterval()
}

// Load returns the last-saved settings. A missing file isn't an error - it
// just means nothing has been customized yet, and Sync creates it. An
// invalid value isn't either: that setting (or that device's field) takes
// its default and Config.Problems says so, while everything else in the
// file still applies. Keys Load doesn't know are ignored. A file that
// can't be read or isn't YAML at all is an error: the returned Config then
// holds defaults. In both cases the caller must not Sync over the file, or
// the user's hand-edits would be replaced with those defaults.
func Load() (Config, error) {
	empty := Config{Devices: map[string]Entry{}}

	data, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return empty, err
	}
	cfg := Config{Exists: true, Devices: map[string]Entry{}}
	if len(doc.Content) == 0 { // an empty file
		return cfg, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return empty, fmt.Errorf("line %d: expected settings like \"hotkey: win+a\", not a %s", root.Line, kindName(root))
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		switch key.Value {
		case "hotkey":
			if !decode(value, &cfg.Hotkey, "hotkey", &cfg.Problems) {
				cfg.Hotkey = DefaultHotkey
			} else if HotkeyEnabled(cfg.Hotkey) {
				if _, _, err := hotkeycfg.Parse(cfg.Hotkey); err != nil {
					cfg.Problems = append(cfg.Problems, fmt.Sprintf("line %d: hotkey %q: %v", value.Line, cfg.Hotkey, err))
					cfg.Hotkey = DefaultHotkey
				}
			}
		case "autostart":
			var on bool
			if decode(value, &on, "autostart", &cfg.Problems) && !isNull(value) {
				cfg.Autostart = &on
			}
		case "poll_seconds":
			decode(value, &cfg.PollSeconds, "poll_seconds", &cfg.Problems)
		case "outputs":
			loadOutputs(value, cfg.Devices, &cfg.Problems)
		}
	}
	return cfg, nil
}

// loadOutputs adds each row of the outputs list to entries. A field with
// an invalid value keeps its default; a row that isn't a mapping, or has
// no ID to match a device by, is skipped.
func loadOutputs(list *yaml.Node, entries map[string]Entry, problems *[]string) {
	if isNull(list) {
		return
	}
	if list.Kind != yaml.SequenceNode {
		*problems = append(*problems, fmt.Sprintf("line %d: outputs should be a list of devices, not a %s", list.Line, kindName(list)))
		return
	}
	for _, row := range list.Content {
		if row.Kind != yaml.MappingNode {
			*problems = append(*problems, fmt.Sprintf("line %d: an outputs entry should have id, alias, skip and so on, not be a %s", row.Line, kindName(row)))
			continue
		}
		var e Entry
		for i := 0; i+1 < len(row.Content); i += 2 {
			key, value := row.Content[i], row.Content[i+1]
			switch key.Value {
			case "id":
				decode(value, &e.ID, "id", problems)
			case "windows_name":
				decode(value, &e.WindowsName, "windows_name", problems)
			case "alias":
				decode(value, &e.Alias, "alias", problems)
			case "last_seen":
				decode(value, &e.LastSeen, "last_seen", problems)
			case "active":
				decode(value, &e.Active, "active", problems)
			case "skip":
				decode(value, &e.Skip, "skip", problems)
			}
		}
		if e.ID != "" { // a row without an ID can't be matched to any device
			entries[e.ID] = e
		}
	}
}

// decode reads value into out. If that fails it leaves out as it was,
// records the problem and reports false. A blank value (key with nothing
// after it) counts as fine and leaves out as it was.
func decode(value *yaml.Node, out any, name string, problems *[]string) bool {
	if isNull(value) {
		return true
	}
	if value.Kind != yaml.ScalarNode {
		*problems = append(*problems, fmt.Sprintf("line %d: %s should be a single value, not a %s", value.Line, name, kindName(value)))
		return false
	}
	// Decode into a fresh value, so a partial failure can't leave out
	// half-changed.
	fresh := reflect.New(reflect.TypeOf(out).Elem())
	if err := value.Decode(fresh.Interface()); err != nil {
		*problems = append(*problems, fmt.Sprintf("line %d: %s %q is not valid", value.Line, name, value.Value))
		return false
	}
	reflect.ValueOf(out).Elem().Set(fresh.Elem())
	return true
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "list"
	case yaml.MappingNode:
		return "set of keys"
	}
	return "single value"
}

// Sync writes the config file with settings, an entry for every
// device in active - with its current Windows name, today's date as
// LastSeen, and given that name as its alias if it has none yet - and
// every entry in current for a device that isn't active, unchanged. Only
// the entry for defaultID (the current default output) is marked Active.
// Entries are never dropped; only a hand-edit removes one. It returns the
// device entries as written, keyed by ID.
func Sync(settings Settings, active []Device, defaultID string, current map[string]Entry) (map[string]Entry, error) {
	today := dateOf(time.Now())

	result := make(map[string]Entry, len(current)+len(active))
	for id, e := range current {
		e.ID = id
		e.Active = id == defaultID
		result[id] = e
	}
	for _, d := range active {
		e := result[d.ID]
		e.ID = d.ID
		e.WindowsName = d.Name
		if e.Alias == "" {
			e.Alias = d.Name
		}
		e.LastSeen = today
		e.Active = d.ID == defaultID
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

	data, err := yaml.Marshal(file{Hotkey: settings.Hotkey, Autostart: &settings.Autostart, PollSeconds: settings.PollSeconds, Outputs: entries})
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(Path(), append([]byte(header), data...)); err != nil {
		return nil, err
	}
	return result, nil
}

// Stale reports whether entries is missing something Sync would write for
// the given active devices and default output: an entry for a device, its
// current Windows name, or which device is the active one. LastSeen alone
// doesn't count, so the file isn't rewritten just because the date changed.
func Stale(active []Device, defaultID string, entries map[string]Entry) bool {
	for _, d := range active {
		e, ok := entries[d.ID]
		if !ok || e.WindowsName != d.Name {
			return true
		}
	}
	for id, e := range entries {
		if e.Active != (id == defaultID) {
			return true
		}
	}
	return false
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
