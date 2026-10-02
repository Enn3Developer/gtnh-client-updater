package update

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// Kind is what the updater does to one path.
type Kind int

const (
	Install  Kind = iota // write the new pack's file (create or overwrite)
	Remove               // delete: the pack dropped it and the player never changed it
	Conflict             // player changed it and so did the pack: the Choice decides (see Plan.Choose)
)

// Choice is the player's decision for one config conflict.
type Choice int

const (
	KeepMine Choice = iota // keep the player's file, write the pack's as .mcnew
	TakeNew                // replace the player's file with the pack's (backed up)
)

// Recommended is the choice to offer by default: many GTNH mods rewrite their configs on
// every game start, so most conflicts are noise and the pack's version is the safe pick
// (the player's file is backed up).
const Recommended Choice = TakeNew

// Action is one planned change. Path is the canonical pack path; Disk is where it lands.
type Action struct {
	Kind Kind
	Path string
	Disk string
}

// Plan is the full set of changes for one update, plus what the player should know.
type Plan struct {
	Actions []Action
	// Kept lists pack paths the player modified that the pack either left alone or
	// removed; their version stays.
	Kept []string
	// ExtraMods lists jars in mods/ that come from neither pack nor the custom-mods sync,
	// i.e. mods the player added by hand. They are left in place.
	ExtraMods []string
	// BaselineMatch is the share of the baseline's mod jars found on disk (0..1). A low
	// value means the installed-version guess is probably wrong.
	BaselineMatch float64
	// choices holds the per-path conflict decisions; unset paths are KeepMine. Unexported
	// so it stays out of JSON and other packages go through the methods.
	choices map[string]Choice
}

// Count returns how many actions of a kind the plan holds.
func (p *Plan) Count(k Kind) int {
	n := 0
	for _, a := range p.Actions {
		if a.Kind == k {
			n++
		}
	}
	return n
}

// Conflicts returns the paths both the player and the pack changed, whatever the choice.
func (p *Plan) Conflicts() []string {
	var out []string
	for _, a := range p.Actions {
		if a.Kind == Conflict {
			out = append(out, a.Path)
		}
	}
	return out
}

// ChoiceOf returns the choice for path; KeepMine when none was made.
func (p *Plan) ChoiceOf(path string) Choice {
	return p.choices[path] // nil map and missing key both yield KeepMine
}

// Choose sets the choice for one path, overwriting any earlier one.
func (p *Plan) Choose(path string, c Choice) {
	if p.choices == nil {
		p.choices = map[string]Choice{}
	}
	p.choices[path] = c
}

// ChooseAll sets c for every Conflict action's Path.
func (p *Plan) ChooseAll(c Choice) {
	for _, path := range p.Conflicts() {
		p.Choose(path, c)
	}
}

// Chosen returns the sorted Paths of the Conflict actions whose choice is c.
func (p *Plan) Chosen(c Choice) []string {
	var out []string
	for _, path := range p.Conflicts() {
		if p.ChoiceOf(path) == c {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// skipped are pack paths the generic reconcile never touches. instance.cfg holds the
// player's launcher settings (Java path, memory, JVM args); only its name is updated,
// separately.
var skipped = map[string]bool{"instance.cfg": true}

// packOwned paths are not meant to be edited by players: the new pack always wins
// (no .mcnew). That is the mods and everything outside the game dir (patches/,
// libraries/, mmc-pack.json, the icon).
func packOwned(p string) bool {
	return strings.HasPrefix(p, pack.GameDir+"mods/") || !strings.HasPrefix(p, pack.GameDir)
}

// DiskPath maps a canonical pack path into the instance.
func DiskPath(inst prism.Instance, p string) string {
	if rest, ok := strings.CutPrefix(p, pack.GameDir); ok {
		return filepath.Join(inst.GameDir, filepath.FromSlash(rest))
	}
	return filepath.Join(inst.Dir, filepath.FromSlash(p))
}

type fp struct {
	ok bool
	v  pack.Fingerprint
}

func (a fp) eq(b fp) bool { return a.ok == b.ok && (!a.ok || a.v == b.v) }

func lookup(m map[string]pack.Fingerprint, p string) fp {
	v, ok := m[p]
	return fp{ok, v}
}

// Scan fingerprints the files on disk for every path in the union of base and next,
// plus the ".disabled" twin of each mod (Prism disables a mod by renaming it).
// progress is called with files scanned so far.
func Scan(inst prism.Instance, base, next map[string]pack.Fingerprint, progress func(done, total int)) map[string]pack.Fingerprint {
	paths := unionPaths(base, next)
	cur := map[string]pack.Fingerprint{}
	for i, p := range paths {
		for _, q := range []string{p, p + ".disabled"} {
			if q != p && !isMod(p) {
				continue
			}
			if v, err := pack.FingerprintFile(DiskPath(inst, q)); err == nil {
				cur[q] = v
			}
		}
		if progress != nil {
			progress(i+1, len(paths))
		}
	}
	return cur
}

func isMod(p string) bool { return strings.HasPrefix(p, pack.GameDir+"mods/") }

func unionPaths(maps ...map[string]pack.Fingerprint) []string {
	set := map[string]bool{}
	for _, m := range maps {
		for k := range m {
			if !skipped[k] {
				set[k] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MakePlan decides what happens to each path. base is the installed version's pack,
// next the new pack, cur the scan of the disk (see Scan). customMods are the file names
// the custom-mods sync manages; they are excluded from ExtraMods.
func MakePlan(inst prism.Instance, base, next, cur map[string]pack.Fingerprint, customMods []string) *Plan {
	pl := &Plan{}
	for _, p := range unionPaths(base, next) {
		B, N := lookup(base, p), lookup(next, p)
		target, C := p, lookup(cur, p)
		if isMod(p) && !C.ok {
			if d := lookup(cur, p+".disabled"); d.ok {
				target, C = p+".disabled", d // disabled in Prism: update it, keep it disabled
			}
		}
		act := func(k Kind) {
			pl.Actions = append(pl.Actions, Action{Kind: k, Path: target, Disk: DiskPath(inst, target)})
		}
		if packOwned(p) {
			// A deleted pack file is restored: a missing GTNH jar is almost always an
			// accident (or an antivirus) and breaks the pack. Switching a mod off is
			// done by disabling it in Prism, which is respected above.
			switch {
			case N.ok && !C.eq(N):
				act(Install)
			case !N.ok && C.ok && C.eq(B):
				act(Remove)
			case !N.ok && C.ok:
				pl.Kept = append(pl.Kept, target)
			}
			continue
		}
		// Conffile rules, identical to the server updater's config reconcile.
		switch {
		case C.eq(B):
			if N.ok {
				if !C.eq(N) {
					act(Install)
				}
			} else if C.ok {
				act(Remove)
			}
		case N.eq(C):
			// Both sides converged on the same content.
		case N.eq(B):
			pl.Kept = append(pl.Kept, p) // pack did not touch it: keep the player's
		case N.ok:
			act(Conflict)
		default:
			pl.Kept = append(pl.Kept, p) // pack removed it, player changed it: keep
		}
	}
	pl.ExtraMods = extraMods(inst, base, next, customMods)
	pl.BaselineMatch = baselineMatch(base, cur)
	return pl
}

func extraMods(inst prism.Instance, base, next map[string]pack.Fingerprint, customMods []string) []string {
	ents, err := os.ReadDir(filepath.Join(inst.GameDir, "mods"))
	if err != nil {
		return nil
	}
	custom := map[string]bool{}
	for _, c := range customMods {
		custom[c] = true
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".jar.disabled")) {
			continue
		}
		p := path.Join(pack.GameDir+"mods", strings.TrimSuffix(name, ".disabled"))
		if _, ok := base[p]; ok {
			continue
		}
		if _, ok := next[p]; ok {
			continue
		}
		if custom[strings.TrimSuffix(name, ".disabled")] {
			continue
		}
		out = append(out, name)
	}
	return out
}

func baselineMatch(base, cur map[string]pack.Fingerprint) float64 {
	total, found := 0, 0
	for p := range base {
		if !isMod(p) || !strings.HasSuffix(p, ".jar") {
			continue
		}
		total++
		if _, ok := cur[p]; ok {
			found++
		} else if _, ok := cur[p+".disabled"]; ok {
			found++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(found) / float64(total)
}
