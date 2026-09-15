// Package appstate persists the switcher's small amount of state (the
// configured hotkey and whether switching is currently enabled) across
// restarts.
package appstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultHotkey is used the first time the app runs, and whenever the
// configured hotkey string can't be parsed.
const DefaultHotkey = "win+s"

// Config is the on-disk shape of the app's state file.
type Config struct {
	Hotkey  string `json:"hotkey"`
	Enabled bool   `json:"enabled"`
}

func defaults() Config {
	return Config{Hotkey: DefaultHotkey, Enabled: true}
}

// Dir returns the directory used for this app's persisted state
// (%APPDATA%\AudioOutputSwitcher on Windows).
func Dir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "AudioOutputSwitcher")
}

func configPath() string {
	return filepath.Join(Dir(), "config.json")
}

// Load reads the config file, creating it with default values the first
// time it's called.
func Load() (Config, error) {
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		cfg := defaults()
		return cfg, cfg.Save()
	}
	if err != nil {
		return Config{}, err
	}

	cfg := defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaults(), nil
	}
	if cfg.Hotkey == "" {
		cfg.Hotkey = DefaultHotkey
	}
	return cfg, nil
}

// Save persists the config file, creating its directory if needed.
func (c Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0o644)
}
