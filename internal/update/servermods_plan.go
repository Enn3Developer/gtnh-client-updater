package update

import (
	"path/filepath"
	"slices"
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
//   - The server's overrides (jars in its archive's overrides folder) win over GTNH's: GTNH's
//     jars of the same mod are switched off in Prism (foo.jar.disabled) instead of the
//     server's jar being left out, and switched back on once the server stops overriding
//     them. They win over the player's override of that mod too (it goes to the
//     replaced-mods folder). An override GTNH ships a jar of the same name as is left out,
//     and without a known pack an override is just a server jar.

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
	ModDisable                 // GTNH's jar the server overrides: switched off (Disk -> Disk.disabled)
	ModEnable                  // GTNH's jar the server doesn't override any more: switched back on
)

// ModChange is one step of a sync. Name is the server's jar (for ModSetAside the
// player's jar, for ModDisable and ModEnable GTNH's), Disk the file in mods/ it writes,
// moves or keeps ("" = none), With the jar behind a ModSkip, a ModSetAside or a
// ModDisable. Preserve marks a Disk that is the player's: it
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
	// Overrides are the server's jars that stand in for GTNH's afterwards, with GTNH's
	// jars they keep switched off.
	Overrides []Override
	// Dropped are the player's overrides the server's override of the same mod takes
	// over: their records go.
	Dropped []string
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
		case ModAdd, ModUpdate, ModReplace, ModRemove, ModSetAside, ModDisable, ModEnable:
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
	own                []string // the player's override jars: they win like GTNH's own
	// over are the server's jars that stand in for GTNH's (its overrides folder);
	// serverOv the server's overrides as the last sync left them, ownOv the player's.
	over            map[string]bool
	serverOv, ownOv []Override
	// ovServerIDs and ovDiskIDs read mod ids for the overrides when serverIDs and diskIDs
	// are nil (a preview): only the server's overrides and GTNH's jars beside them are read.
	ovServerIDs, ovDiskIDs func(name string) []string
}

// packIDs makes in.ovDiskIDs read a jar that isn't in mods/ yet from the pack p.
func (in *modsInput) packIDs(p *pack.Pack) {
	disk := in.ovDiskIDs
	in.ovDiskIDs = memoIDs(func(n string) []string {
		if disk != nil {
			if ids := disk(n); ids != nil {
				return ids
			}
		}
		if e, ok := p.Entries[pack.GameDir+"mods/"+strings.TrimSuffix(n, ".disabled")]; ok {
			return nestedModIDs(e.FP.Size, e.Open)
		}
		return nil
	})
}

// withOwn adds the override jars to pk, GTNH's jars by lower-cased name (nil: unknown).
func (in *modsInput) withOwn(pk map[string]string) map[string]string {
	if pk == nil {
		return nil
	}
	for _, j := range in.own {
		pk[strings.ToLower(j)] = j
	}
	return pk
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
	handled := map[string]bool{} // managed jars a ModSkip already dealt with
	overrides, enabled := planServerOverrides(&in, p, d, handled)
	if len(enabled) > 0 {
		in.checkAll = true // a server jar may be of the mod of a jar switched back on
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
			idx = newJarIndex(append(names, enabled...), in.diskIDs)
		}
		return idx
	}

	for _, n := range sortedKeys(in.archive) {
		want := in.archive[n]
		rec, managed := in.managed[n]
		over := overrides[n]
		if handled[n] {
			continue
		}
		if g, ok := in.pack[strings.ToLower(n)]; ok && !over {
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
				_, isPack := in.pack[strings.ToLower(h)]
				if over && isPack {
					continue // GTNH's: planServerOverrides switches it off
				}
				if isPack || in.pack == nil {
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
	if st != nil {
		p.Overrides = slices.Clone(st.ServerOverrides)
	}
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
	in.pack = in.withOwn(packJars(next, files))
}

// planServerOverrides plans the server's overrides (in.over) into p: GTNH's jars of each
// one's mod are switched off and the player's override of it set aside, and GTNH's jars a
// former override kept off are switched back on. It returns the archive jars that stand
// in for GTNH's, and the jars switched back on. An override GTNH ships a jar of the same
// name as is left out (handled); without a known pack there are none.
func planServerOverrides(in *modsInput, p *ModsPlan, d diskNames, handled map[string]bool) (map[string]bool, []string) {
	over := map[string]bool{}
	if in.pack == nil && len(in.serverOv) == 0 {
		return over, nil
	}
	add := func(c ModChange) { p.Changes = append(p.Changes, c) }
	ownOf := map[string]Override{} // the player's overrides by lower-cased jar
	for _, o := range in.ownOv {
		ownOf[strings.ToLower(o.Jar)] = o
	}
	dropped := map[string]bool{}
	// ownCovers reports whether a player's override that stays stands in for GTNH's r.
	ownCovers := func(r string) bool {
		for _, o := range in.ownOv {
			if !dropped[o.Jar] && (strings.EqualFold(o.Jar, r) || slices.ContainsFunc(o.Replaces, func(x string) bool { return strings.EqualFold(x, r) })) {
				return true
			}
		}
		return false
	}
	covered := map[string]bool{} // lower-cased GTNH jars an override keeps off afterwards
	if in.pack != nil {
		// GTNH's jars and the player's override jars in mods/, enabled or not, by their
		// enabled name; file is the file of each (the enabled one when both are there).
		names, file := []string{}, map[string]string{}
		for _, f := range in.files {
			on := strings.TrimSuffix(f, ".disabled")
			if _, ok := in.pack[strings.ToLower(on)]; !ok {
				continue
			}
			if _, ok := file[on]; !ok {
				names = append(names, on)
			}
			if cur, ok := file[on]; !ok || strings.HasSuffix(cur, ".disabled") {
				file[on] = f
			}
		}
		var idx *jarIndex
		off := map[string]bool{} // GTNH's jars switched off already
		for _, n := range sortedKeys(in.archive) {
			if !in.over[n] {
				continue
			}
			if g, ok := in.pack[strings.ToLower(n)]; ok {
				if _, own := ownOf[strings.ToLower(g)]; !own {
					add(ModChange{Kind: ModSkip, Name: n, With: g}) // it can't stand beside GTNH's jar of that name
					handled[n] = true
					continue
				}
			}
			over[n] = true
			var reps []string
			takeOwn := func(o Override) {
				if !dropped[o.Jar] {
					dropped[o.Jar] = true
					p.Dropped = append(p.Dropped, o.Jar)
				}
				reps = append(reps, o.Replaces...)
			}
			if o, ok := ownOf[strings.ToLower(n)]; ok {
				takeOwn(o) // the player's jar of that name makes way in the main plan
			}
			if idx == nil {
				diskIDs := orIDs(in.diskIDs, in.ovDiskIDs)
				idx = newJarIndex(names, func(h string) []string {
					if diskIDs == nil {
						return nil
					}
					return diskIDs(file[h])
				})
			}
			var ids []string
			if serverIDs := orIDs(in.serverIDs, in.ovServerIDs); serverIDs != nil {
				ids = serverIDs(n)
			}
			for _, h := range idx.same(n, ids) {
				f := file[h]
				switch o, own := ownOf[strings.ToLower(h)]; {
				case strings.EqualFold(h, n):
				case own:
					add(ModChange{Kind: ModSetAside, Name: f, Disk: f, With: n, Preserve: true})
					takeOwn(o)
				case f != h && ownCovers(h): // off for another of the player's overrides
				default:
					if f == h && !off[h] {
						add(ModChange{Kind: ModDisable, Name: h, Disk: h, With: n})
						off[h] = true
					}
					reps = append(reps, h)
				}
			}
			for _, o := range in.serverOv {
				if o.Jar != n {
					continue
				}
				for _, r := range o.Replaces {
					if _, ok := d.find(r + ".disabled"); ok {
						reps = append(reps, r)
					}
				}
			}
			slices.Sort(reps)
			if reps = slices.Compact(reps); len(reps) > 0 {
				p.Overrides = append(p.Overrides, Override{Jar: n, Replaces: reps})
			}
			for _, r := range reps {
				covered[strings.ToLower(r)] = true
			}
		}
	}
	var enabled []string
	for _, o := range in.serverOv {
		for _, r := range o.Replaces {
			if covered[strings.ToLower(r)] || ownCovers(r) {
				continue
			}
			covered[strings.ToLower(r)] = true
			if _, ok := d.find(r); ok {
				continue // something has its enabled name
			}
			if f, ok := d.find(r + ".disabled"); ok {
				add(ModChange{Kind: ModEnable, Name: r, Disk: f})
				enabled = append(enabled, r)
			}
		}
	}
	return over, enabled
}

// orIDs is a, or b when a is nil.
func orIDs(a, b func(string) []string) func(string) []string {
	if a != nil {
		return a
	}
	return b
}
