package update

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

const (
	cfgBoth   = ".minecraft/config/both.cfg"    // conflict: player and pack both changed it
	cfgOther  = ".minecraft/config/other.cfg"   // second conflict
	cfgNew    = ".minecraft/config/new.cfg"     // Install: only in the new pack
	cfgDrop   = ".minecraft/config/dropped.cfg" // Remove: dropped by the pack, untouched by player
	cfgAbsent = ".minecraft/config/nowhere.cfg" // in no action at all
)

// choicePlan builds a real instance and plan with two Conflicts, one Install and one Remove.
func choicePlan(t *testing.T) (prism.Instance, *Plan, map[string]string) {
	t.Helper()
	// modX is unchanged everywhere (no action); it only makes the pack a valid GTNH pack.
	base := map[string]string{cfgBoth: "v1", cfgOther: "v1", cfgDrop: "v1", modX: "x1"}
	disk := map[string]string{cfgBoth: "mine", cfgOther: "mine too", cfgDrop: "v1", modX: "x1"}
	next := map[string]string{cfgBoth: "v2", cfgOther: "v2 other", cfgNew: "fresh", modX: "x1"}
	inst := newInstance(t, "i", disk)
	B, N := fps(base), fps(next)
	pl := MakePlan(inst, B, N, Scan(inst, B, N, nil), nil)
	want := map[string]Kind{cfgBoth: Conflict, cfgOther: Conflict, cfgNew: Install, cfgDrop: Remove}
	if got := planKinds(pl); !reflect.DeepEqual(got, want) {
		t.Fatalf("setup plan = %v, want %v", got, want)
	}
	return inst, pl, next
}

func actionFor(t *testing.T, pl *Plan, path string) Action {
	t.Helper()
	for _, a := range pl.Actions {
		if a.Path == path {
			return a
		}
	}
	t.Fatalf("no action for %s", path)
	return Action{}
}

func readFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// C1
func TestChoiceOfIsKeepMineForEveryPathOnFreshPlan(t *testing.T) {
	_, pl, _ := choicePlan(t)
	for _, p := range []string{cfgBoth, cfgOther, cfgNew, cfgDrop, cfgAbsent} {
		if got := pl.ChoiceOf(p); got != KeepMine {
			t.Errorf("ChoiceOf(%s) = %v, want KeepMine", p, got)
		}
	}
}

// C1 (zero-value Plan, no storage) + C2
func TestChooseWorksOnZeroValuePlan(t *testing.T) {
	var pl Plan
	pl.Choose(cfgBoth, TakeNew)
	if got := pl.ChoiceOf(cfgBoth); got != TakeNew {
		t.Errorf("ChoiceOf(%s) = %v, want TakeNew", cfgBoth, got)
	}
}

// C2
func TestChooseSetsOnlyThatPath(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.Choose(cfgBoth, TakeNew)
	if got := pl.ChoiceOf(cfgBoth); got != TakeNew {
		t.Errorf("ChoiceOf(%s) = %v, want TakeNew", cfgBoth, got)
	}
	if got := pl.ChoiceOf(cfgOther); got != KeepMine {
		t.Errorf("ChoiceOf(%s) = %v, want KeepMine (untouched path)", cfgOther, got)
	}
}

// C2
func TestChooseOverwritesEarlierChoiceForSamePath(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.Choose(cfgBoth, TakeNew)
	pl.Choose(cfgBoth, KeepMine)
	if got := pl.ChoiceOf(cfgBoth); got != KeepMine {
		t.Errorf("after TakeNew then KeepMine, ChoiceOf = %v, want KeepMine", got)
	}
	pl.Choose(cfgBoth, TakeNew)
	if got := pl.ChoiceOf(cfgBoth); got != TakeNew {
		t.Errorf("after KeepMine then TakeNew, ChoiceOf = %v, want TakeNew", got)
	}
}

// C2: a choice on a non-conflict path is stored but not listed by Chosen.
func TestChooseOnNonConflictPathIsStoredButNotChosen(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.Choose(cfgNew, TakeNew)
	pl.Choose(cfgAbsent, TakeNew)
	if got := pl.ChoiceOf(cfgNew); got != TakeNew {
		t.Errorf("ChoiceOf(%s) = %v, want TakeNew", cfgNew, got)
	}
	if got := pl.ChoiceOf(cfgAbsent); got != TakeNew {
		t.Errorf("ChoiceOf(%s) = %v, want TakeNew", cfgAbsent, got)
	}
	if got := pl.Chosen(TakeNew); len(got) != 0 {
		t.Errorf("Chosen(TakeNew) = %v, want empty", got)
	}
}

// C2: a choice on Install/Remove paths does not change what Apply does to them.
func TestChooseOnNonConflictPathHasNoEffectOnApply(t *testing.T) {
	inst, pl, next := choicePlan(t)
	pl.Choose(cfgNew, KeepMine)
	pl.Choose(cfgDrop, TakeNew)
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, openPack(t, next), inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, inst, cfgNew); got != "fresh" {
		t.Errorf("Install path content = %q, want %q", got, "fresh")
	}
	if _, ok := readFile(DiskPath(inst, cfgNew) + ".mcnew"); ok {
		t.Errorf("Install path got a .mcnew")
	}
	if _, ok := read(t, inst, cfgDrop); ok {
		t.Errorf("Remove path still on disk")
	}
}

// C3
func TestChooseAllSetsEveryConflictOverwritingEarlierChoices(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.Choose(cfgBoth, KeepMine)
	pl.ChooseAll(TakeNew)
	if got := pl.ChoiceOf(cfgBoth); got != TakeNew {
		t.Errorf("ChooseAll(TakeNew): ChoiceOf(%s) = %v, want TakeNew", cfgBoth, got)
	}
	if got := pl.ChoiceOf(cfgOther); got != TakeNew {
		t.Errorf("ChooseAll(TakeNew): ChoiceOf(%s) = %v, want TakeNew", cfgOther, got)
	}
	pl.ChooseAll(KeepMine)
	if got := pl.ChoiceOf(cfgBoth); got != KeepMine {
		t.Errorf("ChooseAll(KeepMine): ChoiceOf(%s) = %v, want KeepMine", cfgBoth, got)
	}
	if got := pl.ChoiceOf(cfgOther); got != KeepMine {
		t.Errorf("ChooseAll(KeepMine): ChoiceOf(%s) = %v, want KeepMine", cfgOther, got)
	}
}

// C3, K3: ChooseAll leaves Install/Remove paths without a choice.
func TestChooseAllDoesNotTouchInstallOrRemovePaths(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.ChooseAll(TakeNew)
	if got := pl.ChoiceOf(cfgNew); got != KeepMine {
		t.Errorf("ChoiceOf(Install %s) = %v, want KeepMine", cfgNew, got)
	}
	if got := pl.ChoiceOf(cfgDrop); got != KeepMine {
		t.Errorf("ChoiceOf(Remove %s) = %v, want KeepMine", cfgDrop, got)
	}
	want := []string{cfgBoth, cfgOther}
	if got := pl.Chosen(TakeNew); !reflect.DeepEqual(got, want) {
		t.Errorf("Chosen(TakeNew) = %v, want %v", got, want)
	}
}

// C3: the Path is used verbatim, also for a disabled-mod-style or odd path.
func TestChooseAllUsesActionPathVerbatim(t *testing.T) {
	odd := ".minecraft/config/Sub Dir/X.CFG"
	pl := &Plan{Actions: []Action{{Kind: Conflict, Path: odd, Disk: "ignored"}}}
	pl.ChooseAll(TakeNew)
	if got := pl.ChoiceOf(odd); got != TakeNew {
		t.Errorf("ChoiceOf(%q) = %v, want TakeNew", odd, got)
	}
	if got := pl.ChoiceOf(strings.ToLower(odd)); got != KeepMine {
		t.Errorf("ChoiceOf(lowercased) = %v, want KeepMine", got)
	}
}

// reversedConflicts is a plan whose Conflict actions are in reverse-sorted order, with
// Install/Remove actions in between.
func reversedConflicts() *Plan {
	return &Plan{Actions: []Action{
		{Kind: Conflict, Path: "h.cfg"},
		{Kind: Conflict, Path: "g.cfg"},
		{Kind: Install, Path: "f-install.cfg"},
		{Kind: Conflict, Path: "f.cfg"},
		{Kind: Conflict, Path: "e.cfg"},
		{Kind: Conflict, Path: "d.cfg"},
		{Kind: Remove, Path: "c-remove.cfg"},
		{Kind: Conflict, Path: "c.cfg"},
		{Kind: Conflict, Path: "b.cfg"},
		{Kind: Conflict, Path: "a.cfg"},
	}}
}

// C4, K2
func TestChosenTakeNewIsSortedAscending(t *testing.T) {
	pl := reversedConflicts()
	pl.ChooseAll(TakeNew)
	want := []string{"a.cfg", "b.cfg", "c.cfg", "d.cfg", "e.cfg", "f.cfg", "g.cfg", "h.cfg"}
	if got := pl.Chosen(TakeNew); !reflect.DeepEqual(got, want) {
		t.Errorf("Chosen(TakeNew) = %v, want %v", got, want)
	}
}

// C4, K2: unset paths count as KeepMine and are also sorted.
func TestChosenKeepMineIsSortedAndIncludesUnsetConflicts(t *testing.T) {
	pl := reversedConflicts()
	pl.Choose("g.cfg", TakeNew)
	pl.Choose("b.cfg", TakeNew)
	want := []string{"a.cfg", "c.cfg", "d.cfg", "e.cfg", "f.cfg", "h.cfg"}
	if got := pl.Chosen(KeepMine); !reflect.DeepEqual(got, want) {
		t.Errorf("Chosen(KeepMine) = %v, want %v", got, want)
	}
	wantNew := []string{"b.cfg", "g.cfg"}
	if got := pl.Chosen(TakeNew); !reflect.DeepEqual(got, wantNew) {
		t.Errorf("Chosen(TakeNew) = %v, want %v", got, wantNew)
	}
}

// C4: sort.Strings order is byte order (upper case before lower case).
func TestChosenUsesByteOrder(t *testing.T) {
	pl := &Plan{Actions: []Action{
		{Kind: Conflict, Path: "b.cfg"},
		{Kind: Conflict, Path: "a.cfg"},
		{Kind: Conflict, Path: "Z.cfg"},
	}}
	want := []string{"Z.cfg", "a.cfg", "b.cfg"}
	if got := pl.Chosen(KeepMine); !reflect.DeepEqual(got, want) {
		t.Errorf("Chosen(KeepMine) = %v, want %v", got, want)
	}
}

// C4
func TestChosenIsEmptyWhenNoConflictHasThatChoice(t *testing.T) {
	_, pl, _ := choicePlan(t)
	if got := pl.Chosen(TakeNew); len(got) != 0 {
		t.Errorf("fresh plan Chosen(TakeNew) = %v, want empty", got)
	}
	pl.ChooseAll(TakeNew)
	if got := pl.Chosen(KeepMine); len(got) != 0 {
		t.Errorf("after ChooseAll(TakeNew), Chosen(KeepMine) = %v, want empty", got)
	}
}

// C4
func TestChosenIsEmptyForPlanWithoutConflicts(t *testing.T) {
	pl := &Plan{Actions: []Action{{Kind: Install, Path: "x.cfg"}, {Kind: Remove, Path: "y.cfg"}}}
	if got := pl.Chosen(KeepMine); len(got) != 0 {
		t.Errorf("Chosen(KeepMine) = %v, want empty", got)
	}
}

// C8
func TestConflictsAndCountIgnoreChoices(t *testing.T) {
	_, pl, _ := choicePlan(t)
	pl.Choose(cfgBoth, TakeNew)
	wantConflicts := []string{cfgBoth, cfgOther}
	got := pl.Conflicts()
	if !reflect.DeepEqual(sortedCopy(got), wantConflicts) {
		t.Errorf("Conflicts() = %v, want %v", got, wantConflicts)
	}
	if n := pl.Count(Conflict); n != 2 {
		t.Errorf("Count(Conflict) = %d, want 2", n)
	}
	pl.ChooseAll(TakeNew)
	if got := pl.Conflicts(); !reflect.DeepEqual(sortedCopy(got), wantConflicts) {
		t.Errorf("after ChooseAll, Conflicts() = %v, want %v", got, wantConflicts)
	}
	if n := pl.Count(Conflict); n != 2 {
		t.Errorf("after ChooseAll, Count(Conflict) = %d, want 2", n)
	}
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// C5, K1
func TestApplyKeepMineWritesMcnewAndLeavesFile(t *testing.T) {
	inst, pl, next := choicePlan(t)
	pl.Choose(cfgBoth, KeepMine)
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, openPack(t, next), inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	a := actionFor(t, pl, cfgBoth)
	if got, _ := readFile(a.Disk); got != "mine" {
		t.Errorf("%s = %q, want %q (player's file kept)", cfgBoth, got, "mine")
	}
	if got, _ := readFile(a.Disk + ".mcnew"); got != "v2" {
		t.Errorf("%s.mcnew = %q, want %q", cfgBoth, got, "v2")
	}
}

// C6, K1
func TestApplyTakeNewReplacesFileAndBacksUpOld(t *testing.T) {
	inst, pl, next := choicePlan(t)
	pl.Choose(cfgBoth, TakeNew)
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, openPack(t, next), inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	a := actionFor(t, pl, cfgBoth)
	if got, _ := readFile(a.Disk); got != "v2" {
		t.Errorf("%s = %q, want pack content %q", cfgBoth, got, "v2")
	}
	if _, ok := readFile(a.Disk + ".mcnew"); ok {
		t.Errorf("%s.mcnew created for a TakeNew conflict", cfgBoth)
	}
	if got, _ := readFile(filepath.Join(backup, ".minecraft", "config", "both.cfg")); got != "mine" {
		t.Errorf("backup of %s = %q, want %q", cfgBoth, got, "mine")
	}
}

// C5 + C6 together: the other conflict keeps its default.
func TestApplyMixedChoicesTreatsEachConflictByItsChoice(t *testing.T) {
	inst, pl, next := choicePlan(t)
	pl.Choose(cfgOther, TakeNew)
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, openPack(t, next), inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	both, other := actionFor(t, pl, cfgBoth), actionFor(t, pl, cfgOther)
	if got, _ := readFile(both.Disk); got != "mine" {
		t.Errorf("KeepMine %s = %q, want %q", cfgBoth, got, "mine")
	}
	if got, _ := readFile(both.Disk + ".mcnew"); got != "v2" {
		t.Errorf("KeepMine %s.mcnew = %q, want %q", cfgBoth, got, "v2")
	}
	if got, _ := readFile(other.Disk); got != "v2 other" {
		t.Errorf("TakeNew %s = %q, want %q", cfgOther, got, "v2 other")
	}
	if _, ok := readFile(other.Disk + ".mcnew"); ok {
		t.Errorf("TakeNew %s got a .mcnew", cfgOther)
	}
}

// C6: a .mcnew left from an earlier run is not touched by a TakeNew conflict.
func TestApplyTakeNewLeavesPreexistingMcnewAlone(t *testing.T) {
	inst, pl, next := choicePlan(t)
	a := actionFor(t, pl, cfgBoth)
	if err := os.WriteFile(a.Disk+".mcnew", []byte("old mcnew"), 0o644); err != nil {
		t.Fatal(err)
	}
	pl.Choose(cfgBoth, TakeNew)
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, openPack(t, next), inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := readFile(a.Disk + ".mcnew"); got != "old mcnew" {
		t.Errorf("pre-existing .mcnew = %q (exists %v), want %q", got, ok, "old mcnew")
	}
	if got, _ := readFile(a.Disk); got != "v2" {
		t.Errorf("%s = %q, want %q", cfgBoth, got, "v2")
	}
}

// C7
func TestApplyTakeNewIsRestoredOnRollback(t *testing.T) {
	inst, pl, next := choicePlan(t)
	pl.Choose(cfgBoth, TakeNew)
	missing := ".minecraft/config/missing.cfg"
	pl.Actions = append(pl.Actions, Action{Kind: Install, Path: missing, Disk: DiskPath(inst, missing)})
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	err := Apply(pl, openPack(t, next), inst.Dir, backup, nil)
	if err == nil || !strings.Contains(err.Error(), "not in pack") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("Apply error = %v, want a rolled-back \"not in pack\" error", err)
	}
	a := actionFor(t, pl, cfgBoth)
	if got, _ := readFile(a.Disk); got != "mine" {
		t.Errorf("after rollback %s = %q, want %q", cfgBoth, got, "mine")
	}
	if _, statErr := os.Stat(backup); !os.IsNotExist(statErr) {
		t.Errorf("backup dir still exists after rollback (stat err %v)", statErr)
	}
}
