package theme

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ported from Rust src/theme/tests.rs: bundled and user files, extends,
// the default as the last base, and errors that say where.

// themesDir is a scratch themes directory holding name → text.
func themesDir(t *testing.T, files map[string]string) (Themes, string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".jsonc"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return NewThemes(dir), dir
}

func load(t *testing.T, th Themes, name string) Theme {
	t.Helper()
	theme, err := th.Load(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return theme
}

func refusal(t *testing.T, th Themes, name string) *Error {
	t.Helper()
	_, err := th.Load(name)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: want a *theme.Error, got %v", name, err)
	}
	return e
}

func phosphor(t *testing.T) Theme { return load(t, Themes{}, "cogitator-phosphor") }

func TestBothBundledThemesLoad(t *testing.T) {
	for _, name := range Bundled() {
		theme := load(t, Themes{}, name)
		if theme.Name != name || theme.Dir != "" {
			t.Errorf("%s: %q %q", name, theme.Name, theme.Dir)
		}
	}
	if got := Bundled(); !reflect.DeepEqual(got, []string{"forge-crimson-brass", "cogitator-phosphor"}) {
		t.Errorf("%v", got)
	}
}

func TestTheDefaultIsForgeWithTheDecidedGeometryAndMotifs(t *testing.T) {
	forge := Default()
	switch {
	case forge.Name != "forge-crimson-brass":
		t.Errorf("%q", forge.Name)
	case forge.Window.Width != 820 || forge.Window.Height != 520 || forge.Window.Rows != 9:
		t.Errorf("%+v", forge.Window)
	case !forge.HasRivets || !forge.Prompt.HasSigil || forge.Prompt.Sigil.Image != "gear":
		t.Error("corner rivets and the gear")
	case !forge.Border.HasHighlight || forge.Glow.Pulse <= 0:
		t.Error("a travelling highlight and a pulsing glow")
	case forge.Effects.Flicker <= 0 || forge.Effects.Flicker >= 0.05:
		t.Error("subtle flicker")
	case len(forge.Border.Paint) != 3:
		t.Error("brass, ember, crimson")
	}
	if f := forge.Faces[forge.Row.Font.Face].Families; !reflect.DeepEqual(f, []string{"IBM Plex Mono", "monospace"}) {
		t.Errorf("body %v", f)
	}
	if f := forge.Faces[forge.Prompt.Font.Face].Families; !reflect.DeepEqual(f, []string{"Cinzel", "serif"}) {
		t.Errorf("label %v", f)
	}
}

func TestPhosphorIsAGreenCRTWithCurvatureBloomAndInverseSelection(t *testing.T) {
	p := phosphor(t)
	if p.Effects.Curvature <= 0 || p.Effects.Bloom <= 0 || p.HasRivets || p.Prompt.HasSigil {
		t.Errorf("%+v rivets %v sigil %v", p.Effects, p.HasRivets, p.Prompt.HasSigil)
	}
	if f := p.Faces[p.Row.Font.Face].Families; !reflect.DeepEqual(f, []string{"VT323", "Terminus", "monospace"}) {
		t.Errorf("%v", f)
	}
	// Inverse: the band is the bright phosphor, the text on it dark.
	if band, text := p.Selection.Band[0], p.Selection.Columns[0]; band.G <= 0.9 || text.G >= 0.1 {
		t.Errorf("%v %v", band, text)
	}
	if !p.Match.Underline {
		t.Error("underlined matches")
	}
}

func TestAUserFileOverridesABundledThemeOfItsName(t *testing.T) {
	th, dir := themesDir(t, map[string]string{
		"forge-crimson-brass": `{"window": {"rows": 4}, "palette": {"brass": "#000000"}}`,
	})
	theme := load(t, th, "forge-crimson-brass")
	if theme.Window.Rows != 4 || theme.Prompt.Color != render.Hex(0) || theme.Dir != dir || theme.Window.Width != 820 {
		t.Errorf("rows %d prompt %v dir %q width %v", theme.Window.Rows, theme.Prompt.Color, theme.Dir, theme.Window.Width)
	}
}

func TestValuesNotGivenFallBackToTheDefault(t *testing.T) {
	th, _ := themesDir(t, map[string]string{"sparse": "// nothing at all\n"})
	theme := load(t, th, "sparse")
	forge := Default()
	if theme.Name != "sparse" || !reflect.DeepEqual(theme.Border, forge.Border) || !reflect.DeepEqual(theme.Faces, forge.Faces) {
		t.Errorf("%+v", theme)
	}
}

func TestAThemeExtendsAnotherAndOverridesItsKeys(t *testing.T) {
	th, _ := themesDir(t, map[string]string{
		"amber": `{"extends": "cogitator-phosphor", "palette": {"phosphor": "#ffb000"}}`,
	})
	amber := load(t, th, "amber")
	if amber.Selection.Band[0] != render.Hex(0xffb000) {
		t.Errorf("%v", amber.Selection.Band)
	}
	if amber.Effects != phosphor(t).Effects || amber.HasRivets {
		t.Errorf("%+v, rivets %v: phosphor's 'off' carries over", amber.Effects, amber.HasRivets)
	}
}

func TestAThemeExtendingItsOwnNameAmendsTheBundledOne(t *testing.T) {
	th, _ := themesDir(t, map[string]string{
		"cogitator-phosphor": `{"extends": "cogitator-phosphor", "effects": {"curvature": 0}}`,
	})
	theme := load(t, th, "cogitator-phosphor")
	if theme.Effects.Curvature != 0 || theme.Effects.Bloom <= 0 {
		t.Errorf("%+v", theme.Effects)
	}
}

func TestExtendsGoesOneLevelAndNeverInACircle(t *testing.T) {
	th, _ := themesDir(t, map[string]string{
		"a": `{"extends": "b"}`, "b": `{"extends": "a"}`,
		"c": `{"extends": "d"}`, "d": `{"extends": "cogitator-phosphor"}`,
		"e": `{"extends": "e"}`, "f": `{"extends": "nowhere"}`,
	})
	for name, want := range map[string]string{
		"a": `"extends" goes round in a circle: 'a' extends 'b', which extends 'a'`,
		"c": "'d' extends 'cogitator-phosphor' in turn; a theme extends at most one level",
		"e": `"extends": "e" names this theme itself, and no bundled theme bears that name`,
		"f": `"extends": no such theme is bundled (forge-crimson-brass, cogitator-phosphor) nor in `,
	} {
		if e := refusal(t, th, name); !strings.HasPrefix(e.Problem, want) || !strings.HasSuffix(e.Source, name+".jsonc") {
			t.Errorf("%s: %v", name, e)
		}
	}
}

func TestAnUnknownThemeNamesWhatThereIs(t *testing.T) {
	th, dir := themesDir(t, nil)
	e := refusal(t, th, "purple")
	if e.Source != "theme 'purple'" || e.Problem != "no such theme is bundled (forge-crimson-brass, cogitator-phosphor) nor in "+dir {
		t.Errorf("%v", e)
	}
	if e := refusal(t, Themes{}, "purple"); e.Error() != "theme 'purple': no such theme is bundled (forge-crimson-brass, cogitator-phosphor)" {
		t.Errorf("without a themes directory: %v", e)
	}
}

func TestAMalformedFileIsReportedWithLineAndColumn(t *testing.T) {
	th, dir := themesDir(t, map[string]string{"bad": "{\n  \"glow\": {\"radus\": 3}\n}"})
	e := refusal(t, th, "bad")
	if e.Source != filepath.Join(dir, "bad.jsonc")+":2:18" || !strings.HasPrefix(e.Problem, "unknown field `radus`") {
		t.Errorf("%v", e)
	}
}

func TestAnUnreadableFileSaysWhy(t *testing.T) {
	th, dir := themesDir(t, nil)
	if err := os.Mkdir(filepath.Join(dir, "odd.jsonc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if e := refusal(t, th, "odd"); e.Source != filepath.Join(dir, "odd.jsonc") || e.Problem == "" {
		t.Errorf("%v", e)
	}
}

// A bad reference or value names its key, in Rust logis v1's words.
func TestABadReferenceOrValueNamesItsKey(t *testing.T) {
	cases := map[string]string{
		`{"border": {"paint": ["brass", "embr"]}}`:                            "border.paint[1]: no colour named 'embr' in the palette",
		`{"effects": {"bloom": 2}}`:                                           "effects.bloom: 2 is out of range (0 to 1)",
		`{"row": {"font": "heading"}}`:                                        "row.font: no font role 'heading' in fonts",
		`{"window": {"rows": 0}}`:                                             "window.rows: show at least one row",
		`{"rule": {"dash": [1, -2]}}`:                                         "rule.dash[1]: -2 is no length (0 or more)",
		`{"rule": {"dash": [0, 1]}}`:                                          "rule.dash[0]: must be more than 0",
		`{"rule": {"dash": [1e39, 2]}}`:                                       "rule.dash[0]: inf is no length (0 or more)",
		`{"row": {"columns": []}}`:                                            "row.columns: give at least one colour",
		`{"glow": {"pulse": 2}}`:                                              "glow.pulse: 2 is out of range (0 to 1)",
		`{"glow": {"radius": -1}}`:                                            "glow.radius: -1 is no length (0 or more)",
		`{"glow": {"radius": -0.1}}`:                                          "glow.radius: -0.1 is no length (0 or more)",
		`{"glow": {"radius": 1e39}}`:                                          "glow.radius: inf is no length (0 or more)",
		`{"glow": {"color": "nope"}}`:                                         "glow.color: no colour named 'nope' in the palette",
		`{"window": {"width": 0}}`:                                            "window.width: must be more than 0",
		`{"fonts": {"body": {"weight": 50}}}`:                                 "fonts.body.weight: 50 is no weight (100 to 900)",
		`{"fonts": {"body": {"tracking": 2}}}`:                                "fonts.body.tracking: 2 is out of range (-0.5 to 1)",
		`{"match": {"weight": 1000}}`:                                         "match.weight: 1000 is no weight (100 to 900)",
		`{"prompt": {"sigil": {"size": 0}}}`:                                  "prompt.sigil.size: must be more than 0",
		`{"fonts": {"heading": {"size": 3}}, "row": {"font": "heading"}}`:     "fonts.heading.family: missing, and no base theme gives it",
		`{"fonts": {"heading": {"family": "X"}}, "row": {"font": "heading"}}`: "fonts.heading.size: missing, and no base theme gives it",
		`{"selection": {"columns": ["brass", "nope"]}}`:                       "selection.columns[1]: no colour named 'nope' in the palette",
		`{"rule": {"ornament": {"fill": "nope"}}}`:                            "rule.ornament.fill: no colour named 'nope' in the palette",
		`{"decorations": {"rivets": {"top": ["x.9"]}}}`:                       "decorations.rivets.top[0]: no colour named 'x' in the palette",
		`{"decorations": {"rivets": {"top": "soot.9"}}}`:                      "decorations.rivets.top[0]: 'soot' has 2 colour(s); 'soot.9' names none",
		`{"row": {"column-max": 1.5}}`:                                        "row.column-max: 1.5 is no share of the row (more than 0, at most 1)",
		`{"row": {"column-max": 0}}`:                                          "row.column-max: 0 is no share of the row (more than 0, at most 1)",
		`{"row": {"column-gap": -2}}`:                                         "row.column-gap: -2 is no length (0 or more)",
	}
	for text, want := range cases {
		th, dir := themesDir(t, map[string]string{"t": text})
		e := refusal(t, th, "t")
		if e.Source != filepath.Join(dir, "t.jsonc") || e.Problem != want {
			t.Errorf("%s:\n got %v\nwant %s", text, e, want)
		}
	}
}

func TestRowsSpaceTheirColumnsAndCapAllButTheLast(t *testing.T) {
	forge := Default()
	if forge.Row.ColumnGap != 32 || forge.Row.ColumnMax != 0.4 {
		t.Errorf("%+v", forge.Row)
	}
	p := phosphor(t)
	if p.Row.ColumnGap != 28 || p.Row.ColumnMax != 0.4 {
		t.Errorf("phosphor: its own gap for its larger face, the default's cap: %+v", p.Row)
	}
}

func TestReduceMotionStillsEveryContinuousEffect(t *testing.T) {
	forge := Default()
	if !forge.IsContinuous() {
		t.Fatal("forge moves on its own")
	}
	still := forge.Stilled()
	if still.IsContinuous() || still.Shimmers() || still.Border.Highlight.Period != 0 {
		t.Errorf("%+v", still)
	}
	if still.Effects.Scanlines != forge.Effects.Scanlines {
		t.Error("static effects stay")
	}
}

// Faces are interned in Rust's order: the body's first, the row's at the
// match weight, then the roles as the elements name them.
func TestFacesAreInternedOnceInTheResolversOrder(t *testing.T) {
	forge := Default()
	if len(forge.Faces) != 3 || forge.Match.Face != 1 || forge.Faces[1].Weight != 600 || forge.Faces[2].Families[0] != "Cinzel" {
		t.Errorf("%+v match %d", forge.Faces, forge.Match.Face)
	}
}
