// Package appcfg is the launcher's own small settings file: the Prism executable override and what to do after Play.
package appcfg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is the persisted launcher settings.
type Config struct {
	PrismExe  string `json:"prismExe,omitempty"`  // Prism executable the player pointed at; "" = find it automatically
	AfterPlay string `json:"afterPlay,omitempty"` // AfterPlayStay (default) or AfterPlayQuit
}

// AfterPlayStay keeps the launcher open after Play; it is also the default for empty or unknown values.
const AfterPlayStay = "stay"

// AfterPlayQuit closes the launcher after Play.
const AfterPlayQuit = "quit"

// Path returns the settings file location under the user's config directory.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gtnh-update", "config.json"), nil
}

// Load reads the settings from Path.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the settings from path; a missing file yields a zero Config and no error.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes the settings to Path.
func Save(c Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveTo(path, c)
}

// SaveTo atomically writes the settings to path via a temporary file and rename.
func SaveTo(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// StaysOpen reports whether the launcher should remain open after Play.
func (c Config) StaysOpen() bool {
	return c.AfterPlay != AfterPlayQuit
}
