// Package update applies a GTNH Prism pack to an existing instance in place.
//
// The model is the server updater's file-level 3-way reconcile, extended to the whole
// instance: B = the pack the instance was installed from (baseline), C = what is on disk
// now, N = the new pack. Only paths that B or N ship are ever touched, so worlds,
// JourneyMap map data, screenshots and the player's own mods are left alone.
//
// Unlike the server, the baseline is stored as fingerprints (size + CRC-32) rather than
// a copy: the pack's config/ alone is ~300 MB. On an instance this tool has never
// updated, the baseline is reconstructed from the installed version's pack, whose
// fingerprints come straight from its zip central directory (HTTP range reads, a few MB)
// instead of a full download.
package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

// StateDir is the per-instance directory where the updater keeps its state and backups.
const StateDir = ".gtnh-updater"

// State is what the updater remembers about an instance between runs.
type State struct {
	Version    string                      `json:"version"`
	Baseline   map[string]pack.Fingerprint `json:"baseline"`
	CustomMods []string                    `json:"customMods,omitempty"`
	// CustomModsURL is the server extra-mods archive the player chose for this
	// instance ("" = none). CustomModsAsked tells "chose none" from "never asked".
	CustomModsURL   string `json:"customModsURL,omitempty"`
	CustomModsAsked bool   `json:"customModsAsked,omitempty"`
	// ServerAddress is the host[:port] the player joins with "Play & join"; "" = none.
	ServerAddress string `json:"serverAddress,omitempty"`
}

func statePath(instDir string) string { return filepath.Join(instDir, StateDir, "state.json") }

// LoadState returns the saved state, or nil if the instance was never updated by this
// tool.
func LoadState(instDir string) (*State, error) {
	data, err := os.ReadFile(statePath(instDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Baseline == nil {
		st.Baseline = map[string]pack.Fingerprint{}
	}
	return &st, nil
}

// SaveState writes the state atomically.
func SaveState(instDir string, st *State) error {
	p := statePath(instDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// UpdateState loads the saved state (an empty one if the instance was never saved),
// lets fn edit it and saves it back. Load and save errors are returned; fn is not
// called on a load error.
func UpdateState(instDir string, fn func(*State)) error {
	st, err := LoadState(instDir)
	if err != nil {
		return err
	}
	if st == nil {
		st = &State{Baseline: map[string]pack.Fingerprint{}}
	}
	fn(st)
	return SaveState(instDir, st)
}
