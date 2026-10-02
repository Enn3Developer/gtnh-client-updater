package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// BackupManifest is the file inside a backup-<ts> dir that describes the update it undoes.
const BackupManifest = "gtnh-backup.json"

// BackupInfo is what a restore needs beyond the stashed files themselves.
type BackupInfo struct {
	From      string    `json:"from"` // version the update came from
	To        string    `json:"to"`   // version the update installed
	When      time.Time `json:"when"`
	PrevName  string    `json:"prevName,omitempty"`  // instance name before the rename; "" if unchanged
	PrevState *State    `json:"prevState,omitempty"` // state.json before the update; nil if there was none
	Added     []string  `json:"added,omitempty"`     // backup-mirror paths (slashed) the update created
	AddedMods []string  `json:"addedMods,omitempty"` // server extra-mod jars the sync created
}

// Backup is one restorable backup dir with its manifest.
type Backup struct {
	Dir  string
	Info BackupInfo
}

// RestoreResult summarises a restore. From/To are the versions moved from and back to.
type RestoreResult struct {
	From, To           string
	MovedBack, Removed int
	Skipped            []string
	Renamed            string
}

// ListBackups returns the instance's restorable backups, newest first.
func ListBackups(instDir string) ([]Backup, error) {
	dir := filepath.Join(instDir, StateDir)
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") {
			continue
		}
		bdir := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(bdir, BackupManifest))
		if err != nil {
			continue // no notes: an old or partial backup, not restorable
		}
		var info BackupInfo
		if json.Unmarshal(data, &info) != nil {
			continue
		}
		out = append(out, Backup{Dir: bdir, Info: info})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Info.When.Equal(out[j].Info.When) {
			return out[i].Info.When.After(out[j].Info.When)
		}
		return filepath.Base(out[i].Dir) > filepath.Base(out[j].Dir)
	})
	return out, nil
}

// writeBackupInfo stores info as the backup dir's manifest.
func writeBackupInfo(backupDir string, info BackupInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(backupDir, BackupManifest), data, 0o644)
}

// Restore undoes the update recorded in b: removes what it added, moves the stashed files
// back, restores the updater state and the instance name. There is no undo; on failure
// the backup dir stays so the player can retry.
func Restore(inst prism.Instance, b Backup, rep Reporter) (*RestoreResult, error) {
	if prism.Running(inst) {
		return nil, errGameRunning
	}
	res := &RestoreResult{From: b.Info.To, To: b.Info.From}

	rep.Step("Removing files the update added")
	if err := removeAdded(inst, b.Info, res); err != nil {
		return nil, fmt.Errorf("removing added files: %w", err)
	}

	rep.Step("Putting the old files back")
	if err := moveBack(inst, b.Dir, rep, res); err != nil {
		return nil, fmt.Errorf("putting files back: %w", err)
	}

	rep.Step("Saving my notes")
	if err := restoreState(inst.Dir, b.Info.PrevState); err != nil {
		return nil, fmt.Errorf("saving updater state: %w", err)
	}

	if name, ok, err := prism.RenameVersion(inst.Dir, b.Info.To, b.Info.From); err != nil {
		rep.Warn("could not restore the instance name: " + err.Error())
	} else if ok {
		res.Renamed = name
	}

	if len(res.Skipped) == 0 {
		if err := os.RemoveAll(b.Dir); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// removeAdded deletes the files and extra-mod jars the update created from nothing.
// Paths outside the instance (_external) are left alone and reported as skipped.
func removeAdded(inst prism.Instance, info BackupInfo, res *RestoreResult) error {
	var dirs []string
	remove := func(p string) error {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		res.Removed++
		dirs = append(dirs, filepath.Dir(p))
		return nil
	}
	for _, p := range info.Added {
		if strings.HasPrefix(p, "_external/") {
			res.Skipped = append(res.Skipped, p)
			continue
		}
		if err := remove(filepath.Join(inst.Dir, filepath.FromSlash(p))); err != nil {
			return err
		}
	}
	for _, name := range info.AddedMods {
		if err := remove(filepath.Join(inst.GameDir, "mods", name)); err != nil {
			return err
		}
	}
	pruneEmptyDirs(dirs, inst.Dir)
	return nil
}

// moveBack moves every stashed file in backupDir to where it came from, overwriting
// what the update put there. _external files stay in the backup and are reported.
func moveBack(inst prism.Instance, backupDir string, rep Reporter, res *RestoreResult) error {
	manifest := filepath.Join(backupDir, BackupManifest)
	var files []string
	err := filepath.WalkDir(backupDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && p != manifest {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i, f := range files {
		rel, err := filepath.Rel(backupDir, f)
		if err != nil {
			return err
		}
		first, rest, _ := strings.Cut(rel, string(filepath.Separator))
		var dst string
		switch first {
		case "_external":
			res.Skipped = append(res.Skipped, filepath.ToSlash(rel))
		case "custom-mods":
			dst = filepath.Join(inst.GameDir, "mods", rest)
		default:
			dst = filepath.Join(inst.Dir, rel)
		}
		if dst != "" {
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := moveFile(f, dst); err != nil {
				return err
			}
			res.MovedBack++
		}
		rep.Progress(int64(i+1), int64(len(files)))
	}
	return nil
}

// restoreState puts back the pre-update state, keeping the player's current per-instance
// settings (extra-mods URL, server address), which are not part of the update.
func restoreState(instDir string, prev *State) error {
	cur, err := LoadState(instDir)
	if err != nil {
		return err
	}
	st := &State{Baseline: map[string]pack.Fingerprint{}}
	if prev != nil {
		cp := *prev
		st = &cp
	}
	if cur != nil {
		st.CustomModsURL, st.CustomModsAsked, st.ServerAddress = cur.CustomModsURL, cur.CustomModsAsked, cur.ServerAddress
	} else {
		st.CustomModsURL, st.CustomModsAsked, st.ServerAddress = "", false, ""
	}
	return SaveState(instDir, st)
}
