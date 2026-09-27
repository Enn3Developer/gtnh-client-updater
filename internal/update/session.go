package update

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// Reporter receives progress. Implementations must be safe to call from the worker
// goroutine.
type Reporter interface {
	Step(msg string)            // a new phase begins
	Progress(done, total int64) // progress within the phase (bytes or items); total -1 = unknown
	Warn(msg string)            // something the player should know; not fatal
}

// Options describes one update.
type Options struct {
	Client        *http.Client
	Manifest      *manifest.Manifest
	Instance      prism.Instance
	Installed     string // manifest version the instance currently runs
	Target        string // manifest version to install
	CustomModsURL string // server extra-mods archive; "" = none. Saved for next time.
}

// Session is a prepared update: new pack downloaded and verified, plan computed. Call
// Apply to execute it, or Close to throw it away.
type Session struct {
	opts           Options
	state          *State
	base           map[string]pack.Fingerprint
	next           *pack.Pack // nil when nothing needed downloading
	nextFP         map[string]pack.Fingerprint
	zipPath        string
	Flavor         manifest.Flavor
	BaselineSource string // "saved" or "reconstructed from <version>"
	Plan           *Plan
	customMods     *CustomModsArchive // fetched in Prepare when a link is set
	customErr      error
}

// Result summarizes a finished update.
type Result struct {
	From, To   string
	Renamed    string // new display name, "" if unchanged
	BackupDir  string
	CustomMods *CustomModsResult
	CustomErr  error
}

// FlavorOf returns the pack flavor an instance uses.
func FlavorOf(inst prism.Instance) manifest.Flavor {
	if prism.UsesLWJGL3(inst.Dir) {
		return manifest.Java17
	}
	return manifest.Java8
}

// Prepare does everything that does not modify the instance: resolve the baseline,
// download and verify the new pack, scan the disk, and plan.
func Prepare(opts Options, rep Reporter) (s *Session, err error) {
	s = &Session{opts: opts, Flavor: FlavorOf(opts.Instance)}
	defer func() {
		if err != nil {
			s.Close()
			s = nil
		}
	}()
	target, ok := opts.Manifest.Find(opts.Target)
	if !ok {
		return nil, fmt.Errorf("no such version: %s", opts.Target)
	}
	url, err := target.URL(s.Flavor)
	if err != nil {
		return nil, err
	}
	if s.state, err = LoadState(opts.Instance.Dir); err != nil {
		return nil, fmt.Errorf("read updater state: %w", err)
	}
	dir := filepath.Join(opts.Instance.Dir, StateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	rep.Step("Checking which files came with GTNH " + opts.Installed)
	if err := s.loadBaseline(rep); err != nil {
		return nil, err
	}

	var managed []string
	if s.state != nil {
		managed = s.state.CustomMods
	}

	// Same version as the saved baseline (a re-run, e.g. to pick up new server mods):
	// the baseline already describes the target pack, so if every pack file on disk is
	// as the pack ships it there is nothing to download.
	if s.BaselineSource == "saved" && opts.Target == opts.Installed {
		rep.Step("Looking through your instance")
		cur := Scan(opts.Instance, s.base, s.base, itemProgress(rep))
		if pl := MakePlan(opts.Instance, s.base, s.base, cur, managed); pl.Count(Install)+pl.Count(Conflict) == 0 {
			s.nextFP, s.Plan = s.base, pl
		}
	}

	if s.Plan == nil {
		rep.Step("Downloading GTNH " + target.Version)
		s.zipPath = filepath.Join(dir, "download.zip")
		if err := pack.Download(opts.Client, url, s.zipPath, rep.Progress); err != nil {
			return nil, err
		}
		if s.next, err = pack.OpenFile(s.zipPath); err != nil {
			return nil, err
		}
		rep.Step("Checking the download")
		if err := s.next.Verify(itemProgress(rep)); err != nil {
			return nil, fmt.Errorf("%w -- aborting, nothing touched", err)
		}
		rep.Step("Looking through your instance")
		s.nextFP = s.next.Fingerprints()
		cur := Scan(opts.Instance, s.base, s.nextFP, itemProgress(rep))
		s.Plan = MakePlan(opts.Instance, s.base, s.nextFP, cur, managed)
	}

	if opts.CustomModsURL != "" {
		// Fetch now so the summary already knows which jars are the server's, not the
		// player's own. A failure is not fatal: Apply retries and reports it.
		rep.Step("Checking your server's extra mods")
		if s.customMods, s.customErr = FetchCustomMods(opts.Client, opts.CustomModsURL); s.customErr != nil {
			rep.Warn(s.customErr.Error())
		} else {
			s.Plan.ExtraMods = withoutNames(s.Plan.ExtraMods, s.customMods.Names())
		}
	}
	return s, nil
}

func (s *Session) loadBaseline(rep Reporter) error {
	if s.state != nil && s.state.Version == s.opts.Installed {
		s.base, s.BaselineSource = s.state.Baseline, "saved"
		return nil
	}
	rel, ok := s.opts.Manifest.Find(s.opts.Installed)
	if !ok {
		return fmt.Errorf("installed version %q is not in the manifest", s.opts.Installed)
	}
	url, err := rel.URL(s.Flavor)
	if err != nil {
		return err
	}
	// Fingerprints live in the zip central directory: read just that over HTTP ranges.
	if rf, err := pack.OpenRemote(s.opts.Client, url); err == nil {
		old, err := pack.OpenReaderAt(rf, rf.Size())
		if err == nil {
			s.base = old.Fingerprints()
			s.BaselineSource = "reconstructed from " + rel.Version
			return nil
		}
		if !errors.Is(err, pack.ErrNoRanges) {
			return fmt.Errorf("read %s pack: %w", rel.Version, err)
		}
	} else if !errors.Is(err, pack.ErrNoRanges) {
		return fmt.Errorf("read %s pack: %w", rel.Version, err)
	}
	rep.Warn("the GTNH download server didn't allow a partial download, so the whole " + rel.Version + " pack is fetched once to compare against")
	tmp := filepath.Join(s.opts.Instance.Dir, StateDir, "baseline.zip")
	defer os.Remove(tmp)
	if err := pack.Download(s.opts.Client, url, tmp, rep.Progress); err != nil {
		return err
	}
	old, err := pack.OpenFile(tmp)
	if err != nil {
		return err
	}
	defer old.Close()
	s.base = old.Fingerprints()
	s.BaselineSource = "reconstructed from " + rel.Version
	return nil
}

// Apply executes the plan, then renames the instance, syncs custom mods, saves the new
// baseline and prunes older backups. If the pack changes fail, everything is rolled
// back. A custom-mods failure is reported in Result.CustomErr but does not undo the
// update.
func (s *Session) Apply(rep Reporter) (*Result, error) {
	defer s.Close()
	inst := s.opts.Instance
	if prism.Running(inst) {
		return nil, errors.New("the game is still running from this instance -- close Minecraft and try again")
	}
	ts := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(inst.Dir, StateDir, "backup-"+ts)
	res := &Result{From: s.opts.Installed, To: s.opts.Target, BackupDir: backupDir}

	rep.Step("Updating files")
	if err := Apply(s.Plan, s.next, inst.Dir, backupDir, itemProgress(rep)); err != nil {
		return nil, err
	}

	if name, ok, err := prism.RenameVersion(inst.Dir, s.opts.Installed, s.opts.Target); err != nil {
		rep.Warn("could not update the instance name: " + err.Error())
	} else if ok {
		res.Renamed = name
	}

	var managed []string
	if s.state != nil {
		managed = s.state.CustomMods
	}
	nextFP := s.nextFP
	if s.opts.CustomModsURL != "" {
		rep.Step("Syncing your server's extra mods")
		var cm *CustomModsResult
		archive, err := s.customMods, s.customErr
		if archive == nil {
			archive, err = FetchCustomMods(s.opts.Client, s.opts.CustomModsURL)
		}
		if err == nil {
			cm, err = SyncCustomMods(archive, inst, managed, nextFP, backupDir)
		}
		if err != nil {
			res.CustomErr = err
			rep.Warn("your server's extra mods could not be synced: " + err.Error())
		} else {
			res.CustomMods, managed = cm, cm.Installed
			s.Plan.ExtraMods = withoutNames(s.Plan.ExtraMods, cm.Installed)
		}
	}

	if s.opts.CustomModsURL == "" && len(managed) > 0 {
		// Sync switched off (e.g. moved to another server): take out the jars it put in.
		rep.Step("Removing the old server's extra mods")
		cm, err := RemoveCustomMods(inst, managed, backupDir)
		if err != nil {
			res.CustomErr = err
			rep.Warn("the old server's extra mods could not be removed: " + err.Error())
		} else {
			res.CustomMods, managed = cm, nil
		}
	}
	st := &State{Version: s.opts.Target, Baseline: nextFP, CustomMods: managed,
		CustomModsURL: s.opts.CustomModsURL, CustomModsAsked: true}
	if err := SaveState(inst.Dir, st); err != nil {
		return nil, fmt.Errorf("update applied, but saving updater state failed: %w", err)
	}
	// Keep only the newest backup, but never let a run that changed nothing (and so
	// made no backup) throw away the last real one.
	if _, err := os.Stat(backupDir); err == nil {
		pruneBackups(filepath.Join(inst.Dir, StateDir), "backup-"+ts)
	} else {
		res.BackupDir = ""
	}
	return res, nil
}

// Close releases the downloaded pack. Safe to call more than once.
func (s *Session) Close() {
	if s.next != nil {
		s.next.Close()
		s.next = nil
	}
	if s.zipPath != "" {
		os.Remove(s.zipPath)
		s.zipPath = ""
	}
}

// pruneBackups removes older backup-* dirs, keeping keep.
func pruneBackups(dir, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "backup-") && e.Name() != keep {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// withoutNames drops jars (possibly ".disabled") the custom-mods sync now manages: on a
// first run they were already on disk and looked like the player's own mods.
func withoutNames(mods, drop []string) []string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	var out []string
	for _, m := range mods {
		if !skip[strings.TrimSuffix(m, ".disabled")] {
			out = append(out, m)
		}
	}
	return out
}

func itemProgress(rep Reporter) func(done, total int) {
	return func(done, total int) { rep.Progress(int64(done), int64(total)) }
}
