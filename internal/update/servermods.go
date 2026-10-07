package update

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// ModsSyncOptions describes a sync of an instance's server mods on its own: before the
// game starts, or after the player changed the link.
type ModsSyncOptions struct {
	Context  context.Context // cancels the download; nil = never
	Client   *http.Client
	Instance prism.Instance
	// URL overrides the link the instance remembers ("none" = no link) and is saved;
	// "" keeps the remembered one.
	URL string
}

// ModsSync is a prepared sync: the server's archive fetched (or, when that failed, the
// copy of the last one) and the plan worked out. Apply carries it out, Close throws it
// away.
type ModsSync struct {
	Plan *ModsPlan
	// FetchErr is why the server's archive couldn't be fetched; the plan then works from
	// the copy of the last one, or changes nothing.
	FetchErr error
	Link     string // the link synced from; "" = none, so the plan removes what one installed

	inst     prism.Instance
	state    *State
	archive  *ModsArchive
	override bool // Link came from ModsSyncOptions.URL: save it
}

// PrepareModsSync fetches the server's archive and plans the sync without touching
// mods/.
func PrepareModsSync(opts ModsSyncOptions, rep Reporter) (*ModsSync, error) {
	inst := opts.Instance
	st, err := LoadState(inst.Dir)
	if err != nil {
		return nil, fmt.Errorf("read updater state: %w", err)
	}
	s := &ModsSync{inst: inst, state: st}
	if st != nil {
		s.Link = st.CustomModsURL
	}
	switch opts.URL {
	case "":
	case "none":
		s.Link, s.override = "", true
	default:
		if err := CheckCustomModsURL(opts.URL); err != nil {
			return nil, err
		}
		s.Link, s.override = opts.URL, true
	}
	stateDir := filepath.Join(inst.Dir, StateDir)
	if s.Link != "" {
		rep.Step("Checking your server's mods")
		s.archive, s.FetchErr = fetchMods(opts.Context, opts.Client, s.Link, stateDir, rep.Progress)
		if errors.Is(s.FetchErr, context.Canceled) {
			return nil, s.FetchErr
		}
		if s.FetchErr != nil {
			s.archive = cachedMods(s.Link, stateDir)
		}
	}
	if s.Link != "" && s.archive == nil {
		s.Plan = keepMods(inst, st)
		return s, nil
	}
	var baseline map[string]pack.Fingerprint
	if st != nil {
		baseline = st.Baseline
	}
	s.Plan = planMods(modsInputFor(s.archive, inst, st, baseline, true, false))
	return s, nil
}

// Apply carries out the plan and saves what the sync manages now. It refuses while the
// game runs from the instance.
func (s *ModsSync) Apply(rep Reporter) (*ModsPlan, error) {
	defer s.Close()
	if prism.Running(s.inst) {
		return nil, ErrGameRunning
	}
	if s.Link == "" && !s.override && len(managedOf(s.state)) == 0 {
		return s.Plan, nil // no link and nothing from an old one: nothing to note either
	}
	if s.Plan.writes() {
		rep.Step("Updating your server's mods")
	}
	if err := applyModsPlan(s.Plan, s.archive, s.inst); err != nil {
		return nil, err
	}
	err := UpdateState(s.inst.Dir, func(st *State) {
		if s.override {
			st.CustomModsURL, st.CustomModsAsked = s.Link, true
		}
		setManaged(st, s.Plan.Managed)
		switch {
		case s.Link == "":
			st.CustomModsSynced = time.Time{}
		case s.FetchErr == nil:
			st.CustomModsSynced = time.Now()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("the mods are in place, but saving my notes failed: %w", err)
	}
	if s.Link == "" {
		dropModsCache(filepath.Join(s.inst.Dir, StateDir))
	}
	return s.Plan, nil
}

// Close releases the archive. Safe to call more than once.
func (s *ModsSync) Close() {
	s.archive.Close()
}

// modsInputFor gathers what planMods looks at for the archive a (nil = an empty one) in
// inst's mods folder as it is now. packFP are the fingerprints of the pack the
// instance has (or gets); ids turns on the same-mod checks.
func modsInputFor(a *ModsArchive, inst prism.Instance, st *State, packFP map[string]pack.Fingerprint, ids, checkAll bool) modsInput {
	modsDir := filepath.Join(inst.GameDir, "mods")
	files := modsFiles(modsDir)
	in := modsInput{
		archive:  map[string]pack.Fingerprint{},
		files:    files,
		fp:       memoFP(modsDir),
		pack:     packJars(packFP, files),
		managed:  managedOf(st),
		checkAll: checkAll,
	}
	if a != nil {
		for n, zf := range a.jars {
			in.archive[n] = pack.Fingerprint{Size: zf.UncompressedSize64, CRC: zf.CRC32}
		}
		in.ignored = a.ignored
	}
	if ids {
		in.serverIDs = memoIDs(func(n string) []string {
			if zf, ok := a.jarOf(n); ok {
				return archiveModIDs(zf)
			}
			return nil
		})
		in.diskIDs = memoIDs(func(n string) []string { return fileModIDs(filepath.Join(modsDir, n)) })
	}
	return in
}

// modsFiles lists the jar files of a mods folder (*.jar and *.jar.disabled), sorted.
func modsFiles(modsDir string) []string {
	ents, err := os.ReadDir(modsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() && (strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".jar.disabled")) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// memoFP fingerprints files of dir on first use.
func memoFP(dir string) func(string) (pack.Fingerprint, bool) {
	seen := map[string]*pack.Fingerprint{}
	return func(name string) (pack.Fingerprint, bool) {
		fp, ok := seen[name]
		if !ok {
			if v, err := pack.FingerprintFile(filepath.Join(dir, name)); err == nil {
				fp = &v
			}
			seen[name] = fp
		}
		if fp == nil {
			return pack.Fingerprint{}, false
		}
		return *fp, true
	}
}

func memoIDs(read func(string) []string) func(string) []string {
	seen := map[string][]string{}
	return func(name string) []string {
		ids, ok := seen[name]
		if !ok {
			ids = read(name)
			seen[name] = ids
		}
		return ids
	}
}

// managedOf is the managed jars of st with their recorded content (nil: not recorded).
func managedOf(st *State) map[string]*pack.Fingerprint {
	out := map[string]*pack.Fingerprint{}
	if st == nil {
		return out
	}
	for _, n := range st.CustomMods {
		if fp, ok := st.CustomModsFP[n]; ok {
			out[n] = &fp
		} else {
			out[n] = nil
		}
	}
	return out
}

// setManaged records the jars the sync manages now.
func setManaged(st *State, m map[string]pack.Fingerprint) {
	st.CustomMods, st.CustomModsFP = sortedKeys(m), nil
	if len(m) > 0 {
		st.CustomModsFP = maps.Clone(m)
	}
}

// packJars are GTNH's jars in mods/ by lower-cased name, read from a pack's
// fingerprints; nil when they aren't known: there are none, or so few of them are in
// files that they can't be this instance's (it changed GTNH versions without me).
func packJars(fps map[string]pack.Fingerprint, files []string) map[string]string {
	out := map[string]string{}
	for p := range fps {
		name, ok := strings.CutPrefix(p, pack.GameDir+"mods/")
		if ok && !strings.Contains(name, "/") && strings.HasSuffix(name, ".jar") {
			out[strings.ToLower(name)] = name
		}
	}
	if len(out) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, f := range files {
		have[strings.ToLower(strings.TrimSuffix(f, ".disabled"))] = true
	}
	found := 0
	for low := range out {
		if have[low] {
			found++
		}
	}
	if found*5 < len(out)*4 {
		return nil
	}
	return out
}

// sortedKeys returns the keys of m, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
