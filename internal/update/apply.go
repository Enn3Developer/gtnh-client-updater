package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

// journal records every filesystem change so a failed apply can be undone. Replaced and
// removed files are moved (not copied) into the backup dir, which is cheap on the same
// filesystem and doubles as the manual rollback copy after a successful run.
type journal struct {
	backupDir string
	root      string // instance dir; backup paths mirror paths relative to it
	undo      []func() error
}

// stash moves an existing file into the backup dir and records how to put it back.
func (j *journal) stash(disk string) error {
	rel, err := filepath.Rel(j.root, disk)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Game dir outside the instance (symlinked elsewhere): key it by absolute path.
		rel = filepath.Join("_external", strings.ReplaceAll(filepath.ToSlash(disk), ":", "_"))
	}
	dst := filepath.Join(j.backupDir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := moveFile(disk, dst); err != nil {
		return err
	}
	j.undo = append(j.undo, func() error { return moveFile(dst, disk) })
	return nil
}

// rollback undoes all recorded changes, newest first, and reports the first failure.
func (j *journal) rollback() error {
	var first error
	for i := len(j.undo) - 1; i >= 0; i-- {
		if err := j.undo[i](); err != nil && first == nil {
			first = err
		}
	}
	j.undo = nil
	return first
}

// install writes content to disk, stashing whatever was there first.
func (j *journal) install(disk string, open func() (io.ReadCloser, error)) error {
	tmp, err := writeTemp(disk, open)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(disk); err == nil {
		if err := j.stash(disk); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, disk); err != nil {
		os.Remove(tmp)
		return err
	}
	j.undo = append(j.undo, func() error { return removeIfExists(disk) })
	return nil
}

// Apply executes the plan against the new pack. On any failure it rolls back every
// change it made and returns the error; the instance is then as it was before.
// progress is called with actions done so far.
func Apply(pl *Plan, next *pack.Pack, instDir, backupDir string, progress func(done, total int)) (err error) {
	j := &journal{backupDir: backupDir, root: instDir}
	defer func() {
		if err != nil {
			if rerr := j.rollback(); rerr != nil {
				err = fmt.Errorf("%w (ROLLBACK ALSO FAILED: %v — originals are in %s)", err, rerr, backupDir)
			} else {
				os.RemoveAll(backupDir) // everything was moved back; only empty dirs remain
				err = fmt.Errorf("%w (all changes rolled back, nothing modified)", err)
			}
		}
	}()
	var removedDirs []string
	for i, a := range pl.Actions {
		src := strings.TrimSuffix(a.Path, ".disabled")
		switch a.Kind {
		case Install, Conflict:
			if next == nil {
				return fmt.Errorf("internal: %s needs the pack, but none was downloaded", src)
			}
			e, ok := next.Entries[src]
			if !ok {
				return fmt.Errorf("internal: %s not in pack", src)
			}
			if a.Kind == Conflict && pl.ChoiceOf(a.Path) == KeepMine {
				if err := j.install(a.Disk+".mcnew", e.Open); err != nil {
					return fmt.Errorf("write %s.mcnew: %w", a.Path, err)
				}
			} else if err := j.install(a.Disk, e.Open); err != nil {
				return fmt.Errorf("install %s: %w", a.Path, err)
			}
		case Remove:
			if err := j.stash(a.Disk); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", a.Path, err)
			}
			removedDirs = append(removedDirs, filepath.Dir(a.Disk))
		}
		if progress != nil {
			progress(i+1, len(pl.Actions))
		}
	}
	pruneEmptyDirs(removedDirs, instDir)
	return nil
}

// pruneEmptyDirs removes directories emptied by removals, walking up but never past
// stop. Best effort: a non-empty or busy dir simply stays.
func pruneEmptyDirs(dirs []string, stop string) {
	for _, d := range dirs {
		for d != stop && strings.HasPrefix(d, stop) {
			if os.Remove(d) != nil {
				break
			}
			d = filepath.Dir(d)
		}
	}
}

// writeTemp writes open's content next to disk as <disk>.gtnh-tmp (creating the parent
// dirs) for the caller to rename into place. On failure no temp file is left.
func writeTemp(disk string, open func() (io.ReadCloser, error)) (tmp string, err error) {
	if err := os.MkdirAll(filepath.Dir(disk), 0o755); err != nil {
		return "", err
	}
	tmp = disk + ".gtnh-tmp"
	if err := writeFrom(tmp, open); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func writeFrom(dst string, open func() (io.ReadCloser, error)) error {
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil { // zip reader verifies the CRC at EOF
		f.Close()
		return err
	}
	return f.Close()
}

// moveFile renames, falling back to copy+delete across filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := writeFrom(dst, func() (io.ReadCloser, error) { return os.Open(src) }); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

func removeIfExists(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
