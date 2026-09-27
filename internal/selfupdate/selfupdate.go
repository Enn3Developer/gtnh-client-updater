// Package selfupdate keeps gtnh-update itself up to date from its GitHub releases.
//
// A release carries one binary per OS/arch (named like build.sh names them) and a
// checksums.txt with their SHA-256. The new binary is downloaded next to the running
// one, verified against checksums.txt, and swapped in by rename. Windows cannot
// overwrite a running .exe but can rename it, so there the old binary is moved to
// <exe>.old first and deleted on the next start (CleanupOld).
//
// checksums.txt must carry a valid ed25519 signature (checksums.txt.sig) from the key
// embedded in the binary (signature.go), so a release is only installed if it was
// signed by the maintainers — not merely because it appeared on the releases page.
package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

// Repo is the GitHub repository releases come from.
const Repo = "Enn3Developer/gtnh-client-updater"

// Overridable in tests.
var (
	apiBase        = "https://api.github.com"
	downloadPrefix = "https://github.com/" + Repo + "/releases/download/"
	executable     = os.Executable
)

// Release is a published version of the tool.
type Release struct {
	Version string // without the leading "v"
	Page    string // release page URL, for "download it yourself" messages
	assets  map[string]string
}

// Latest asks GitHub for the newest published release.
func Latest(client *http.Client) (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var raw struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	r := &Release{Version: strings.TrimPrefix(raw.Tag, "v"), Page: raw.Page, assets: map[string]string{}}
	for _, a := range raw.Assets {
		r.assets[a.Name] = a.URL
	}
	return r, nil
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// Newer reports whether latest is a newer release than current. Development builds
// ("dev", or anything that is not a version) never offer updates.
func Newer(current, latest string) bool {
	c, l := semverRe.FindStringSubmatch(current), semverRe.FindStringSubmatch(latest)
	if c == nil || l == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(c[i])
		b, _ := strconv.Atoi(l[i])
		if a != b {
			return b > a
		}
	}
	return false
}

// AssetName is the release file for an OS/arch, matching build.sh.
func AssetName(goos, goarch string) string {
	name := "gtnh-update-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ErrNotWritable means the binary lives somewhere the user cannot write (e.g. a system
// directory); they have to download the new version themselves.
var ErrNotWritable = errors.New("can't replace the program file here")

// Apply downloads this platform's binary from r, verifies it and replaces the running
// executable. progress gets bytes downloaded.
func (r *Release) Apply(client *http.Client, progress func(done, total int64)) error {
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	binURL, sumsURL, sigURL := r.assets[name], r.assets["checksums.txt"], r.assets["checksums.txt.sig"]
	if binURL == "" || sumsURL == "" {
		return fmt.Errorf("release %s has no %s download", r.Version, name)
	}
	if sigURL == "" {
		return fmt.Errorf("release %s is not signed, so I won't install it -- download it yourself from %s", r.Version, r.Page)
	}
	for _, u := range []string{binURL, sumsURL, sigURL} {
		if !strings.HasPrefix(u, downloadPrefix) {
			return fmt.Errorf("refusing download from unexpected location: %s", u)
		}
	}
	sums, err := fetchSmall(client, sumsURL)
	if err != nil {
		return err
	}
	sig, err := fetchSmall(client, sigURL)
	if err != nil {
		return err
	}
	if err := VerifyChecksums(publicKey, sums, sig); err != nil {
		return err
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}

	exe, err := executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".gtnh-update-new-*")
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrNotWritable, err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName) // no-op once renamed into place

	if err := pack.Download(client, binURL, tmpName, progress); err != nil {
		return err
	}
	if got, err := fileSHA256(tmpName); err != nil {
		return err
	} else if got != want {
		return fmt.Errorf("downloaded file is damaged (checksum mismatch) -- try again")
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return swap(exe, tmpName)
}

// swap puts newFile in place of exe.
func swap(exe, newFile string) error {
	if runtime.GOOS != "windows" {
		if err := os.Rename(newFile, exe); err != nil {
			return fmt.Errorf("%w (%v)", ErrNotWritable, err)
		}
		return nil
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("%w (%v)", ErrNotWritable, err)
	}
	if err := os.Rename(newFile, exe); err != nil {
		os.Rename(old, exe) // put the working binary back
		return fmt.Errorf("%w (%v)", ErrNotWritable, err)
	}
	return nil
}

// CleanupOld deletes the binary a previous Windows self-update left behind.
func CleanupOld() {
	if exe, err := executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

func fetchSmall(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// sumFor finds name's SHA-256 in sha256sum-format content.
func sumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

func fileSHA256(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
