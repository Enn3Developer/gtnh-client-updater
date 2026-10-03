package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// C3: architecture ratchets over every non-test .go file of the package.

type srcFile struct {
	name  string
	text  string
	lines []string // lines[i] is line i+1
}

func packageSources(t *testing.T) []srcFile {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []srcFile
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		out = append(out, srcFile{name: n, text: text, lines: strings.Split(text, "\n")})
	}
	if len(out) == 0 {
		t.Fatal("no source files found")
	}
	return out
}

// lineCount is the number of "\n", plus one when the last byte isn't one.
func lineCount(text string) int {
	n := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// funcRange is the line range [start, end] of the top-level func or method name in file.
func funcRange(t *testing.T, f srcFile, name string) (int, int) {
	t.Helper()
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, f.name, f.text, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
		}
	}
	return -1, -1
}

type match struct {
	file string
	line int
	text string
	rest string // the rest of the line after the match
}

func findAll(files []srcFile, re *regexp.Regexp) []match {
	var out []match
	for _, f := range files {
		for i, l := range f.lines {
			for _, loc := range re.FindAllStringIndex(l, -1) {
				out = append(out, match{f.name, i + 1, l[loc[0]:loc[1]], l[loc[1]:]})
			}
		}
	}
	return out
}

var (
	dialogAssign   = regexp.MustCompile(`\bm\.dialog\s*=[^=]`)
	colourLiteral  = regexp.MustCompile(`"#[0-9A-Fa-f]{3,8}"`)
	realBoundaries = regexp.MustCompile(`\b(prism\.IsRunning|prism\.FindLauncher|prism\.Launch|appcfg\.Save|update\.Restore)\b`)
	bannedWords    = []string{"instance.cfg", "flatpak", "host:port", "baseline", "reconcile", "gtnh-update"}
)

func TestArchitecture(t *testing.T) {
	files := packageSources(t)

	t.Run("a file length", func(t *testing.T) {
		for _, f := range files {
			if n := lineCount(f.text); n > 450 {
				t.Errorf("%s is %d lines, limit 450: split it", f.name, n)
			}
		}
	})

	t.Run("b dialog assignments", func(t *testing.T) {
		var dialog srcFile
		for _, f := range files {
			if f.name == "dialog.go" {
				dialog = f
			}
		}
		if dialog.name == "" {
			t.Fatal("dialog.go not found")
		}
		oStart, oEnd := funcRange(t, dialog, "openDialog")
		cStart, cEnd := funcRange(t, dialog, "closeDialog")
		inOpen, inClose := 0, 0
		ms := findAll(files, dialogAssign)
		for _, mt := range ms {
			switch {
			case mt.file == "dialog.go" && mt.line >= oStart && mt.line <= oEnd:
				inOpen++
			case mt.file == "dialog.go" && mt.line >= cStart && mt.line <= cEnd:
				inClose++
			default:
				t.Errorf("m.dialog is set at %s:%d; only openDialog/closeDialog may", mt.file, mt.line)
			}
		}
		if len(ms) != 2 || inOpen != 1 || inClose != 1 {
			t.Errorf("m.dialog is set %d times (openDialog %d, closeDialog %d); want exactly once in each", len(ms), inOpen, inClose)
		}
	})

	t.Run("c colour literals", func(t *testing.T) {
		for _, mt := range findAll(files, colourLiteral) {
			if mt.file != "layout.go" {
				t.Errorf("colour literal at %s:%d; colours live in layout.go", mt.file, mt.line)
			}
		}
	})

	t.Run("d real boundaries", func(t *testing.T) {
		var tuiGo srcFile
		for _, f := range files {
			if f.name == "tui.go" {
				tuiGo = f
			}
		}
		start, end := funcRange(t, tuiGo, "newModel")
		for _, mt := range findAll(files, realBoundaries) {
			inNewModel := mt.file == "tui.go" && mt.line > start && mt.line < end
			if !inNewModel || strings.HasPrefix(mt.rest, "(") {
				t.Errorf("%s:%d calls %s directly; go through the model's injected field", mt.file, mt.line, mt.text)
			}
		}
	})

	t.Run("e voice", func(t *testing.T) {
		for _, f := range files {
			fset := token.NewFileSet()
			af, err := parser.ParseFile(fset, f.name, f.text, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(af, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					s = lit.Value
				}
				low := strings.ReplaceAll(strings.ToLower(s), "gtnh-updater", "")
				for _, w := range bannedWords {
					if strings.Contains(low, w) {
						t.Errorf("%s:%d: %q contains %q, which players shouldn't see", f.name, fset.Position(lit.Pos()).Line, s, w)
					}
				}
				return true
			})
		}
	})
}
