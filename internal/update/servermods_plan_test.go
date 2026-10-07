package update

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

// planCase is a server-mods sync on paper: contents are strings, fingerprinted.
type planCase struct {
	name     string
	archive  map[string]string
	disk     map[string]string
	managed  map[string]string // name -> recorded content; "" = not recorded
	pack     []string          // GTNH's jar names; nil = unknown
	ids      map[string][]string
	checkAll bool
	want     []string // describe() of every change, in order
	managedW []string // the managed jars afterwards
}

func (c planCase) input() modsInput {
	in := modsInput{archive: map[string]pack.Fingerprint{}, managed: map[string]*pack.Fingerprint{}, checkAll: c.checkAll}
	for n, v := range c.archive {
		in.archive[n] = fpOf(v)
	}
	for n := range c.disk {
		in.files = append(in.files, n)
	}
	sort.Strings(in.files)
	in.fp = func(name string) (pack.Fingerprint, bool) {
		v, ok := c.disk[name]
		return fpOf(v), ok
	}
	for n, v := range c.managed {
		if v == "" {
			in.managed[n] = nil
		} else {
			fp := fpOf(v)
			in.managed[n] = &fp
		}
	}
	if c.pack != nil {
		in.pack = map[string]string{}
		for _, n := range c.pack {
			in.pack[strings.ToLower(n)] = n
		}
	}
	if c.ids != nil {
		in.serverIDs = func(n string) []string { return c.ids["server:"+n] }
		in.diskIDs = func(n string) []string { return c.ids[n] }
	}
	return in
}

func describe(c ModChange) string {
	kind := [...]string{"add", "update", "remove", "adopt", "replace", "aside", "keep", "skip"}[c.Kind]
	s := kind + " " + c.Name
	if c.Disk != "" && c.Disk != c.Name {
		s += " @" + c.Disk
	}
	if c.With != "" {
		s += " ~" + c.With
	}
	if c.Kind == ModSkip && c.Disk != "" {
		s += " -" + c.Disk
	}
	if c.Preserve {
		s += " (kept)"
	}
	return s
}

func describeAll(p *ModsPlan) []string {
	out := []string{}
	for _, c := range p.Changes {
		out = append(out, describe(c))
	}
	return out
}

func runPlanCases(t *testing.T, cases []planCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := planMods(c.input())
			if got, want := describeAll(p), append([]string{}, c.want...); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("changes = %q\n           want %q", got, want)
			}
			if got := p.Installed(); fmt.Sprint(got) != fmt.Sprint(append([]string{}, c.managedW...)) {
				t.Errorf("managed afterwards = %v, want %v", got, c.managedW)
			}
			for _, n := range p.Installed() {
				if p.Managed[n] != fpOf(c.archive[n]) {
					t.Errorf("Managed[%s] isn't the server's content", n)
				}
			}
		})
	}
}

var gtnh = []string{"gtnh-core.jar"}

func TestPlanModsRules(t *testing.T) {
	runPlanCases(t, []planCase{
		{name: "a new jar is added", archive: map[string]string{"a.jar": "A"}, pack: gtnh,
			want: []string{"add a.jar"}, managedW: []string{"a.jar"}},
		{name: "a managed jar the server changed is updated",
			archive: map[string]string{"a.jar": "A2"}, disk: map[string]string{"a.jar": "A1"},
			managed: map[string]string{"a.jar": "A1"}, pack: gtnh,
			want: []string{"update a.jar"}, managedW: []string{"a.jar"}},
		{name: "a jar an older launcher recorded without content counts as ours",
			archive: map[string]string{"a.jar": "A2"}, disk: map[string]string{"a.jar": "A1"},
			managed: map[string]string{"a.jar": ""}, pack: gtnh,
			want: []string{"update a.jar"}, managedW: []string{"a.jar"}},
		{name: "the player changed our copy: it is updated but theirs is kept",
			archive: map[string]string{"a.jar": "A2"}, disk: map[string]string{"a.jar": "patched"},
			managed: map[string]string{"a.jar": "A1"}, pack: gtnh,
			want: []string{"update a.jar (kept)"}, managedW: []string{"a.jar"}},
		{name: "dropped by the server and unchanged: removed",
			disk: map[string]string{"a.jar": "A1"}, managed: map[string]string{"a.jar": "A1"}, pack: gtnh,
			want: []string{"remove a.jar"}},
		{name: "dropped by the server but changed by the player: stays",
			disk: map[string]string{"a.jar": "patched"}, managed: map[string]string{"a.jar": "A1"}, pack: gtnh,
			want: []string{"keep a.jar"}},
		{name: "dropped and already gone: nothing to do",
			managed: map[string]string{"a.jar": "A1"}, pack: gtnh, want: []string{}},
		{name: "already in mods/ as the server has it: adopted",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar": "A"}, pack: gtnh,
			want: []string{"adopt a.jar"}, managedW: []string{"a.jar"}},
		{name: "managed and unchanged: nothing to do",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar": "A"},
			managed: map[string]string{"a.jar": "A"}, pack: gtnh,
			want: []string{}, managedW: []string{"a.jar"}},
		{name: "the player's own jar of that name makes way, kept",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar": "mine"}, pack: gtnh,
			want: []string{"replace a.jar (kept)"}, managedW: []string{"a.jar"}},
		{name: "unknown pack: a different jar of that name is left alone",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar": "mine"},
			want: []string{"skip a.jar ~a.jar"}},
		{name: "GTNH ships that name: skipped, and the file is GTNH's now",
			archive: map[string]string{"gtnh-core.jar": "server"}, disk: map[string]string{"gtnh-core.jar": "pack"},
			managed: map[string]string{"gtnh-core.jar": "server"}, pack: gtnh,
			want: []string{"skip gtnh-core.jar ~gtnh-core.jar"}},
		{name: "GTNH names are compared ignoring case",
			archive: map[string]string{"GTNH-Core.jar": "server"}, pack: gtnh,
			want: []string{"skip GTNH-Core.jar ~gtnh-core.jar"}},
		{name: "a managed jar GTNH ships now is GTNH's: not removed",
			disk: map[string]string{"gtnh-core.jar": "pack"}, managed: map[string]string{"gtnh-core.jar": "server"}, pack: gtnh,
			want: []string{}},
		{name: "disabled in Prism: updated in its disabled name",
			archive: map[string]string{"a.jar": "A2"}, disk: map[string]string{"a.jar.disabled": "A1"},
			managed: map[string]string{"a.jar": "A1"}, pack: gtnh,
			want: []string{"update a.jar @a.jar.disabled"}, managedW: []string{"a.jar"}},
		{name: "disabled and unchanged: stays disabled",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar.disabled": "A"},
			managed: map[string]string{"a.jar": "A"}, pack: gtnh,
			want: []string{}, managedW: []string{"a.jar"}},
		{name: "an enabled copy an older launcher put next to the disabled one goes",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar": "A", "a.jar.disabled": "A"},
			managed: map[string]string{"a.jar": ""}, pack: gtnh,
			want: []string{"remove a.jar"}, managedW: []string{"a.jar"}},
		{name: "dropped while disabled: the disabled copy goes too",
			disk: map[string]string{"a.jar.disabled": "A"}, managed: map[string]string{"a.jar": "A"}, pack: gtnh,
			want: []string{"remove a.jar @a.jar.disabled"}},
		{name: "the player's own disabled jar of that name stays; the server's is added",
			archive: map[string]string{"a.jar": "A"}, disk: map[string]string{"a.jar.disabled": "mine"}, pack: gtnh,
			want: []string{"add a.jar"}, managedW: []string{"a.jar"}},
		{name: "names on disk are found ignoring case",
			archive: map[string]string{"foo.jar": "A"}, disk: map[string]string{"Foo.jar": "A"}, pack: gtnh,
			want: []string{"adopt foo.jar @Foo.jar"}, managedW: []string{"foo.jar"}},
		{name: "renamed on the server: the old one goes, the new one comes",
			archive: map[string]string{"radio-1.1.jar": "R2"}, disk: map[string]string{"radio-1.0.jar": "R1"},
			managed: map[string]string{"radio-1.0.jar": "R1"}, pack: gtnh,
			want: []string{"add radio-1.1.jar", "remove radio-1.0.jar"}, managedW: []string{"radio-1.1.jar"}},
	})
}

func TestPlanModsSameModChecks(t *testing.T) {
	radio := map[string][]string{"server:radio-1.0.jar": {"radio"}, "radio-1.1.jar": {"radio"}}
	runPlanCases(t, []planCase{
		{name: "the same mod as a GTNH jar under another name is left out",
			archive: map[string]string{"radio-1.0.jar": "R"}, disk: map[string]string{"radio-1.1.jar": "G"},
			pack: []string{"radio-1.1.jar"}, ids: radio,
			want: []string{"skip radio-1.0.jar ~radio-1.1.jar"}},
		{name: "and the copy an earlier sync installed goes",
			archive: map[string]string{"radio-1.0.jar": "R"}, disk: map[string]string{"radio-1.0.jar": "R", "radio-1.1.jar": "G"},
			managed: map[string]string{"radio-1.0.jar": "R"}, pack: []string{"radio-1.1.jar"}, ids: radio, checkAll: true,
			want: []string{"skip radio-1.0.jar ~radio-1.1.jar -radio-1.0.jar"}},
		{name: "but not when the player changed that copy",
			archive: map[string]string{"radio-1.0.jar": "R"}, disk: map[string]string{"radio-1.0.jar": "patched", "radio-1.1.jar": "G"},
			managed: map[string]string{"radio-1.0.jar": "R"}, pack: []string{"radio-1.1.jar"}, ids: radio, checkAll: true,
			want: []string{"keep radio-1.0.jar", "skip radio-1.0.jar ~radio-1.1.jar"}},
		{name: "unchanged jars are only checked when asked to",
			archive: map[string]string{"radio-1.0.jar": "R"}, disk: map[string]string{"radio-1.0.jar": "R", "radio-1.1.jar": "G"},
			managed: map[string]string{"radio-1.0.jar": "R"}, pack: []string{"radio-1.1.jar"}, ids: radio,
			want: []string{}, managedW: []string{"radio-1.0.jar"}},
		{name: "the player's own copy of the same mod is set aside, kept",
			archive: map[string]string{"radio-1.1.jar": "R"}, disk: map[string]string{"radio-1.0.jar": "mine"},
			pack: gtnh, ids: map[string][]string{"server:radio-1.1.jar": {"radio"}, "radio-1.0.jar": {"radio"}},
			want: []string{"aside radio-1.0.jar ~radio-1.1.jar (kept)", "add radio-1.1.jar"}, managedW: []string{"radio-1.1.jar"}},
		{name: "without mcmod.info the name without its version decides",
			archive: map[string]string{"Radio-1.1.jar": "R"}, disk: map[string]string{"radio_1.0.jar": "mine"},
			pack: gtnh, ids: map[string][]string{},
			want: []string{"aside radio_1.0.jar ~Radio-1.1.jar (kept)", "add Radio-1.1.jar"}, managedW: []string{"Radio-1.1.jar"}},
		{name: "different mod ids are different mods, whatever the names",
			archive: map[string]string{"foo-2.jar": "F"}, disk: map[string]string{"foo-1.jar": "mine"},
			pack: gtnh, ids: map[string][]string{"server:foo-2.jar": {"foo"}, "foo-1.jar": {"bar"}},
			want: []string{"add foo-2.jar"}, managedW: []string{"foo-2.jar"}},
		{name: "a disabled jar of the player's isn't loaded: no clash",
			archive: map[string]string{"radio-1.1.jar": "R"}, disk: map[string]string{"radio-1.0.jar.disabled": "mine"},
			pack: gtnh, ids: map[string][]string{"server:radio-1.1.jar": {"radio"}, "radio-1.0.jar.disabled": {"radio"}},
			want: []string{"add radio-1.1.jar"}, managedW: []string{"radio-1.1.jar"}},
		{name: "unknown pack: the same mod already there keeps the server's out",
			archive: map[string]string{"radio-1.1.jar": "R"}, disk: map[string]string{"radio-1.0.jar": "?"},
			ids:  map[string][]string{"server:radio-1.1.jar": {"radio"}, "radio-1.0.jar": {"radio"}},
			want: []string{"skip radio-1.1.jar ~radio-1.0.jar"}},
	})
}

// The confirmation's preview sees mods/ as the pack plan will leave it.
func TestPlanModsAfterPack(t *testing.T) {
	next := map[string]pack.Fingerprint{".minecraft/mods/new.jar": fpOf("pack new"), ".minecraft/mods/core.jar": fpOf("core")}
	pl := &Plan{Actions: []Action{
		{Kind: Install, Path: ".minecraft/mods/new.jar"},
		{Kind: Remove, Path: ".minecraft/mods/old.jar"},
	}}
	c := planCase{archive: map[string]string{"new.jar": "server new", "old.jar": "server old"},
		disk: map[string]string{"old.jar": "pack old", "core.jar": "core"}}
	in := c.input()
	in.afterPack(pl, next)
	got := describeAll(planMods(in))
	if want := []string{"skip new.jar ~new.jar", "add old.jar"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("preview = %q, want %q (GTNH adds new.jar, removes old.jar)", got, want)
	}
}

func TestPackJarsNeedsTheInstanceToMatch(t *testing.T) {
	fps := map[string]pack.Fingerprint{}
	var files []string
	for i := range 10 {
		n := fmt.Sprintf("m%d.jar", i)
		fps[".minecraft/mods/"+n] = fpOf(n)
		files = append(files, n)
	}
	fps[".minecraft/mods/1.7.10/nested.jar"] = fpOf("n")
	fps[".minecraft/config/a.cfg"] = fpOf("c")
	if got := packJars(fps, files); len(got) != 10 || got["m3.jar"] != "m3.jar" {
		t.Errorf("packJars = %v, want the 10 jars at the top of mods/", got)
	}
	if got := packJars(fps, append(files[:7:7], "m8.jar.disabled")); got == nil {
		t.Error("80% of the pack's jars (one disabled) should be enough")
	}
	if got := packJars(fps, files[:7]); got != nil {
		t.Errorf("packJars with 70%% of the jars = %v, want nil (not this instance's pack)", got)
	}
	if got := packJars(nil, files); got != nil {
		t.Errorf("packJars without a pack = %v, want nil", got)
	}
}

func TestModInfoIDs(t *testing.T) {
	for _, c := range []struct{ name, info, want string }{
		{"list", `[{"modid":"NotEnoughItems","name":"NEI"},{"modid":"other"}]`, "[notenoughitems other]"},
		{"v2", `{"modListVersion":2,"modList":[{"modid":"gregtech"}]}`, "[gregtech]"},
		{"bom", "\xef\xbb\xbf[{\"modid\":\"x\"}]", "[x]"},
		{"placeholders", `[{"modid":"examplemod"},{"modid":"${modid}"},{"modid":""}]`, "[]"},
		{"broken", `[{"modid":"x",}]`, "[]"},
	} {
		if got := fmt.Sprint(modInfoIDs([]byte(c.info))); got != c.want {
			t.Errorf("%s: modInfoIDs = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestModNameKey(t *testing.T) {
	for in, want := range map[string]string{
		"NotEnoughItems-2.6.0-GTNH.jar":      "notenoughitems",
		"journeymap-1.7.10-5.2.6.jar":        "journeymap",
		"GT5-Unofficial-5.09.45.jar":         "gt5unofficial",
		"Baubles-1.7.10-1.0.1.10.jar":        "baubles",
		"radio_v1.2.jar.disabled":            "radio",
		"Foo-mc1.7.10-1.0.jar":               "foo",
		"gtnhvoiceradio-1.0.jar":             "gtnhvoiceradio",
		"1.0.jar":                            "10",
		"[1.7.10]Thing-1.2.jar":              "1710thing",
		"no-version-here.jar":                "noversionhere",
		"ae2stuff-0.9.1-GTNH.jar.gtnh-tmp":   "ae2stuff",
		"CodeChickenCore-1.4.10-GTNH.jar":    "codechickencore",
		"server+extra 2.jar":                 "serverextra",
		"hodgepodge-2.6.80-pre-forgepatches": "hodgepodge",
	} {
		if got := modNameKey(in); got != want {
			t.Errorf("modNameKey(%q) = %q, want %q", in, got, want)
		}
	}
}
