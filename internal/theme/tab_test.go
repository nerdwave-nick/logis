package theme

import (
	"path/filepath"
	"testing"
)

// The tab element (feat/logis-go 8cba2fc9, new beside Rust v1): the
// corners of plain logis's tab plates; the rest of their look comes from
// the elements they echo (the selection, the panel, the prompt's label,
// the badges).

func TestTheBundledThemesRoundTheirTabPlatesAsTheOwnerChose(t *testing.T) {
	th, _ := themesDir(t, nil)
	for name, want := range map[string]float32{"forge-crimson-brass": 9, "cogitator-phosphor": 4} {
		if got := load(t, th, name).Tab.Radius; got != want {
			t.Errorf("%s: tab.radius %v, want %v", name, got, want)
		}
	}
}

func TestATabRadiusIsALengthAndExtendsKeepIt(t *testing.T) {
	th, dir := themesDir(t, map[string]string{
		"round": `{"tab": {"radius": 12}}`,
		"heir":  `{"extends": "round", "panel": {"radius": 3}}`,
		"bad":   `{"tab": {"radius": -1}}`,
	})
	if got := load(t, th, "heir").Tab.Radius; got != 12 {
		t.Errorf("heir: %v", got)
	}
	e := refusal(t, th, "bad")
	if want := "tab.radius: -1 is no length (0 or more)"; e.Source != filepath.Join(dir, "bad.jsonc") || e.Problem != want {
		t.Errorf("got %v, want %s", e, want)
	}
}
