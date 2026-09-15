// Package outputconfig persists which playback devices should be
// skipped when cycling through outputs, in a YAML file under the
// current user's profile.
package outputconfig

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type file struct {
	// Skip lists device names (audio.Device.Name) to leave out when
	// cycling to the next output. Storing an exclude-list rather than an
	// include-list means a newly added device is considered by default
	// without the user having to opt it in, and a device that's
	// temporarily disconnected keeps its saved preference for when it
	// comes back.
	Skip []string `yaml:"skip"`
}

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
	if err := yaml.Unmarshal(data, &f); err != nil {
		return map[string]bool{}
	}

	skip := make(map[string]bool, len(f.Skip))
	for _, name := range f.Skip {
		skip[name] = true
	}
	return skip
}

// Save persists the given set of device names to skip.
func Save(skip map[string]bool) error {
	names := make([]string, 0, len(skip))
	for name := range skip {
		names = append(names, name)
	}
	sort.Strings(names)

	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(file{Skip: names})
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), data, 0o644)
}
