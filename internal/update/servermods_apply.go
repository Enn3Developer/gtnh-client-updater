package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

const (
	// ReplacedMods is the folder in StateDir where a sync puts the player's jars it
	// moved out of mods/ to make room for the server's. Nothing ever deletes it.
	ReplacedMods = "replaced-mods"
	modsStaging  = "mods-sync" // in StateDir: what a running sync moved aside
)

// ErrModsRolledBack marks a failed sync after which every jar was put back.
var ErrModsRolledBack = errors.New("your mods are as they were")

// applyModsPlan carries out p in inst's mods folder with the jars of a. Every file it
// replaces or removes is moved aside first, so a failure puts everything back; after a
// success the player's files among them go to the replaced-mods folder and the rest,
// the sync's own old copies, are deleted.
func applyModsPlan(p *ModsPlan, a *ModsArchive, inst prism.Instance) (err error) {
	modsDir := filepath.Join(inst.GameDir, "mods")
	stateDir := filepath.Join(inst.Dir, StateDir)
	staging, kept := filepath.Join(stateDir, modsStaging), filepath.Join(stateDir, ReplacedMods)
	recoverModsSync(staging, kept, modsDir)
	if !p.writes() {
		return nil
	}
	j := &journal{backupDir: staging, root: modsDir}
	defer func() {
		if err == nil {
			return
		}
		if rerr := j.rollback(); rerr != nil {
			err = fmt.Errorf("%w (putting the old mods back failed too: %v — they're in %s)", err, rerr, staging)
			return
		}
		os.RemoveAll(staging)
		err = fmt.Errorf("%w (%w)", err, ErrModsRolledBack)
	}()
	var preserve []string
	for _, c := range p.Changes {
		disk := filepath.Join(modsDir, c.Disk)
		switch {
		case c.Kind == ModAdd || c.Kind == ModUpdate || c.Kind == ModReplace:
			zf, ok := a.jarOf(c.Name)
			if !ok {
				return fmt.Errorf("internal: %s is not in the server's archive", c.Name)
			}
			if err := j.install(disk, zf.Open); err != nil {
				return fmt.Errorf("I couldn't install %s: %w", c.Name, err)
			}
		case c.Kind == ModRemove || c.Kind == ModSetAside || (c.Kind == ModSkip && c.Disk != ""):
			if err := j.stash(disk); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("I couldn't move %s: %w", c.Disk, err)
			}
		default:
			continue
		}
		if c.Preserve {
			preserve = append(preserve, c.Disk)
		}
	}
	clean := true
	for _, n := range preserve {
		if keepFile(filepath.Join(staging, n), kept, n) != nil {
			clean = false // still in the staging folder: the next sync moves it on
		}
	}
	if clean {
		os.RemoveAll(staging)
	}
	return nil
}

// recoverModsSync finishes what a sync killed half way left behind: anything it had moved
// aside goes to the replaced-mods folder (it may be the player's), and its temporary
// files in mods/ are deleted.
func recoverModsSync(staging, kept, modsDir string) {
	if ents, err := os.ReadDir(modsDir); err == nil {
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".gtnh-tmp") {
				os.Remove(filepath.Join(modsDir, e.Name()))
			}
		}
	}
	ents, err := os.ReadDir(staging)
	if err != nil {
		return
	}
	clean := true
	for _, e := range ents {
		if e.IsDir() || keepFile(filepath.Join(staging, e.Name()), kept, e.Name()) != nil {
			clean = false
		}
	}
	if clean {
		os.RemoveAll(staging)
	}
}

// keepFile moves src into dir as name, or as "name (2)", "name (3)"… when that's taken.
func keepFile(src, dir, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base, ext := name, ""
	if i := strings.LastIndex(name, ".jar"); i > 0 {
		base, ext = name[:i], name[i:]
	}
	dst := filepath.Join(dir, name)
	for k := 2; ; k++ {
		if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, k, ext))
	}
	return moveFile(src, dst)
}

// jarOf is the archive entry of the jar name.
func (a *ModsArchive) jarOf(name string) (*zip.File, bool) {
	if a == nil {
		return nil, false
	}
	zf, ok := a.jars[name]
	return zf, ok
}
