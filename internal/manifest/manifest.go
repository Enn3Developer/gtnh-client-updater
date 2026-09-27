// Package manifest reads the official GTNH release manifest
// (https://downloads.gtnewhorizons.com/versions.json). The manifest is the allowlist:
// only versions listed there can be installed, and download URLs are taken from it and
// pinned to the official host, never supplied by the caller.
package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	URL          = "https://downloads.gtnewhorizons.com/versions.json"
	DownloadHost = "downloads.gtnewhorizons.com"
)

// Flavor is the Java variant of a Prism/MultiMC pack.
type Flavor int

const (
	Java17 Flavor = iota // lwjgl3ify pack for Java 17+ (key java17_2XUrl)
	Java8                // legacy pack for Java 8 (key java8Url)
)

func (f Flavor) String() string {
	if f == Java8 {
		return "Java 8"
	}
	return "Java 17+"
}

// Release is one manifest entry that ships a Prism/MultiMC pack.
type Release struct {
	Version     string // manifest key, e.g. "2.9.0-RC-1"
	Title       string // e.g. "Stable release", "Beta release"
	ReleaseDate time.Time
	Java8URL    string
	Java17URL   string
}

// Stable reports whether the manifest marks this release as stable.
func (r Release) Stable() bool { return strings.Contains(strings.ToLower(r.Title), "stable") }

// URL returns the validated pack URL for the flavor, or an error if the release has no
// such pack.
func (r Release) URL(f Flavor) (string, error) {
	u := r.Java17URL
	if f == Java8 {
		u = r.Java8URL
	}
	if u == "" {
		return "", fmt.Errorf("version %s has no %s Prism pack", r.Version, f)
	}
	if err := CheckURL(u); err != nil {
		return "", err
	}
	return u, nil
}

// CheckURL pins a pack URL to HTTPS on the official download host.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad download URL %q: %w", raw, err)
	}
	if u.Scheme != "https" || u.Host != DownloadHost {
		return fmt.Errorf("refusing download from unexpected location: %s", raw)
	}
	return nil
}

// Manifest is the parsed release list, newest first.
type Manifest struct {
	Releases []Release
}

// Find returns the release with the exact version key.
func (m *Manifest) Find(version string) (Release, bool) {
	for _, r := range m.Releases {
		if r.Version == version {
			return r, true
		}
	}
	return Release{}, false
}

type rawRelease struct {
	Title       string `json:"title"`
	ReleaseDate string `json:"releaseDate"`
	MMC         *struct {
		Java8URL  string `json:"java8Url"`
		Java17URL string `json:"java17_2XUrl"`
	} `json:"mmc"`
}

// Parse decodes the manifest. It accepts both the public top-level map and a
// {"versions": {...}} wrapper, like the server updater does.
func Parse(data []byte) (*Manifest, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("could not parse manifest: expected release map: %w", err)
	}
	if inner, ok := top["versions"]; ok {
		top = nil
		if err := json.Unmarshal(inner, &top); err != nil {
			return nil, fmt.Errorf("could not parse manifest: expected release map: %w", err)
		}
	}
	m := &Manifest{}
	for key, raw := range top {
		var rr rawRelease
		if err := json.Unmarshal(raw, &rr); err != nil {
			return nil, fmt.Errorf("could not parse manifest entry %q: expected release object: %w", key, err)
		}
		if rr.MMC == nil || (rr.MMC.Java8URL == "" && rr.MMC.Java17URL == "") {
			continue // no Prism pack for this entry
		}
		date, _ := time.Parse("2006/01/02", rr.ReleaseDate) // zero time sorts last
		m.Releases = append(m.Releases, Release{
			Version:     key,
			Title:       rr.Title,
			ReleaseDate: date,
			Java8URL:    rr.MMC.Java8URL,
			Java17URL:   rr.MMC.Java17URL,
		})
	}
	if len(m.Releases) == 0 {
		return nil, fmt.Errorf("manifest lists no Prism packs")
	}
	// Newest first. Dates break most ties, but the manifest has copy-paste mistakes (it
	// dates 2.9.0-RC-1 the same day as beta-3), so equal dates fall back to comparing
	// the versions themselves.
	sort.SliceStable(m.Releases, func(i, j int) bool {
		a, b := m.Releases[i], m.Releases[j]
		if !a.ReleaseDate.Equal(b.ReleaseDate) {
			return a.ReleaseDate.After(b.ReleaseDate)
		}
		return CompareVersions(a.Version, b.Version) > 0
	})
	return m, nil
}

var versionRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:[-.]?([A-Za-z]+)[-.]?(\d*))?$`)

// preRank orders pre-release tags; a final release ranks above all of them.
var preRank = map[string]int{"alpha": 1, "beta": 2, "pre": 3, "rc": 4}

const finalRank = 10

// CompareVersions orders GTNH version keys: numeric parts first, then pre-release tag
// (beta < pre < rc < final) and its number. Keys that are not versions ("April fools
// 2025") compare as plain strings, below real versions.
func CompareVersions(a, b string) int {
	ma, mb := versionRe.FindStringSubmatch(a), versionRe.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return strings.Compare(a, b)
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	na, nb := strings.Split(ma[1], "."), strings.Split(mb[1], ".")
	for i := 0; i < max(len(na), len(nb)); i++ {
		if c := cmpInt(atoi(na, i), atoi(nb, i)); c != 0 {
			return c
		}
	}
	if c := cmpInt(rank(ma[2]), rank(mb[2])); c != 0 {
		return c
	}
	return cmpInt(atoi([]string{ma[3]}, 0), atoi([]string{mb[3]}, 0))
}

func rank(tag string) int {
	if tag == "" {
		return finalRank
	}
	return preRank[strings.ToLower(tag)] // unknown tags (daily, experimental) rank lowest
}

func atoi(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Fetch downloads and parses the official manifest.
func Fetch(client *http.Client) (*Manifest, error) {
	resp, err := client.Get(URL)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot fetch manifest: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("cannot fetch manifest: %w", err)
	}
	return Parse(data)
}
