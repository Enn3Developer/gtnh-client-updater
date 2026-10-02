package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- C2 ParseChoice / Recommended ----

func TestC2ParseChoiceNewIsTakeNew(t *testing.T) {
	got, err := ParseChoice("new")
	if err != nil || got != TakeNew {
		t.Errorf("ParseChoice(\"new\") = %v, %v; want TakeNew, nil", got, err)
	}
}

func TestC2ParseChoiceMineIsKeepMine(t *testing.T) {
	got, err := ParseChoice("mine")
	if err != nil || got != KeepMine {
		t.Errorf("ParseChoice(\"mine\") = %v, %v; want KeepMine, nil", got, err)
	}
}

func TestC2ParseChoiceRejectsOtherValuesNamingThem(t *testing.T) {
	for _, in := range []string{"", "NEW", "both"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			_, err := ParseChoice(in)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", in)) {
				t.Errorf("ParseChoice(%q) error = %v, want an error mentioning %q", in, err, in)
			}
		})
	}
}

func TestC2RecommendedIsTakeNew(t *testing.T) {
	if Recommended != TakeNew {
		t.Errorf("Recommended = %v, want TakeNew", Recommended)
	}
}

// ---- C3 BaselineSuspect (K1) ----

func TestC3BaselineSuspectBoundary(t *testing.T) {
	cases := []struct {
		match float64
		want  bool
	}{{0.79, true}, {0.8, false}, {1.0, false}}
	for _, c := range cases {
		if got := (&Plan{BaselineMatch: c.match}).BaselineSuspect(); got != c.want {
			t.Errorf("BaselineSuspect() with match %v = %v, want %v", c.match, got, c.want)
		}
	}
}

// ---- C4 IsDowngrade (K2) ----

func TestC4IsDowngrade(t *testing.T) {
	cases := []struct {
		target, installed string
		want              bool
	}{
		{"2.8.4", "2.9.0-RC-1", true},
		{"2.8.4", "2.8.4", false},
		{"2.9.0", "2.8.4", false},
	}
	for _, c := range cases {
		if got := IsDowngrade(c.target, c.installed); got != c.want {
			t.Errorf("IsDowngrade(%q, %q) = %v, want %v", c.target, c.installed, got, c.want)
		}
	}
}

// ---- C5 Apply's rollback error ----

// failingPlan removes cfgDrop, then fails on a file that isn't in the pack.
func failingPlan(t *testing.T) (dir, backup string, run func(progress func(done, total int)) error) {
	t.Helper()
	inst, pl, next := choicePlan(t)
	missing := ".minecraft/config/missing.cfg"
	pl.Actions = []Action{
		{Kind: Remove, Path: cfgDrop, Disk: DiskPath(inst, cfgDrop)},
		{Kind: Install, Path: missing, Disk: DiskPath(inst, missing)},
	}
	backup = filepath.Join(inst.Dir, StateDir, "backup-t")
	p := openPack(t, next)
	return inst.Dir, backup, func(progress func(done, total int)) error {
		return Apply(pl, p, inst.Dir, backup, progress)
	}
}

func TestC5ApplyRollbackErrorIsErrRolledBack(t *testing.T) {
	_, _, run := failingPlan(t)
	err := run(nil)
	if !errors.Is(err, ErrRolledBack) || !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("Apply error = %v; want errors.Is ErrRolledBack and text containing \"rolled back\"", err)
	}
}

func TestC5ApplyFailedRollbackIsNotErrRolledBack(t *testing.T) {
	_, backup, run := failingPlan(t)
	// After the removal, delete the backup so the removed file can't be put back.
	err := run(func(done, _ int) {
		if done == 1 {
			os.RemoveAll(backup)
		}
	})
	if err == nil || errors.Is(err, ErrRolledBack) || !strings.Contains(err.Error(), "ROLLBACK ALSO FAILED") {
		t.Errorf("Apply error = %v; want a ROLLBACK ALSO FAILED error that is not ErrRolledBack", err)
	}
}
