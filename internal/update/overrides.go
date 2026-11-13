package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// An override is a jar the player uses instead of one GTNH ships: a patched or newer
// build of a GTNH mod. The rules:
//
//   - GTNH's jar of that mod stays in mods/, disabled in Prism (foo.jar.disabled), for as
//     long as the override is in use; the player's jar sits next to it, enabled.
//   - An update that leaves GTNH's jar of that mod alone keeps it that way. One that
//     changes it (a new version, a new name, or the mod gone) asks the player: keep
//     theirs (GTNH's new jar is installed disabled) or take GTNH's (the player's jar goes
//     to the backup, so undo brings it back). An override whose jar the player deleted is
//     dropped and GTNH's jar switched back on; one GTNH now ships byte for byte is GTNH's.
//   - Taking an override out moves the player's jar to the replaced-mods folder (never
//     deleted) and switches GTNH's jar back on.
//   - The server-mods sync treats an override like GTNH's own jar: a server jar of the
//     same mod is left out, never set beside it.

// Override is one of the player's jars standing in for jars GTNH ships.
type Override struct {
	Jar string `json:"jar"` // the player's jar in mods/
	// Replaces are GTNH's jars it stands in for, as named in mods/ without ".disabled".
	Replaces []string `json:"replaces,omitempty"`
}

// OverrideCheck is what CheckOverride found out about a jar the player wants to use.
type OverrideCheck struct {
	Src    string   // the jar file
	Jar    string   // its name in mods/
	InMods bool     // it's in mods/ already, so it isn't copied
	Same   []string // GTNH's jars of the same mod: what it replaces
	All    []string // GTNH's jars it could replace, for the player to pick from when Same is empty
}

// CheckOverride looks at the jar src the player wants to use instead of one of GTNH's
// and finds the GTNH jars it replaces. Its errors are sentences for the player.
func CheckOverride(inst prism.Instance, src string) (*OverrideCheck, error) {
	fi, err := os.Stat(src)
	switch {
	case src == "":
		return nil, errors.New("Tell me where the .jar file is.")
	case err != nil:
		return nil, errors.New("I can't find that file.")
	case fi.IsDir():
		return nil, errors.New("That's a folder — I need the .jar file itself.")
	case !strings.HasSuffix(strings.ToLower(src), ".jar"):
		return nil, errors.New("That isn't a .jar file.")
	}
	if zr, err := zip.OpenReader(src); err != nil {
		return nil, errors.New("That file isn't a mod: it doesn't open as a .jar.")
	} else {
		zr.Close()
	}
	st, err := LoadState(inst.Dir)
	if err != nil {
		return nil, fmt.Errorf("I couldn't read my notes on this instance: %w", err)
	}
	modsDir := filepath.Join(inst.GameDir, "mods")
	c := &OverrideCheck{Src: src, Jar: filepath.Base(src), InMods: sameDir(filepath.Dir(src), modsDir)}
	if badModName(c.Jar) {
		return nil, errors.New("That jar's name has characters Windows doesn't allow — rename it first.")
	}
	for _, o := range overridesOf(st) {
		if strings.EqualFold(o.Jar, c.Jar) {
			return nil, fmt.Errorf("You use %s instead of GTNH's %s already.", o.Jar, strings.Join(o.Replaces, ", "))
		}
	}
	for n := range managedOf(st) {
		if strings.EqualFold(n, c.Jar) {
			return nil, fmt.Errorf("%s is one of your server's mods.", c.Jar)
		}
	}
	files := modsFiles(modsDir)
	gtnh, known := gtnhJars(st, files)
	if c.InMods && known && slices.Contains(gtnh, c.Jar) {
		return nil, fmt.Errorf("%s is one of GTNH's own mods.", c.Jar)
	}
	for _, g := range gtnh {
		if !c.InMods || g != c.Jar {
			c.All = append(c.All, g)
		}
	}
	if len(c.All) == 0 {
		return nil, errors.New("I can't find any of GTNH's mods in this instance to replace.")
	}
	idx := newJarIndex(c.All, func(n string) []string { return fileModIDs(filepath.Join(modsDir, n)) })
	c.Same = idx.same(c.Jar, fileModIDs(src))
	if !c.InMods {
		if have, ok := newDiskNames(files).find(c.Jar); ok && !slices.Contains(c.Same, have) {
			return nil, fmt.Errorf("There's a different %s in the mods folder already.", have)
		}
	}
	return c, nil
}

// gtnhJars are the enabled jars of mods/ (files) that came with GTNH: the pack's jars
// when the instance's pack is known (known true), else every jar that neither the
// server-mods sync nor an override put there.
func gtnhJars(st *State, files []string) (jars []string, known bool) {
	var base map[string]pack.Fingerprint
	if st != nil {
		base = st.Baseline
	}
	pk := packJars(base, files)
	skip := map[string]bool{}
	for n := range managedOf(st) {
		skip[strings.ToLower(n)] = true
	}
	for _, o := range overridesOf(st) {
		skip[strings.ToLower(o.Jar)] = true
	}
	for _, f := range files {
		low := strings.ToLower(f)
		if strings.HasSuffix(f, ".disabled") || skip[low] {
			continue
		}
		if _, ok := pk[low]; pk == nil || ok {
			jars = append(jars, f)
		}
	}
	return jars, pk != nil
}

// AddOverride puts the jar src in mods/ in place of GTNH's jars replaces (from
// CheckOverride's All), which are disabled. It refuses while the game runs; a failure
// puts everything back.
func AddOverride(inst prism.Instance, src string, replaces []string) (err error) {
	if prism.Running(inst) {
		return ErrGameRunning
	}
	c, err := CheckOverride(inst, src)
	if err != nil {
		return err
	}
	if len(replaces) == 0 {
		return errors.New("Tell me which of GTNH's mods it replaces.")
	}
	for _, r := range replaces {
		if !slices.Contains(c.All, r) {
			return fmt.Errorf("%s isn't one of GTNH's mods in this instance.", r)
		}
	}
	modsDir := filepath.Join(inst.GameDir, "mods")
	if !c.InMods {
		if have, ok := newDiskNames(modsFiles(modsDir)).find(c.Jar); ok && !slices.Contains(replaces, have) {
			return fmt.Errorf("There's a different %s in the mods folder already.", have)
		}
	}
	var undo []func() error
	defer func() {
		if err != nil {
			for i := len(undo) - 1; i >= 0; i-- {
				undo[i]()
			}
		}
	}()
	for _, r := range replaces {
		on, off := filepath.Join(modsDir, r), filepath.Join(modsDir, r+".disabled")
		if fileExists(off) { // an old disabled copy: out of the way, never deleted
			if err := keepFile(off, filepath.Join(inst.Dir, StateDir, ReplacedMods), r+".disabled"); err != nil {
				return err
			}
		}
		if err := os.Rename(on, off); err != nil {
			return fmt.Errorf("I couldn't switch off GTNH's %s: %w", r, err)
		}
		undo = append(undo, func() error { return os.Rename(off, on) })
	}
	if !c.InMods {
		dst := filepath.Join(modsDir, c.Jar)
		tmp, err := writeTemp(dst, func() (io.ReadCloser, error) { return os.Open(src) })
		if err != nil {
			return fmt.Errorf("I couldn't copy %s into the mods folder: %w", c.Jar, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("I couldn't copy %s into the mods folder: %w", c.Jar, err)
		}
		undo = append(undo, func() error { return os.Remove(dst) })
	}
	return UpdateState(inst.Dir, func(st *State) {
		st.Overrides = append(st.Overrides, Override{Jar: c.Jar, Replaces: slices.Sorted(slices.Values(replaces))})
	})
}

// RemoveOverride stops using the override jar: the player's jar goes to the
// replaced-mods folder and GTNH's jars come back on. It refuses while the game runs.
func RemoveOverride(inst prism.Instance, jar string) error {
	if prism.Running(inst) {
		return ErrGameRunning
	}
	st, err := LoadState(inst.Dir)
	if err != nil {
		return fmt.Errorf("I couldn't read my notes on this instance: %w", err)
	}
	i := slices.IndexFunc(overridesOf(st), func(o Override) bool { return o.Jar == jar })
	if i < 0 {
		return fmt.Errorf("You don't use %s instead of one of GTNH's mods.", jar)
	}
	o := st.Overrides[i]
	modsDir := filepath.Join(inst.GameDir, "mods")
	// The player's jar, or (disabled in Prism) its disabled name, unless that is GTNH's
	// jar of the same name.
	name := o.Jar
	if !fileExists(filepath.Join(modsDir, name)) && !slices.Contains(o.Replaces, o.Jar) {
		name += ".disabled"
	}
	if p := filepath.Join(modsDir, name); fileExists(p) {
		if err := keepFile(p, filepath.Join(inst.Dir, StateDir, ReplacedMods), name); err != nil {
			return fmt.Errorf("I couldn't move %s out of the mods folder: %w", name, err)
		}
	}
	st.Overrides = slices.Delete(st.Overrides, i, i+1)
	var errs []error
	for _, r := range o.Replaces {
		if err := switchOn(modsDir, r, st); err != nil {
			errs = append(errs, err)
		}
	}
	if err := SaveState(inst.Dir, st); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// switchOn enables GTNH's jar r again, unless something has its enabled name or another
// override stands in for it.
func switchOn(modsDir, r string, st *State) error {
	for _, o := range overridesOf(st) {
		if o.Jar == r || slices.Contains(o.Replaces, r) {
			return nil
		}
	}
	on, off := filepath.Join(modsDir, r), filepath.Join(modsDir, r+".disabled")
	if fileExists(on) || !fileExists(off) {
		return nil
	}
	if err := os.Rename(off, on); err != nil {
		return fmt.Errorf("I couldn't switch GTNH's %s back on: %w", r, err)
	}
	return nil
}

// switchOff disables GTNH's jar r; an old disabled copy goes to the replaced-mods folder.
func switchOff(inst prism.Instance, r string) error {
	modsDir := filepath.Join(inst.GameDir, "mods")
	on, off := filepath.Join(modsDir, r), filepath.Join(modsDir, r+".disabled")
	if !fileExists(on) {
		return nil
	}
	if fileExists(off) {
		if err := keepFile(off, filepath.Join(inst.Dir, StateDir, ReplacedMods), r+".disabled"); err != nil {
			return err
		}
	}
	if err := os.Rename(on, off); err != nil {
		return fmt.Errorf("I couldn't switch off GTNH's %s: %w", r, err)
	}
	return nil
}

func overridesOf(st *State) []Override {
	if st == nil {
		return nil
	}
	return st.Overrides
}

func sameDir(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// OverrideChange is what an update does about one of the player's overrides whose mod
// the new version changes.
type OverrideChange struct {
	Jar string // the player's jar
	// Old and New are GTNH's jars of that mod in the installed and in the new version;
	// New is empty when GTNH doesn't ship the mod any more.
	Old, New []string
	Gone     bool // the player's jar isn't in mods/ any more: GTNH's comes back
	Shipped  bool // the new version ships the player's jar itself
}

// Asks reports whether the player decides: keep their jar (KeepMine) or take GTNH's
// (TakeNew). Otherwise GTNH's jar comes back on its own.
func (c OverrideChange) Asks() bool { return !c.Gone && !c.Shipped }

// ovPlan is one override in an update: what it covers before and after, and the change
// (nil: the update leaves its mod alone).
type ovPlan struct {
	o        Override
	old, new []string
	change   *OverrideChange
}

// modJars are the jars at the top of mods/ in a pack's fingerprints, by name.
func modJars(fps map[string]pack.Fingerprint) map[string]pack.Fingerprint {
	out := map[string]pack.Fingerprint{}
	for p, fp := range fps {
		if n, ok := strings.CutPrefix(p, pack.GameDir+"mods/"); ok && !strings.Contains(n, "/") && strings.HasSuffix(n, ".jar") {
			out[n] = fp
		}
	}
	return out
}

// planOverrides works out what the update to next does to each override. nextIDs reads
// the mod ids of a jar of the new pack (nil: by name only).
func (s *Session) planOverrides(next map[string]pack.Fingerprint, nextIDs func(string) []string) {
	s.overrides, s.Overrides = nil, nil
	if len(overridesOf(s.state)) == 0 {
		return
	}
	modsDir := filepath.Join(s.opts.Instance.GameDir, "mods")
	nextJars, baseJars := modJars(next), modJars(s.base)
	// Only a jar the installed version didn't have can be a new jar of a replaced mod: a
	// jar of the same name is the same mod as before.
	var fresh []string
	for n := range nextJars {
		if _, ok := baseJars[n]; !ok {
			fresh = append(fresh, n)
		}
	}
	sort.Strings(fresh)
	if nextIDs == nil {
		nextIDs = func(string) []string { return nil }
	}
	var idx *jarIndex
	diskIDs := memoIDs(func(n string) []string {
		if ids := fileModIDs(filepath.Join(modsDir, n)); ids != nil {
			return ids
		}
		return fileModIDs(filepath.Join(modsDir, n+".disabled"))
	})
	for _, o := range s.state.Overrides {
		v := ovPlan{o: o, old: slices.Sorted(slices.Values(o.Replaces))}
		set := map[string]bool{}
		for _, n := range append([]string{o.Jar}, o.Replaces...) {
			if _, ok := nextJars[n]; ok {
				set[n] = true
			}
		}
		if len(fresh) > 0 {
			if idx == nil {
				idx = newJarIndex(fresh, memoIDs(nextIDs))
			}
			for _, who := range append([]string{o.Jar}, o.Replaces...) {
				for _, n := range idx.same(who, diskIDs(who)) {
					set[n] = true
				}
			}
		}
		v.new = sortedKeys(set)
		changed := !slices.Equal(v.old, v.new)
		for _, n := range v.new {
			changed = changed || baseJars[n] != nextJars[n]
		}
		c := OverrideChange{Jar: o.Jar, Old: v.old, New: v.new}
		jarFP, err := pack.FingerprintFile(filepath.Join(modsDir, o.Jar))
		shipFP, ships := nextJars[o.Jar]
		switch {
		case err != nil && !fileExists(filepath.Join(modsDir, o.Jar+".disabled")):
			c.Gone = true
		case err == nil && ships && shipFP == jarFP:
			c.Shipped = true
		case !changed:
			s.overrides = append(s.overrides, v)
			continue
		}
		v.change = &c
		s.overrides = append(s.overrides, v)
		s.Overrides = append(s.Overrides, c)
	}
}

// keeps reports whether the override v stays after the update.
func (s *Session) keeps(v ovPlan) bool {
	return v.change == nil || v.change.Asks() && s.OverrideChoice(v.o.Jar) == KeepMine
}

// holds are what the overrides ask of the pack plan, with the player's choices.
func (s *Session) holds(next map[string]pack.Fingerprint) modHolds {
	h := modHolds{held: map[string]bool{}, released: map[string]bool{}}
	for _, v := range s.overrides {
		paths := []string{}
		for _, n := range append(slices.Clone(v.old), v.new...) {
			paths = append(paths, pack.GameDir+"mods/"+n)
		}
		if s.keeps(v) {
			for _, p := range paths {
				h.held[p] = true
			}
			h.own = append(h.own, v.o.Jar)
			continue
		}
		for _, p := range paths {
			h.released[p] = true
		}
		if _, ships := next[pack.GameDir+"mods/"+v.o.Jar]; !ships && !v.change.Gone {
			h.drop = append(h.drop, v.o.Jar)
		}
	}
	for p := range h.held {
		delete(h.released, p) // another override still stands in for it
	}
	return h
}

// overridesAfter are the overrides once the update is in: those kept, standing in for
// the new version's jars. One whose mod GTNH dropped is just a mod the player added.
func (s *Session) overridesAfter() []Override {
	var out []Override
	for _, v := range s.overrides {
		if s.keeps(v) && len(v.new) > 0 {
			out = append(out, Override{Jar: v.o.Jar, Replaces: v.new})
		}
	}
	return out
}

// OverrideChoice is the player's choice for the override jar; KeepMine when none was made.
func (s *Session) OverrideChoice(jar string) Choice { return s.ovChoice[jar] }

// ChooseOverride sets the choice for the override jar and plans the update again.
func (s *Session) ChooseOverride(jar string, c Choice) {
	if s.ovChoice == nil {
		s.ovChoice = map[string]Choice{}
	}
	s.ovChoice[jar] = c
	s.replan()
}

// ChooseAllOverrides sets c for every override the player decides about.
func (s *Session) ChooseAllOverrides(c Choice) {
	for _, ch := range s.Overrides {
		if ch.Asks() {
			if s.ovChoice == nil {
				s.ovChoice = map[string]Choice{}
			}
			s.ovChoice[ch.Jar] = c
		}
	}
	s.replan()
}

// mergeOverrides is the overrides after an undo: those of the state before the update
// (prev), then those added since (cur). healOverrides drops the ones whose jar is gone.
func mergeOverrides(prev, cur []Override) []Override {
	out := slices.Clone(prev)
	for _, o := range cur {
		if !slices.ContainsFunc(prev, func(p Override) bool { return p.Jar == o.Jar }) {
			out = append(out, o)
		}
	}
	return out
}

// healOverrides makes st's overrides fit mods/ as it is (after an undo): an override
// whose jar is gone is dropped and GTNH's jars switched back on; one standing in for jars
// that aren't there any more finds GTNH's jars of its mod again; every jar an override
// stands in for is disabled. What can't be done is a warning.
func healOverrides(inst prism.Instance, st *State, rep Reporter) {
	if len(st.Overrides) == 0 {
		return
	}
	modsDir := filepath.Join(inst.GameDir, "mods")
	files := modsFiles(modsDir)
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	all := st.Overrides
	st.Overrides = nil
	var gone []Override
	for _, o := range all {
		if !have[o.Jar] && !have[o.Jar+".disabled"] {
			gone = append(gone, o)
			continue
		}
		var keep []string
		missing := len(o.Replaces) == 0
		for _, r := range o.Replaces {
			if have[r] || have[r+".disabled"] {
				keep = append(keep, r)
			} else {
				missing = true
			}
		}
		if missing {
			tmp := *st
			tmp.Overrides = all
			gtnh, _ := gtnhJars(&tmp, files)
			idx := newJarIndex(gtnh, func(n string) []string { return fileModIDs(filepath.Join(modsDir, n)) })
			for _, n := range idx.same(o.Jar, fileModIDs(filepath.Join(modsDir, o.Jar))) {
				if !slices.Contains(keep, n) {
					keep = append(keep, n)
				}
			}
		}
		for _, r := range keep {
			if r == o.Jar {
				continue // GTNH's jar of the same name is the disabled one already
			}
			if err := switchOff(inst, r); err != nil {
				rep.Warn(err.Error())
			}
		}
		if len(keep) > 0 {
			o.Replaces = slices.Sorted(slices.Values(keep))
			st.Overrides = append(st.Overrides, o)
		}
	}
	for _, o := range gone {
		for _, r := range o.Replaces {
			if err := switchOn(modsDir, r, st); err != nil {
				rep.Warn(err.Error())
			}
		}
	}
}
