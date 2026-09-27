// Package pack reads GTNH Prism/MultiMC pack archives.
//
// A pack zip wraps everything in one directory ("GT New Horizons 2.9.0-RC-1/") holding
// instance.cfg, mmc-pack.json, patches/, libraries/ and the game dir .minecraft/. Entries
// are exposed by their path relative to that root, in canonical form: the game dir is
// always spelled ".minecraft/" (older packs used "minecraft/"). Each entry carries a
// Fingerprint (size + CRC-32 from the zip central directory), which is what the updater
// compares against files on disk, so a pack can be fingerprinted from its directory alone.
package pack

import (
	"archive/zip"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// GameDir is the canonical game-dir prefix of pack paths.
const GameDir = ".minecraft/"

// Fingerprint identifies file content well enough to tell "unchanged" from "changed".
// It is not a security hash: integrity of downloads comes from HTTPS to the official
// host plus the zip CRC check.
type Fingerprint struct {
	Size uint64 `json:"size"`
	CRC  uint32 `json:"crc"`
}

// FingerprintFile computes the fingerprint of a file on disk.
func FingerprintFile(name string) (Fingerprint, error) {
	f, err := os.Open(name)
	if err != nil {
		return Fingerprint{}, err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	n, err := io.Copy(h, f)
	if err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{Size: uint64(n), CRC: h.Sum32()}, nil
}

// Entry is one regular file in the pack.
type Entry struct {
	Path string // canonical path relative to the pack root
	FP   Fingerprint
	file *zip.File
}

// Open returns a reader for the entry's content. Reading it to EOF verifies its CRC.
func (e *Entry) Open() (io.ReadCloser, error) { return e.file.Open() }

// Pack is an opened pack archive.
type Pack struct {
	Entries map[string]*Entry
	closer  io.Closer
}

// Close releases the underlying archive.
func (p *Pack) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

// Paths returns all entry paths, sorted.
func (p *Pack) Paths() []string {
	out := make([]string, 0, len(p.Entries))
	for k := range p.Entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Fingerprints returns path -> fingerprint for every entry.
func (p *Pack) Fingerprints() map[string]Fingerprint {
	out := make(map[string]Fingerprint, len(p.Entries))
	for k, e := range p.Entries {
		out[k] = e.FP
	}
	return out
}

// OpenFile opens a pack zip on disk.
func OpenFile(name string) (*Pack, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive: %w", err)
	}
	p, err := fromZip(&zr.Reader)
	if err != nil {
		zr.Close()
		return nil, err
	}
	p.closer = zr
	return p, nil
}

// OpenReaderAt opens a pack from any random-access source (e.g. a remote HTTP file).
func OpenReaderAt(r io.ReaderAt, size int64) (*Pack, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive: %w", err)
	}
	return fromZip(zr)
}

func fromZip(zr *zip.Reader) (*Pack, error) {
	root, err := findRoot(zr.File)
	if err != nil {
		return nil, err
	}
	p := &Pack{Entries: map[string]*Entry{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, root) {
			continue
		}
		rel, err := Clean(strings.TrimPrefix(f.Name, root))
		if err != nil {
			return nil, err
		}
		if rel == "" {
			continue
		}
		p.Entries[rel] = &Entry{
			Path: rel,
			FP:   Fingerprint{Size: f.UncompressedSize64, CRC: f.CRC32},
			file: f,
		}
	}
	if !p.hasGameDir() {
		return nil, errors.New("archive does not look like a GTNH Prism pack (no .minecraft/mods)")
	}
	return p, nil
}

func (p *Pack) hasGameDir() bool {
	for k := range p.Entries {
		if strings.HasPrefix(k, GameDir+"mods/") {
			return true
		}
	}
	return false
}

// findRoot returns the zip-internal prefix of the pack root: the shallowest directory
// holding mmc-pack.json.
func findRoot(files []*zip.File) (string, error) {
	best := ""
	found := false
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if path.Base(name) != "mmc-pack.json" {
			continue
		}
		dir := strings.TrimSuffix(name, "mmc-pack.json")
		if !found || strings.Count(dir, "/") < strings.Count(best, "/") {
			best, found = dir, true
		}
	}
	if !found {
		return "", errors.New("archive does not look like a GTNH Prism pack (no mmc-pack.json)")
	}
	return best, nil
}

// Clean validates a pack-relative path and canonicalizes the game dir. It rejects
// absolute paths and any ".." component (zip-slip).
func Clean(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return "", fmt.Errorf("unsafe path in archive: %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe path in archive: %q", p)
		}
	}
	p = path.Clean(p)
	if p == "." {
		return "", nil
	}
	if p == "minecraft" || strings.HasPrefix(p, "minecraft/") {
		p = "." + p
	}
	return p, nil
}

// Verify reads every entry to EOF so the zip reader checks each CRC. progress, if not
// nil, is called with the number of entries verified so far.
func (p *Pack) Verify(progress func(done, total int)) error {
	paths := p.Paths()
	for i, k := range paths {
		rc, err := p.Entries[k].Open()
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		_, err = io.Copy(io.Discard, rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("pack failed integrity check at %s: %w", k, err)
		}
		if progress != nil {
			progress(i+1, len(paths))
		}
	}
	return nil
}
