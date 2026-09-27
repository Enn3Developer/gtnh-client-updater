package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// CheckCustomModsURL requires HTTPS for the custom-mods archive.
func CheckCustomModsURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("the server mods link must start with https:// (got %q)", raw)
	}
	return nil
}

// CustomModsResult reports what the sync did.
type CustomModsResult struct {
	Installed []string // jars now managed in mods/
	Added     []string // newly written or updated this run
	Removed   []string // previously managed jars that are gone from the archive
	Skipped   []string // names that clash with a pack mod; the pack's file wins
}

// CustomModsArchive is a fetched server extra-mods archive: its jars by file name.
type CustomModsArchive struct {
	Jars map[string]*zip.File
}

// Names returns the jar names in the archive.
func (a *CustomModsArchive) Names() []string {
	out := make([]string, 0, len(a.Jars))
	for name := range a.Jars {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SyncCustomMods mirrors the archive into mods/. Only jars it installed itself (tracked
// in managed) are ever removed, so the player's own jars are safe. Replaced and removed
// jars are moved into backupDir.
func SyncCustomMods(a *CustomModsArchive, inst prism.Instance, managed []string,
	packMods map[string]pack.Fingerprint, backupDir string) (*CustomModsResult, error) {
	res := &CustomModsResult{}
	modsDir := filepath.Join(inst.GameDir, "mods")
	j := &journal{backupDir: filepath.Join(backupDir, "custom-mods"), root: modsDir}

	for _, name := range managed {
		if _, still := a.Jars[name]; still {
			continue
		}
		if err := j.stash(filepath.Join(modsDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove extra mod %s: %w", name, err)
		}
		res.Removed = append(res.Removed, name)
	}
	for _, name := range a.Names() {
		zf := a.Jars[name]
		if _, clash := packMods[path.Join(pack.GameDir+"mods", name)]; clash {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		disk := filepath.Join(modsDir, name)
		if cur, err := pack.FingerprintFile(disk); err == nil &&
			cur == (pack.Fingerprint{Size: zf.UncompressedSize64, CRC: zf.CRC32}) {
			res.Installed = append(res.Installed, name)
			continue
		}
		if err := j.install(disk, zf.Open); err != nil {
			return nil, fmt.Errorf("install extra mod %s: %w", name, err)
		}
		res.Installed = append(res.Installed, name)
		res.Added = append(res.Added, name)
	}
	return res, nil
}

// RemoveCustomMods takes out every jar the sync installed (moved into backupDir).
func RemoveCustomMods(inst prism.Instance, managed []string, backupDir string) (*CustomModsResult, error) {
	modsDir := filepath.Join(inst.GameDir, "mods")
	j := &journal{backupDir: filepath.Join(backupDir, "custom-mods"), root: modsDir}
	res := &CustomModsResult{}
	for _, name := range managed {
		if err := j.stash(filepath.Join(modsDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove extra mod %s: %w", name, err)
		}
		res.Removed = append(res.Removed, name)
	}
	return res, nil
}

// FetchCustomMods downloads a server extra-mods archive (kept in memory; these are a
// handful of jars). A 404 means the server has none right now: an empty archive.
func FetchCustomMods(client *http.Client, archiveURL string) (*CustomModsArchive, error) {
	resp, err := client.Get(archiveURL)
	if err != nil {
		return nil, fmt.Errorf("couldn't download your server's extra mods: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &CustomModsArchive{Jars: map[string]*zip.File{}}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("couldn't download your server's extra mods: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, fmt.Errorf("couldn't download your server's extra mods: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("your server's extra mods link doesn't point to a zip file: %w", err)
	}
	a := &CustomModsArchive{Jars: map[string]*zip.File{}}
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if zf.FileInfo().IsDir() || strings.ContainsAny(name, "/:") || !strings.HasSuffix(name, ".jar") {
			continue // the archive is flat; ignore anything nested, odd or not a jar
		}
		a.Jars[name] = zf
	}
	return a, nil
}
