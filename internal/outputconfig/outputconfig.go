// Package outputconfig persists which playback devices should be
// skipped when cycling through outputs, in a YAML file under the
// current user's profile. The file is meant to be hand-edited: Sync
// writes it out with every known device listed explicitly (rather than
// just an exclude-list with no context of what else exists), and Load
// reads back whatever the user last saved.
package outputconfig

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// entry is one device's row in the config file.
type entry struct {
	Name string `yaml:"name"`
	Skip bool   `yaml:"skip"`
}

type file struct {
	Outputs []entry `yaml:"outputs"`
}

// legacyFile is the exclude-list-only format used before the config file
// became hand-editable; Load still understands it so upgrading doesn't
// silently drop anyone's saved exclusions.
type legacyFile struct {
	Skip []string `yaml:"skip"`
}

const header = `# Audio Output Switcher - output configuration
#
# One entry per playback device. Set "skip: true" on any device you
# don't want included when cycling outputs (hotkey, left-click tray
# icon, or "Next"); it stays fully clickable in the tray menu for a
# direct, one-off switch either way.
#
# Edits are picked up automatically after saving - no need to restart
# the app.

`

// Path returns where the config file lives:
// %APPDATA%\AudioOutputSwitcher\outputs.yaml.
func Path() string {
	return filepath.Join(os.Getenv("APPDATA"), "AudioOutputSwitcher", "outputs.yaml")
}

// Load returns the set of device names currently marked to be skipped.
// A missing or unreadable file just means nothing is skipped yet.
func Load() map[string]bool {
	data, err := os.ReadFile(Path())
	if err != nil {
		return map[string]bool{}
	}

	var f file
	if err := yaml.Unmarshal(data, &f); err == nil && len(f.Outputs) > 0 {
		skip := make(map[string]bool, len(f.Outputs))
		for _, e := range f.Outputs {
			if e.Skip {
				skip[e.Name] = true
			}
		}
		return skip
	}

	var legacy legacyFile
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return map[string]bool{}
	}
	skip := make(map[string]bool, len(legacy.Skip))
	for _, name := range legacy.Skip {
		skip[name] = true
	}
	return skip
}

// Sync writes the config file listing every name in names, each marked
// skip according to the current skip set - plus any name in skip that
// isn't in names, so a device that's momentarily disconnected keeps its
// saved exclusion visible instead of quietly vanishing from the file.
// It returns the resulting skip set (equal to skip, just normalized to
// what was actually written).
func Sync(names []string, skip map[string]bool) (map[string]bool, error) {
	seen := make(map[string]bool, len(names)+len(skip))
	all := make([]string, 0, len(names)+len(skip))
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			all = append(all, name)
		}
	}
	for name := range skip {
		if !seen[name] {
			seen[name] = true
			all = append(all, name)
		}
	}
	sort.Strings(all)

	entries := make([]entry, len(all))
	result := make(map[string]bool, len(skip))
	for i, name := range all {
		entries[i] = entry{Name: name, Skip: skip[name]}
		if skip[name] {
			result[name] = true
		}
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
