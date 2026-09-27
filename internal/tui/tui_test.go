package tui

import (
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
)

func TestDefaultTargetNeverDowngrades(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for installed, want := range map[string]string{
		"2.8.1":      "2.8.4",      // behind the stable: take the stable
		"2.8.4":      "2.8.4",      // on the stable: stay (repair / custom-mods sync)
		"2.9.0-RC-1": "2.9.0-RC-1", // ahead of the stable: never preselect a downgrade
	} {
		if got := defaultTarget(m, installed); got != want {
			t.Errorf("installed %s: default %s, want %s", installed, got, want)
		}
	}
}
