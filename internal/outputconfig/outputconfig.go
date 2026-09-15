// Package outputconfig persists per-device output settings - whether a
// device is skipped when cycling outputs, and the display name (alias)
// shown for it in the tray menu - in a YAML file under the current
// user's profile. The file is meant to be hand-edited: Sync writes it
// out with every known device listed explicitly (rather than just an
// exclude-list with no context of what else exists), and Load reads
// back whatever the user last saved.
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
	Outputs []Entry `yaml:"outputs"`
}

// legacyFile is the outputs.yaml format used before the config file
// gained the alias field and was renamed to devices.yaml (Outputs, with
// no "alias" key - Entry.Alias just comes back empty, which is the
// correct "use the real name" default anyway), plus the even older
// exclude-list-only format (Skip) from before that. Load still
// understands both so upgrading doesn't silently drop anyone's saved
// settings.
type legacyFile struct {
	Outputs []Entry  `yaml:"outputs"`
	Skip    []string `yaml:"skip"`
}

const header = `# Audio Output Switcher - device configuration
#
# One entry per playback device.
#   alias  - the name shown for this device in the tray's right-click
#            menu; defaults to the real device name, edit freely.
#   skip   - set to true to leave this device out when cycling outputs
#            (hotkey, left-click tray icon, or "Next"); it stays fully
#            clickable in the tray menu for a direct, one-off switch
#            either way.
#
# Edits are picked up automatically after saving - no need to restart
# the app.

`

// fileName is deliberately not "outputs.yaml" (its name before the
// alias field existed) - "devices.yaml" better reflects that it holds
// per-device settings in general, not just which outputs to skip.
const fileName = "devices.yaml"

// legacyFileName is the previous name of this file, still read as a
// migration fallback - see Load.
const legacyFileName = "outputs.yaml"

// Path returns where the config file lives:
// %APPDATA%\AudioOutputSwitcher\devices.yaml.
func Path() string {
	return filepath.Join(os.Getenv("APPDATA"), "AudioOutputSwitcher", fileName)
}

func legacyPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "AudioOutputSwitcher", legacyFileName)
}

// Load returns the last-saved settings, keyed by device name. A missing
// or unreadable file just means nothing has been customized yet; if
// devices.yaml doesn't exist yet, this falls back to reading the older
// outputs.yaml (in either format it was ever written in) so upgrading
// doesn't lose anyone's saved exclusions or drop them the first time
// devices.yaml is written.
func Load() map[string]Entry {
	if data, err := os.ReadFile(Path()); err == nil {
		var f file
		if err := yaml.Unmarshal(data, &f); err == nil && len(f.Outputs) > 0 {
			entries := make(map[string]Entry, len(f.Outputs))
			for _, e := range f.Outputs {
				entries[e.Name] = e
			}
			return entries
		}
	}

	data, err := os.ReadFile(legacyPath())
	if err != nil {
		return map[string]Entry{}
	}

	var legacy legacyFile
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return map[string]Entry{}
	}
	entries := make(map[string]Entry, len(legacy.Outputs)+len(legacy.Skip))
	for _, e := range legacy.Outputs {
		entries[e.Name] = Entry{Name: e.Name, Skip: e.Skip}
	}
	for _, name := range legacy.Skip {
		entries[name] = Entry{Name: name, Skip: true}
	}
	return entries
}

// Sync writes devices.yaml listing every name in names, carrying over
// each one's alias/skip from current - plus any entry in current whose
// device isn't in names, so a device that's momentarily disconnected
// keeps its saved settings visible instead of quietly vanishing from
// the file. A name with no prior entry gets one with its alias defaulted
// to the name itself. It returns the resulting settings (equal to
// current, just normalized to what was actually written).
func Sync(names []string, current map[string]Entry) (map[string]Entry, error) {
	seen := make(map[string]bool, len(names)+len(current))
	all := make([]string, 0, len(names)+len(current))
	for _, name := range names {
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
		entries[i] = e
		result[name] = e
	}

	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(file{Outputs: entries})
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
