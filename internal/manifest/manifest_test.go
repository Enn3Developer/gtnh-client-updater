package manifest

import "testing"

const sample = `{
  "2.8.4": {"title": "Stable release", "releaseDate": "2025/12/23",
    "mmc": {"java8Url": "https://downloads.gtnewhorizons.com/a_8.zip",
            "java17_2XUrl": "https://downloads.gtnewhorizons.com/a_17.zip"}},
  "2.9.0-RC-1": {"title": "Beta release", "releaseDate": "2026/09/24",
    "mmc": {"java17_2XUrl": "https://downloads.gtnewhorizons.com/b_17.zip"}},
  "April fools 2025": {"title": "Joke", "releaseDate": "2025/04/01",
    "mmc": {"java17_2XUrl": "https://evil.example.com/x.zip"}},
  "server-only": {"title": "Beta release", "releaseDate": "2026/01/01",
    "server": {"java8Url": "https://downloads.gtnewhorizons.com/s.zip"}}
}`

func TestParseSortsAndSkipsNonPrism(t *testing.T) {
	m, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range m.Releases {
		got = append(got, r.Version)
	}
	want := []string{"2.9.0-RC-1", "2.8.4", "April fools 2025"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if r, _ := m.Find("2.8.4"); !r.Stable() {
		t.Error("2.8.4 should be stable")
	}
}

func TestParseWrapper(t *testing.T) {
	m, err := Parse([]byte(`{"versions": ` + sample + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Find("2.8.4"); !ok {
		t.Fatal("wrapped manifest lost 2.8.4")
	}
}

func TestURLPinsHostAndFlavor(t *testing.T) {
	m, _ := Parse([]byte(sample))
	rc, _ := m.Find("2.9.0-RC-1")
	if _, err := rc.URL(Java8); err == nil {
		t.Error("RC-1 has no Java 8 pack, want error")
	}
	if u, err := rc.URL(Java17); err != nil || u == "" {
		t.Errorf("RC-1 Java 17 URL: %q, %v", u, err)
	}
	joke, _ := m.Find("April fools 2025")
	if _, err := joke.URL(Java17); err == nil {
		t.Error("foreign host accepted")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{`[]`, `{"a": 1}`, `{}`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%s) succeeded, want error", in)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{"April fools 2025", "2.8.0-beta-4", "2.8.0-rc-1", "2.8.0-rc-2", "2.8.0",
		"2.8.2-pre1", "2.8.2", "2.8.10", "2.9.0-beta-3", "2.9.0-RC-1", "2.9.0"}
	for i := range ordered {
		for j := range ordered {
			got := CompareVersions(ordered[i], ordered[j])
			want := cmpInt(i, j)
			if got != want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestSameDayOrderedByVersion(t *testing.T) {
	m, err := Parse([]byte(`{
	  "2.9.0-beta-3": {"title":"Beta release","releaseDate":"2026/09/06","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/06","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Releases[0].Version != "2.9.0-RC-1" {
		t.Errorf("newest is %s, want 2.9.0-RC-1", m.Releases[0].Version)
	}
}
