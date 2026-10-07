package update

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// Two jars are the same mod when their mcmod.info files name the same mod id; Forge
// refuses to start with both in mods/. A jar without a usable mcmod.info is compared by
// its file name without the version instead ("NotEnoughItems-2.6.0-GTNH.jar" ->
// "notenoughitems").

// maxJarRead bounds how much of a server jar is read into memory to find its mcmod.info.
const maxJarRead = 256 << 20

// usableID reports whether a mod id names a real mod, not what example code and
// unexpanded build templates leave behind.
func usableID(id string) bool {
	return id != "" && id != "examplemod" && !strings.Contains(id, "${")
}

// modInfoIDs reads the mod ids of an mcmod.info: a list of mods, or {"modList": [...]}.
func modInfoIDs(data []byte) []string {
	type mod struct {
		ModID string `json:"modid"`
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var list []mod
	if json.Unmarshal(data, &list) != nil {
		var v2 struct {
			ModList []mod `json:"modList"`
		}
		if json.Unmarshal(data, &v2) != nil {
			return nil
		}
		list = v2.ModList
	}
	var out []string
	for _, m := range list {
		if id := strings.ToLower(strings.TrimSpace(m.ModID)); usableID(id) {
			out = append(out, id)
		}
	}
	return out
}

// zipModIDs reads the mod ids of an opened jar.
func zipModIDs(r *zip.Reader) []string {
	for _, f := range r.File {
		if f.Name != "mcmod.info" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil
		}
		return modInfoIDs(data)
	}
	return nil
}

// fileModIDs reads the mod ids of the jar file at p (nil when it has none or isn't a jar).
func fileModIDs(p string) []string {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil
	}
	defer zr.Close()
	return zipModIDs(&zr.Reader)
}

// archiveModIDs reads the mod ids of a jar inside the server's archive.
func archiveModIDs(zf *zip.File) []string {
	if zf.UncompressedSize64 > maxJarRead {
		return nil
	}
	rc, err := zf.Open()
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	return zipModIDs(zr)
}

// modNameKey is a jar's name up to its version, letters and digits only, lower case;
// "" when nothing is left.
func modNameKey(name string) string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".jar"))
	var b strings.Builder
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c == '-' || c == '_' || c == ' ' || c == '+') && versionAt(n[i+1:]) {
			break
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// versionAt reports whether s starts with a version: a digit, maybe after "v" or "mc".
func versionAt(s string) bool {
	for _, p := range []string{"mc", "v"} {
		if t, ok := strings.CutPrefix(s, p); ok {
			s = t
			break
		}
	}
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// jarIndex finds the jars of mods/ that are the same mod as another jar.
type jarIndex struct {
	byID   map[string][]string // mod id -> jar names
	byName map[string][]string // name key -> jar names
	hasIDs map[string]bool     // jar name -> its mcmod.info names a mod
}

// newJarIndex indexes the jars names, reading their mod ids with ids.
func newJarIndex(names []string, ids func(name string) []string) *jarIndex {
	x := &jarIndex{byID: map[string][]string{}, byName: map[string][]string{}, hasIDs: map[string]bool{}}
	for _, n := range names {
		got := ids(n)
		for _, id := range got {
			x.byID[id] = append(x.byID[id], n)
		}
		x.hasIDs[n] = len(got) > 0
		if k := modNameKey(n); k != "" {
			x.byName[k] = append(x.byName[k], n)
		}
	}
	return x
}

// same lists, sorted, the indexed jars that are the same mod as the jar name with mod
// ids ids: those sharing a mod id and, where either side has no mod id, those with the
// same name key.
func (x *jarIndex) same(name string, ids []string) []string {
	seen := map[string]bool{}
	for _, id := range ids {
		for _, n := range x.byID[id] {
			seen[n] = true
		}
	}
	if k := modNameKey(name); k != "" {
		for _, n := range x.byName[k] {
			if len(ids) == 0 || !x.hasIDs[n] {
				seen[n] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
