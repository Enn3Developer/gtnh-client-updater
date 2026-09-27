package update

import (
	"os"
	"regexp"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// Detection is a guess of the installed pack version and where it came from.
type Detection struct {
	Version string
	Source  string // "updater state", "pack changelog", "instance name"; "" if unknown
}

var changelogRe = regexp.MustCompile(`^changelog from .+ to (.+)\.md$`)

// DetectVersion guesses which manifest version an instance runs, most reliable first:
// the updater's own state, the "changelog from X to Y.md" file every pack ships in its
// game dir, then a manifest key embedded in the instance name or directory name.
func DetectVersion(inst prism.Instance, st *State, m *manifest.Manifest) Detection {
	if st != nil && st.Version != "" {
		return Detection{st.Version, "updater state"}
	}
	if v := fromChangelog(inst.GameDir, m); v != "" {
		return Detection{v, "pack changelog"}
	}
	if v := fromName(inst, m); v != "" {
		return Detection{v, "instance name"}
	}
	return Detection{}
}

func fromChangelog(gameDir string, m *manifest.Manifest) string {
	ents, err := os.ReadDir(gameDir)
	if err != nil {
		return ""
	}
	best, bestIdx := "", len(m.Releases)
	for _, e := range ents {
		sub := changelogRe.FindStringSubmatch(e.Name())
		if sub == nil {
			continue
		}
		// Manual upgrades can leave several changelogs; the newest release wins
		// (Releases is sorted newest first).
		for i, r := range m.Releases {
			if strings.EqualFold(r.Version, sub[1]) && i < bestIdx {
				best, bestIdx = r.Version, i
			}
		}
	}
	return best
}

func fromName(inst prism.Instance, m *manifest.Manifest) string {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", " ")) }
	hay := norm(inst.Name) + "\x00" + norm(baseName(inst.Dir))
	best := ""
	for _, r := range m.Releases {
		key := norm(r.Version)
		if !containsToken(hay, key) {
			continue
		}
		if len(r.Version) > len(best) { // "2.8.4-pre1" beats "2.8.4"
			best = r.Version
		}
	}
	return best
}

// containsToken reports whether key occurs in hay not glued to more version characters,
// so "2.8.1" does not match inside "2.8.10".
func containsToken(hay, key string) bool {
	for i := 0; ; {
		j := strings.Index(hay[i:], key)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(key)
		before := start == 0 || !isVerChar(hay[start-1])
		after := end == len(hay) || !isVerChar(hay[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isVerChar(c byte) bool { return c >= '0' && c <= '9' || c == '.' }

func baseName(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
