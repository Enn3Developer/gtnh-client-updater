package update

import (
	"archive/zip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

// The server's extra-mods archive is a zip of jars at its top level (or inside a single
// folder, which is what "compress folder" makes). It is downloaded into the instance's
// state dir and kept there, so the next sync can ask the server whether it changed
// (ETag / Last-Modified) and work from that copy when it didn't, or when the server
// can't be reached.

const (
	modsZip  = "server-mods.zip"  // the archive last downloaded, in StateDir
	modsInfo = "server-mods.json" // where it came from, for a conditional download
)

// Limits of a server-mods archive (variables for the tests).
var (
	maxModsDownload int64  = 1 << 30 // bytes of the archive
	maxModsUnpacked uint64 = 2 << 30 // bytes of all its jars together
)

// Why the server's mods couldn't be fetched, worded to end a sentence for the player:
// "I couldn't check your server's mods: <reason>."
var (
	ErrModsNotFound  = errors.New("the link doesn't lead to a file anymore")
	ErrModsInsecure  = errors.New("the link sends me to an address that isn't https, so I didn't download it")
	ErrModsTooBig    = errors.New("the file is bigger than 1 GB, so I didn't download it")
	ErrModsUnpacked  = errors.New("the mods in it would take more than 2 GB, so I didn't use it")
	ErrModsWebPage   = errors.New("the link gives a web page, not a zip file — ask the server owner for a direct download link")
	ErrModsNotZip    = errors.New("the link doesn't give a zip file")
	ErrModsInFolders = errors.New("the mods in the zip are inside folders — they have to be at the top of the zip")
)

var errInsecureRedirect = errors.New("redirected to an address that isn't https")

// modsErr is a reason for the player that keeps the error behind it for errors.Is.
type modsErr struct {
	reason string
	err    error
}

func (e *modsErr) Error() string { return e.reason }
func (e *modsErr) Unwrap() error { return e.err }

// CheckCustomModsURL requires HTTPS for the custom-mods archive.
func CheckCustomModsURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("the server mods link must start with https:// (got %q)", raw)
	}
	return nil
}

// httpsOnly is the redirect policy of the server-mods download: an https link must not
// lead to plain http, where anyone on the way could swap the jars.
func httpsOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Scheme != "https" {
		return errInsecureRedirect
	}
	return nil
}

// ModsArchive is an opened server-mods archive: its usable jars by file name.
type ModsArchive struct {
	jars    map[string]*zip.File
	ignored []string // entries left out: jars in folders, odd or doubled names
	closer  io.Closer
}

// Names returns the jar names in the archive, sorted.
func (a *ModsArchive) Names() []string {
	if a == nil {
		return nil
	}
	return sortedKeys(a.jars)
}

// Close releases the archive. Safe on nil.
func (a *ModsArchive) Close() error {
	if a == nil || a.closer == nil {
		return nil
	}
	err := a.closer.Close()
	a.closer = nil
	return err
}

// modsCache says where the cached archive came from.
type modsCache struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// fetchMods brings the copy of the archive at link in stateDir up to date and opens it.
// The download is conditional when the copy came from the same link, and a download only
// replaces the copy when it opens as a mods archive.
func fetchMods(ctx context.Context, client *http.Client, link, stateDir string, progress func(done, total int64)) (*ModsArchive, error) {
	if err := CheckCustomModsURL(link); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a, err := downloadMods(ctx, client, link, stateDir, true, progress)
	if errors.Is(err, errStaleCopy) {
		a, err = downloadMods(ctx, client, link, stateDir, false, progress)
	}
	return a, err
}

// errStaleCopy: the server said "not modified", but the copy doesn't open any more.
var errStaleCopy = errors.New("the saved copy is damaged")

func downloadMods(ctx context.Context, client *http.Client, link, stateDir string, conditional bool, progress func(done, total int64)) (*ModsArchive, error) {
	zipPath, infoPath := filepath.Join(stateDir, modsZip), filepath.Join(stateDir, modsInfo)
	h := http.Header{}
	if c, ok := readModsCache(infoPath); conditional && ok && c.URL == link && fileExists(zipPath) {
		if c.ETag != "" {
			h.Set("If-None-Match", c.ETag)
		}
		if c.LastModified != "" {
			h.Set("If-Modified-Since", c.LastModified)
		}
	}
	cl := *client
	cl.CheckRedirect = httpsOnly
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, &modsErr{"I couldn't save the download (" + err.Error() + ")", err}
	}
	part := zipPath + ".part"
	res, err := pack.FetchContext(ctx, &cl, link, part, pack.FetchOptions{Header: h, MaxBytes: maxModsDownload, Progress: progress})
	if err != nil {
		return nil, modsFetchError(err)
	}
	if res.Status == http.StatusNotModified {
		a, err := openModsArchive(zipPath)
		if err != nil {
			os.Remove(infoPath)
			return nil, errStaleCopy
		}
		return a, nil
	}
	a, err := openModsArchive(part)
	if err != nil {
		if errors.Is(err, ErrModsNotZip) && (strings.HasPrefix(res.ContentType, "text/html") || looksLikeHTML(part)) {
			err = ErrModsWebPage
		}
		os.Remove(part)
		return nil, err
	}
	a.Close()
	os.Remove(infoPath) // the copy changes: never leave validators that belong to the old one
	if err := os.Rename(part, zipPath); err != nil {
		os.Remove(part)
		return nil, &modsErr{"I couldn't save the download (" + err.Error() + ")", err}
	}
	writeModsCache(infoPath, modsCache{URL: link, ETag: res.ETag, LastModified: res.LastModified})
	return openModsArchive(zipPath)
}

// cachedMods opens the copy the last download of link left in stateDir, if any.
func cachedMods(link, stateDir string) *ModsArchive {
	c, ok := readModsCache(filepath.Join(stateDir, modsInfo))
	if !ok || c.URL != link {
		return nil
	}
	a, err := openModsArchive(filepath.Join(stateDir, modsZip))
	if err != nil {
		return nil
	}
	return a
}

// dropModsCache deletes the saved copy: the instance has no link any more.
func dropModsCache(stateDir string) {
	os.Remove(filepath.Join(stateDir, modsInfo))
	os.Remove(filepath.Join(stateDir, modsZip))
}

func readModsCache(p string) (modsCache, bool) {
	data, err := os.ReadFile(p)
	if err != nil {
		return modsCache{}, false
	}
	var c modsCache
	if json.Unmarshal(data, &c) != nil {
		return modsCache{}, false
	}
	return c, true
}

// writeModsCache is best effort: without it the next download is just unconditional.
func writeModsCache(p string, c modsCache) {
	if data, err := json.Marshal(c); err == nil {
		os.WriteFile(p, data, 0o644)
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// looksLikeHTML reports whether the file starts like a web page.
func looksLikeHTML(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	return strings.HasPrefix(http.DetectContentType(buf[:n]), "text/html")
}

// modsFetchError turns a failed download into a reason the player can act on.
func modsFetchError(err error) error {
	var he *pack.HTTPError
	var pathErr *fs.PathError
	var urlErr *url.Error
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, errInsecureRedirect):
		return &modsErr{ErrModsInsecure.Error(), errors.Join(ErrModsInsecure, err)}
	case errors.Is(err, pack.ErrTooBig):
		return &modsErr{ErrModsTooBig.Error(), errors.Join(ErrModsTooBig, err)}
	case errors.As(err, &he) && (he.Status == http.StatusNotFound || he.Status == http.StatusGone):
		return &modsErr{ErrModsNotFound.Error(), errors.Join(ErrModsNotFound, err)}
	case he != nil:
		return &modsErr{fmt.Sprintf("the server behind the link answered with error %d", he.Status), err}
	case errors.Is(err, pack.ErrStalled):
		return &modsErr{"the download stopped — check your internet connection", err}
	case badCertificate(err):
		return &modsErr{"the site's security certificate isn't valid, so I didn't download it", err}
	case errors.As(err, &urlErr):
		return &modsErr{"I couldn't reach the server behind the link — check your internet connection", err}
	case errors.As(err, &pathErr):
		return &modsErr{"I couldn't save the download (" + err.Error() + ")", err}
	}
	return &modsErr{"the download broke off — check your internet connection", err}
}

func badCertificate(err error) bool {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &host) || errors.As(err, &invalid)
}

// openModsArchive opens a downloaded archive and picks its usable jars.
func openModsArchive(file string) (*ModsArchive, error) {
	zr, err := zip.OpenReader(file)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, &modsErr{ErrModsNotZip.Error(), errors.Join(ErrModsNotZip, err)}
	}
	a, err := readModsArchive(zr.File)
	if err != nil {
		zr.Close()
		return nil, err
	}
	a.closer = zr
	return a, nil
}

// readModsArchive picks the jars at the top of the archive, or inside the one folder
// that holds everything. Jars deeper down and names that can't be a file on every
// supported OS are left out and listed; anything that isn't a jar is skipped silently.
func readModsArchive(files []*zip.File) (*ModsArchive, error) {
	root := archiveRoot(files)
	a := &ModsArchive{jars: map[string]*zip.File{}}
	taken := map[string]bool{} // lower-cased: Windows and macOS don't tell Foo.jar from foo.jar
	var total uint64
	nested := 0
	for _, zf := range files {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if archiveJunk(name) || zf.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(name, root)
		switch {
		case !strings.HasSuffix(rel, ".jar"):
			continue
		case strings.Contains(rel, "/"):
			nested++
			a.ignored = append(a.ignored, rel)
			continue
		case badModName(rel) || taken[strings.ToLower(rel)]:
			a.ignored = append(a.ignored, rel)
			continue
		}
		if total += zf.UncompressedSize64; total > maxModsUnpacked {
			return nil, ErrModsUnpacked
		}
		taken[strings.ToLower(rel)] = true
		a.jars[rel] = zf
	}
	if len(a.jars) == 0 && nested > 0 {
		return nil, ErrModsInFolders
	}
	sort.Strings(a.ignored)
	return a, nil
}

// archiveRoot is the one folder every entry is in ("custom_mods/" for a zipped
// folder), or "" when anything sits at the top.
func archiveRoot(files []*zip.File) string {
	root, set := "", false
	for _, zf := range files {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if archiveJunk(name) {
			continue
		}
		first, _, nested := strings.Cut(name, "/")
		if !nested || first == "" || (set && first != root) {
			return ""
		}
		root, set = first, true
	}
	if !set {
		return ""
	}
	return root + "/"
}

// archiveJunk are the files macOS and Windows add to a zip on their own.
func archiveJunk(name string) bool {
	base := name[strings.LastIndex(name, "/")+1:]
	return strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, "._") || base == ".DS_Store" || base == "Thumbs.db"
}

// badModName reports whether a jar name can't be used as a file in mods/ on every
// supported OS, or is hidden.
func badModName(n string) bool {
	if strings.HasPrefix(n, ".") || strings.ContainsAny(n, `<>:"/\|?*`) || windowsReserved(n) {
		return true
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
