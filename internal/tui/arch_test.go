package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Architecture invariants established by the cleanup slice: they record where shared
// layout maths, list state, the error screen, colours and shared texts live, so the
// duplication the slice removed can't silently come back.

type srcLine struct {
	file string
	num  int
	text string
}

// archSources reads the package's own non-test .go files.
func archSources(t *testing.T) (map[string][]string, []string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]string{}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || filepath.Ext(n) != ".go" || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", n))
		if err != nil {
			t.Fatal(err)
		}
		files[n] = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		names = append(names, n)
	}
	sort.Strings(names)
	return files, names
}

// matches lists every line of every file that match reports true for, in file order.
func matches(files map[string][]string, names []string, match func(string) bool) []srcLine {
	var out []srcLine
	for _, n := range names {
		for i, l := range files[n] {
			if match(l) {
				out = append(out, srcLine{n, i + 1, strings.TrimSpace(l)})
			}
		}
	}
	return out
}

func describe(hits []srcLine) string {
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "\n  %s:%d: %s", h.file, h.num, h.text)
	}
	return b.String()
}

// onlyIn fails unless every hit is in file and there are exactly want of them
// (want < 0: any number, including none).
func onlyIn(t *testing.T, what string, hits []srcLine, file string, want int) {
	t.Helper()
	var outside []srcLine
	for _, h := range hits {
		if h.file != file {
			outside = append(outside, h)
		}
	}
	if len(outside) > 0 {
		t.Errorf("%s outside %s:%s", what, file, describe(outside))
	}
	if want >= 0 && len(hits) != want {
		t.Errorf("%s: %d occurrences, want exactly %d (in %s):%s", what, len(hits), want, file, describe(hits))
	}
}

func nowhere(t *testing.T, what string, hits []srcLine) {
	t.Helper()
	if len(hits) > 0 {
		t.Errorf("%s must not appear:%s", what, describe(hits))
	}
}

func contains(sub string) func(string) bool {
	return func(l string) bool { return strings.Contains(l, sub) }
}

func TestArchitecture(t *testing.T) {
	files, names := archSources(t)
	t.Logf("scanned %d non-test files: %s", len(names), strings.Join(names, ", "))
	if len(names) < 10 { // C9: guards against running in the wrong directory
		t.Fatalf("scanned %d non-test .go files, want at least 10 (wrong directory?)", len(names))
	}
	find := func(match func(string) bool) []srcLine { return matches(files, names, match) }

	t.Run("body width maths lives in layout.go", func(t *testing.T) { // C1
		widthMaths := regexp.MustCompile(`m\.width\s*-\s*(4|8|10)\b`)
		onlyIn(t, "m.width-4/-8/-10", find(widthMaths.MatchString), "layout.go", 1)
	})

	t.Run("list filter state lives in lists.go", func(t *testing.T) { // C2
		onlyIn(t, "list.Filtering", find(contains("list.Filtering")), "lists.go", 1)
		onlyIn(t, "list.Unfiltered", find(contains("list.Unfiltered")), "lists.go", 1)
	})

	t.Run("selected item lookup lives in lists.go", func(t *testing.T) { // C3
		onlyIn(t, "SelectedItem().(item)", find(contains("SelectedItem().(item)")), "lists.go", 1)
	})

	t.Run("only fail sets the error screen", func(t *testing.T) { // C4
		assignErr := regexp.MustCompile(`(^|[^=!<>])=[^=].*\bscError\b`)
		onlyIn(t, "assignment of scError", find(assignErr.MatchString), "tui.go", 1)
	})

	t.Run("colours live in layout.go", func(t *testing.T) { // C5
		onlyIn(t, `lipgloss.Color("#`, find(contains(`lipgloss.Color("#`)), "layout.go", -1)
		hexLit := regexp.MustCompile(`"#[0-9a-fA-F]`)
		onlyIn(t, `"#<hex>`, find(hexLit.MatchString), "layout.go", -1)
	})

	t.Run("message blocks live in blocks.go", func(t *testing.T) { // C6
		onlyIn(t, "okSty.Bold(true)", find(contains("okSty.Bold(true)")), "blocks.go", 1)
		onlyIn(t, "warnSty.Render(l)", find(contains("warnSty.Render(l)")), "blocks.go", -1)
		nowhere(t, "bullet := func", find(contains("bullet := func")))
		nowhere(t, "titleSty.Render(wrap", find(contains("titleSty.Render(wrap")))
		nowhere(t, "b.WriteString(m.bullet(", find(contains("b.WriteString(m.bullet(")))
	})

	t.Run("shared texts are written once in blocks.go", func(t *testing.T) { // C7
		for _, s := range []string{"This goes BACK", "Your worlds, screenshots", "Renamed the instance in Prism", "Nothing was changed."} {
			onlyIn(t, fmt.Sprintf("%q", s), find(contains(s)), "blocks.go", 1)
		}
	})

	t.Run("files stay within their line budget", func(t *testing.T) { // C8
		// Ratchet: these allowances predate the cleanup slice; the files may be split later.
		allowance := map[string]int{"settings.go": 500, "tui.go": 400, "layout.go": 330}
		for _, n := range names {
			limit, ok := allowance[n]
			if !ok {
				limit = 300
			}
			lines := len(files[n])
			if last := files[n][lines-1]; last == "" { // a trailing newline is not a line
				lines--
			}
			if lines > limit {
				t.Errorf("%s has %d lines, budget %d", n, lines, limit)
			}
		}
	})
}
