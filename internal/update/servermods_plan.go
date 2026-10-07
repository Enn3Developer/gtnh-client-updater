package update

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// The rules of a server-mods sync, in order of precedence:
//
//   - GTNH's own jars win: a server jar that GTNH ships too (same file name, or the same
//     mod under another name) is left out, and a copy the sync installed earlier goes.
//   - The server's jars win over the player's own copies of them (same file name, or the
//     same mod): the player's file is moved to the replaced-mods folder, never deleted.
//   - The sync only removes or replaces files it installed itself and the player didn't
//     change; a changed one the server dropped stays, unmanaged.
//   - A jar disabled in Prism (foo.jar.disabled) is updated in its disabled name.
//   - Without a known pack (an instance this launcher never installed or updated) every
//     jar the sync doesn't manage might be GTNH's, so nothing of the player's is touched.

// ModKind is what a server-mods sync does about one jar.
type ModKind int

const (
	ModAdd      ModKind = iota // a new jar from the server
	ModUpdate                  // the server's jar changed: our copy is replaced
	ModRemove                  // the server dropped it (or it's a stray copy): our file goes
	ModAdopt                   // already in mods/ exactly as the server has it: managed from now on
	ModReplace                 // the player's own jar of that name makes way for the server's
	ModSetAside                // the player's own jar of the same mod makes way for the server's
	ModKeep                    // dropped by the server, but the player changed our copy: it stays, unmanaged
	ModSkip                    // GTNH ships the same mod, or a different jar of that name is there: left out
)

// ModChange is one step of a sync. Name is the server's jar (for ModSetAside the
// player's jar), Disk the file in mods/ it writes, moves or keeps ("" = none), With the
// jar behind a ModSkip or a ModSetAside. Preserve marks a Disk that is the player's: it
// is moved to the replaced-mods folder, never deleted.
type ModChange struct {
	Kind     ModKind
	Name     string
	Disk     string
	With     string
	Preserve bool
}

// ModsPlan is what a sync does, worked out without touching anything.
type ModsPlan struct {
	Changes []ModChange
	// Managed is what the sync manages afterwards: jar name -> content.
	Managed map[string]pack.Fingerprint
	// Ignored are archive entries left out: jars in folders, odd or doubled names.
	Ignored []string
}

// Of returns the changes of kind k, in plan order.
func (p *ModsPlan) Of(k ModKind) []ModChange {
	if p == nil {
		return nil
	}
	var out []ModChange
	for _, c := range p.Changes {
		if c.Kind == k {
			out = append(out, c)
		}
	}
	return out
}

// Count returns how many changes of kind k the plan holds.
func (p *ModsPlan) Count(k ModKind) int { return len(p.Of(k)) }

// Names returns the Name of every change of kind k, in plan order.
func (p *ModsPlan) Names(k ModKind) []string {
	var out []string
	for _, c := range p.Of(k) {
		out = append(out, c.Name)
	}
	return out
}

// Installed returns the jars the sync manages afterwards, sorted.
func (p *ModsPlan) Installed() []string {
	if p == nil {
		return nil
	}
	return sortedKeys(p.Managed)
}

// writes reports whether carrying out the plan changes any file.
func (p *ModsPlan) writes() bool {
	for _, c := range p.Changes {
		switch c.Kind {
		case ModAdd, ModUpdate, ModReplace, ModRemove, ModSetAside:
			return true
		case ModSkip:
			if c.Disk != "" {
				return true
			}
		}
	}
	return false
}

// modsInput is everything a sync plan looks at.
type modsInput struct {
	archive map[string]pack.Fingerprint                // the server's jars
	files   []string                                   // the jar files in mods/ (*.jar, *.jar.disabled)
	fp      func(name string) (pack.Fingerprint, bool) // content of a file in mods/
	// pack holds GTNH's jars in mods/ by lower-cased name; nil when they aren't known.
	pack    map[string]string
	managed map[string]*pack.Fingerprint // managed jar -> recorded content (nil: not recorded)
	// serverIDs and diskIDs read the mod ids of a server jar and of a jar in mods/; nil
	// means no same-mod checks (a preview, which can't read the new pack's jars yet).
	serverIDs, diskIDs func(name string) []string
	checkAll           bool // check every server jar for the same mod, not just new content
	ignored            []string
}

// planMods decides what a sync does; see the rules at the top of the file.
func planMods(in modsInput) *ModsPlan {
	p := &ModsPlan{Managed: map[string]pack.Fingerprint{}, Ignored: in.ignored}
	d := newDiskNames(in.files)
	content := func(name string) (pack.Fingerprint, bool) {
		if name == "" {
			return pack.Fingerprint{}, false
		}
		return in.fp(name)
	}
	// ours reports whether the file disk is what the sync put there for the jar name.
	ours := func(name, disk string) bool {
		cur, ok := content(disk)
		if !ok {
			return false
		}
		want, inArchive := in.archive[name]
		rec := in.managed[name]
		return rec == nil || cur == *rec || (inArchive && cur == want)
	}
	add := func(c ModChange) { p.Changes = append(p.Changes, c) }

	archived := map[string]bool{}
	for n := range in.archive {
		archived[strings.ToLower(n)] = true
	}
	var idx *jarIndex
	index := func() *jarIndex { // the enabled jars the sync doesn't manage, read on first need
		if idx == nil {
			var names []string
			for _, f := range in.files {
				if _, managed := in.managed[f]; !strings.HasSuffix(f, ".disabled") && !archived[strings.ToLower(f)] && !managed {
					names = append(names, f)
				}
			}
			idx = newJarIndex(names, in.diskIDs)
		}
		return idx
	}
	handled := map[string]bool{} // managed jars a ModSkip already dealt with

	for _, n := range sortedKeys(in.archive) {
		want := in.archive[n]
		rec, managed := in.managed[n]
		if g, ok := in.pack[strings.ToLower(n)]; ok {
			add(ModChange{Kind: ModSkip, Name: n, With: g}) // the file of that name is GTNH's
			handled[n] = true
			continue
		}
		target := ""
		if t, ok := d.find(n + ".disabled"); ok && managed {
			target = t
		} else if t, ok := d.find(n); ok {
			target = t
		}
		cur, onDisk := content(target)
		if in.serverIDs != nil && in.diskIDs != nil && (!onDisk || cur != want || in.checkAll) {
			skipWith, asides := "", []string(nil)
			for _, h := range index().same(n, in.serverIDs(n)) {
				if _, isPack := in.pack[strings.ToLower(h)]; isPack || in.pack == nil {
					skipWith = h
					break
				}
				asides = append(asides, h)
			}
			if skipWith != "" {
				ch := ModChange{Kind: ModSkip, Name: n, With: skipWith}
				if managed && target != "" {
					if ours(n, target) {
						ch.Disk = target
					} else {
						add(ModChange{Kind: ModKeep, Name: n, Disk: target})
					}
				}
				add(ch)
				handled[n] = true
				continue
			}
			for _, h := range asides {
				add(ModChange{Kind: ModSetAside, Name: h, Disk: h, With: n, Preserve: true})
			}
		}
		switch {
		case !onDisk:
			add(ModChange{Kind: ModAdd, Name: n, Disk: n})
		case cur == want:
			if !managed {
				add(ModChange{Kind: ModAdopt, Name: n, Disk: target})
			}
		case managed:
			add(ModChange{Kind: ModUpdate, Name: n, Disk: target, Preserve: rec != nil && cur != *rec})
		case in.pack == nil:
			add(ModChange{Kind: ModSkip, Name: n, With: target}) // can't tell whose it is: leave it
			continue
		default:
			add(ModChange{Kind: ModReplace, Name: n, Disk: target, Preserve: true})
		}
		p.Managed[n] = want
		// An enabled copy next to our disabled one: an older launcher re-enabled it.
		if strings.HasSuffix(target, ".disabled") {
			if t, ok := d.find(n); ok && ours(n, t) {
				add(ModChange{Kind: ModRemove, Name: t, Disk: t})
			}
		}
	}

	for _, m := range sortedKeys(in.managed) {
		if _, still := p.Managed[m]; still || handled[m] {
			continue
		}
		if _, isPack := in.pack[strings.ToLower(m)]; isPack {
			continue // GTNH ships a jar of that name now: the file is GTNH's
		}
		for _, name := range []string{m, m + ".disabled"} {
			t, ok := d.find(name)
			switch {
			case !ok:
			case ours(m, t):
				add(ModChange{Kind: ModRemove, Name: m, Disk: t})
			default:
				add(ModChange{Kind: ModKeep, Name: m, Disk: t})
			}
		}
	}
	return p
}

// diskNames looks names up in mods/ the way Windows and macOS do: the exact name, else
// the only file whose name differs just in case.
type diskNames struct {
	exact map[string]bool
	fold  map[string][]string
}

func newDiskNames(files []string) diskNames {
	d := diskNames{exact: map[string]bool{}, fold: map[string][]string{}}
	for _, f := range files {
		d.exact[f] = true
		d.fold[strings.ToLower(f)] = append(d.fold[strings.ToLower(f)], f)
	}
	return d
}

func (d diskNames) find(name string) (string, bool) {
	if d.exact[name] {
		return name, true
	}
	if c := d.fold[strings.ToLower(name)]; len(c) == 1 {
		return c[0], true
	}
	return "", false
}

// keepMods is the plan of a sync that can't see the server's archive: nothing changes,
// and the managed jars stay managed (recorded as their files are now where nothing was
// recorded yet).
func keepMods(inst prism.Instance, st *State) *ModsPlan {
	p := &ModsPlan{Managed: map[string]pack.Fingerprint{}}
	modsDir := filepath.Join(inst.GameDir, "mods")
	for n, rec := range managedOf(st) {
		if rec != nil {
			p.Managed[n] = *rec
			continue
		}
		for _, name := range []string{n, n + ".disabled"} {
			if fp, err := pack.FingerprintFile(filepath.Join(modsDir, name)); err == nil {
				p.Managed[n] = fp
				break
			}
		}
	}
	return p
}

// afterPack makes in see mods/ as it will be once the pack plan pl ran: the jars it
// installs with their content in next, the ones it removes gone.
func (in *modsInput) afterPack(pl *Plan, next map[string]pack.Fingerprint) {
	over := map[string]*pack.Fingerprint{}
	for _, a := range pl.Actions {
		name, ok := strings.CutPrefix(a.Path, pack.GameDir+"mods/")
		if !ok || strings.Contains(name, "/") {
			continue
		}
		switch a.Kind {
		case Install:
			fp := next[strings.TrimSuffix(a.Path, ".disabled")]
			over[name] = &fp
		case Remove:
			over[name] = nil
		}
	}
	files, seen := []string{}, map[string]bool{}
	for _, f := range in.files {
		if v, ok := over[f]; !ok || v != nil {
			files, seen[f] = append(files, f), true
		}
	}
	for f, v := range over {
		if v != nil && !seen[f] {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	fp := in.fp
	in.files = files
	in.fp = func(name string) (pack.Fingerprint, bool) {
		if v, ok := over[name]; ok {
			if v == nil {
				return pack.Fingerprint{}, false
			}
			return *v, true
		}
		return fp(name)
	}
	in.pack = packJars(next, files)
}
