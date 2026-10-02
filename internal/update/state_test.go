package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

const playAddr = "play.example:25565"

// C1, C4: never-saved instance -> fn gets an empty state with a non-nil Baseline.
func TestUpdateStateOnNeverSavedInstanceStartsFromEmptyState(t *testing.T) {
	dir := t.TempDir()
	var seen State
	var seenNilBaseline bool
	calls := 0
	err := UpdateState(dir, func(st *State) {
		calls++
		seen = *st
		seenNilBaseline = st.Baseline == nil
		st.ServerAddress = playAddr
	})
	if err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
	if seenNilBaseline || len(seen.Baseline) != 0 {
		t.Errorf("fn saw Baseline %v (nil %v), want an empty non-nil map", seen.Baseline, seenNilBaseline)
	}
	if seen.Version != "" || seen.ServerAddress != "" || seen.CustomModsURL != "" || seen.CustomModsAsked || len(seen.CustomMods) != 0 {
		t.Errorf("fn saw %+v, want an empty state", seen)
	}
}

// C4: the new state.json holds only what fn set.
func TestUpdateStateOnInstanceWithoutStateDirCreatesStateJSON(t *testing.T) {
	dir := t.TempDir()
	if err := UpdateState(dir, func(st *State) { st.ServerAddress = playAddr }); err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(dir, StateDir, "state.json")); err != nil {
		t.Fatalf("state.json not created: %v", err)
	}
	st, err := LoadState(dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	want := State{Baseline: map[string]pack.Fingerprint{}, ServerAddress: playAddr}
	if !reflect.DeepEqual(*st, want) {
		t.Errorf("state = %+v, want %+v", *st, want)
	}
}

// C1: fn sees the saved state and only its edits change the file.
func TestUpdateStateKeepsSavedFieldsAndAppliesOnlyFnEdits(t *testing.T) {
	dir := t.TempDir()
	saved := State{
		Version:         "2.8.4",
		Baseline:        map[string]pack.Fingerprint{"mods/a.jar": {Size: 3, CRC: 7}},
		CustomMods:      []string{"extra-1.jar"},
		CustomModsURL:   "https://files.example/custom_mods.zip",
		CustomModsAsked: true,
		ServerAddress:   "old.example",
	}
	if err := SaveState(dir, &saved); err != nil {
		t.Fatal(err)
	}
	var seen State
	err := UpdateState(dir, func(st *State) {
		seen = *st
		st.ServerAddress = playAddr
	})
	if err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	if !reflect.DeepEqual(seen, saved) {
		t.Errorf("fn saw %+v, want the saved %+v", seen, saved)
	}
	got, err := LoadState(dir)
	if err != nil || got == nil {
		t.Fatalf("LoadState = %v, %v", got, err)
	}
	want := State{
		Version:         "2.8.4",
		Baseline:        map[string]pack.Fingerprint{"mods/a.jar": {Size: 3, CRC: 7}},
		CustomMods:      []string{"extra-1.jar"},
		CustomModsURL:   "https://files.example/custom_mods.zip",
		CustomModsAsked: true,
		ServerAddress:   playAddr,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("state = %+v, want %+v", *got, want)
	}
}

// C1: a load error is returned, fn is never called, the file is left as it was.
func TestUpdateStateInvalidJSONReturnsErrorWithoutCallingFn(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, StateDir, "state.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	const broken = `{"version": "2.8.4", not json`
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	err := UpdateState(dir, func(st *State) {
		called = true
		st.ServerAddress = playAddr
	})
	if err == nil {
		t.Error("UpdateState on invalid JSON = nil, want an error")
	}
	if called {
		t.Error("fn was called despite the load error")
	}
	if got := mustRead(t, p); got != broken {
		t.Errorf("state.json = %q, want it untouched %q", got, broken)
	}
}

// C1: a save error is returned. A non-empty directory where the temp file goes makes
// the write fail on every OS; loading succeeds (no state.json yet).
func TestUpdateStateReturnsSaveError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, StateDir, "state.json.tmp")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "inside"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	err := UpdateState(dir, func(st *State) {
		called = true
		st.ServerAddress = playAddr
	})
	if err == nil {
		t.Error("UpdateState with an unwritable state = nil, want the save error")
	}
	if !called {
		t.Error("fn not called although loading succeeded")
	}
}

// C3: an old state.json without the key loads with ServerAddress "" and keeps the rest.
func TestUpdateStateOnOldStateWithoutServerAddressKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, StateDir, "state.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"version":"2.8.4","baseline":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var seenVersion, seenAddr string
	seenAddr = "sentinel"
	err := UpdateState(dir, func(st *State) {
		seenVersion, seenAddr = st.Version, st.ServerAddress
	})
	if err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	if seenVersion != "2.8.4" || seenAddr != "" {
		t.Errorf("fn saw version %q address %q, want %q %q", seenVersion, seenAddr, "2.8.4", "")
	}
	st, err := LoadState(dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if st.Version != "2.8.4" || st.ServerAddress != "" {
		t.Errorf("state = %+v, want version 2.8.4 and no server address", *st)
	}
}

// C3: an empty ServerAddress is left out of the JSON.
func TestStateWithoutServerAddressOmitsTheKey(t *testing.T) {
	dir := t.TempDir()
	if err := UpdateState(dir, func(st *State) { st.Version = "2.8.4" }); err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	if got := mustRead(t, filepath.Join(dir, StateDir, "state.json")); strings.Contains(got, "serverAddress") {
		t.Errorf("state.json = %s, want no serverAddress key", got)
	}
	b, err := json.Marshal(&State{Version: "2.8.4", Baseline: map[string]pack.Fingerprint{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "serverAddress") {
		t.Errorf("json.Marshal = %s, want no serverAddress key", b)
	}
}

// C3: a set ServerAddress is stored under "serverAddress" and round-trips.
func TestStateServerAddressRoundTripsUnderItsKey(t *testing.T) {
	dir := t.TempDir()
	if err := UpdateState(dir, func(st *State) { st.ServerAddress = playAddr }); err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	raw := mustRead(t, filepath.Join(dir, StateDir, "state.json"))
	if !strings.Contains(raw, `"serverAddress":"play.example:25565"`) {
		t.Errorf("state.json = %s, want \"serverAddress\":\"play.example:25565\"", raw)
	}
	other := t.TempDir()
	if err := SaveState(other, &State{Version: "2.9.0", Baseline: map[string]pack.Fingerprint{}, ServerAddress: "mc.example"}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(other)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if st.ServerAddress != "mc.example" {
		t.Errorf("ServerAddress after SaveState/LoadState = %q, want %q", st.ServerAddress, "mc.example")
	}
}

// C4: a state made by UpdateState alone has Version "", so DetectVersion still uses the
// instance name.
func TestDetectVersionIgnoresStateWithoutVersion(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/19","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstance(t, "GT_New_Horizons_2.8.4_Java_17-25", nil)
	if err := UpdateState(inst.Dir, func(st *State) { st.ServerAddress = playAddr }); err != nil {
		t.Fatalf("UpdateState = %v, want nil", err)
	}
	st, err := LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if d := DetectVersion(inst, st, m); d.Version != "2.8.4" || d.Source != "instance name" {
		t.Errorf("DetectVersion = %+v, want 2.8.4 from instance name", d)
	}
}
