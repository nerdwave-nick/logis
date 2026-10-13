package theme

import (
	"math"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ported from Rust src/theme/colour.rs tests.

func testPalette() palette {
	return palette{
		"brass": {render.Hex(0xd4a017)},
		"soot":  {render.Hex(0x170e0b), render.Hex(0x0b0807).WithAlpha(128.0 / 255)},
	}
}

func colourOf(t *testing.T, text string) (render.Color, error) {
	t.Helper()
	c, ok := parseColour(text)
	if !ok {
		t.Fatalf("%q is no colour", text)
	}
	return c.resolve(testPalette())
}

func mustColour(t *testing.T, text string) render.Color {
	t.Helper()
	c, err := colourOf(t, text)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return c
}

func TestHexColoursCarryAnOptionalAlphaByte(t *testing.T) {
	if got := mustColour(t, "#ff8000"); got != render.Hex(0xff8000) {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "#ff800080"); !near(got.A, 128.0/255) {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "transparent"); got != render.Transparent {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "#FFFFFF/0.5"); got != render.Hex(0xffffff).WithAlpha(0.5) {
		t.Errorf("upper case hex: %v", got)
	}
}

func TestPaletteNamesAndIndicesResolve(t *testing.T) {
	if got := mustColour(t, "brass"); got != render.Hex(0xd4a017) {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "soot"); got != render.Hex(0x170e0b) {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "soot.1"); got.A != 128.0/255 {
		t.Errorf("%v", got)
	}
	// Rust's usize parse takes a leading '+' and leading zeros.
	for _, text := range []string{"soot.+1", "soot.01"} {
		if got := mustColour(t, text); got.A != 128.0/255 {
			t.Errorf("%s: %v", text, got)
		}
	}
	if _, err := colourOf(t, "soot.2"); err == nil || err.Error() != "'soot' has 2 colour(s); 'soot.2' names none" {
		t.Errorf("%v", err)
	}
	if _, err := colourOf(t, "embr"); err == nil || err.Error() != "no colour named 'embr' in the palette" {
		t.Errorf("%v", err)
	}
}

func TestAnAlphaSuffixMultipliesTheAlpha(t *testing.T) {
	if got := mustColour(t, "brass/0.5"); got != render.Hex(0xd4a017).WithAlpha(0.5) {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "soot.1/0.5"); math.Abs(float64(got.A-64.0/255)) > 1e-3 {
		t.Errorf("%v", got)
	}
	if got := mustColour(t, "#ffffff/0"); got != render.Hex(0xffffff).WithAlpha(0) {
		t.Errorf("%v", got)
	}
	for _, text := range []string{"brass/1.00", "brass/01", "transparent/0.5"} {
		mustColour(t, text)
	}
}

func TestMalformedColoursAreNoColours(t *testing.T) {
	for _, bad := range []string{
		"", "#fff", "#12345", "#gggggg", "Brass", "1brass", "brass/", "brass/1.5",
		"brass/-0.1", "brass/.5", "soot.x", "brass/0.5/0.5", "#ff8000/abc",
		"soot.-1", "soot.", "soot.++1", "#+12345", "brass/+1", "brass/1e0",
	} {
		if _, ok := parseColour(bad); ok {
			t.Errorf("%q reads as a colour", bad)
		}
	}
}

func TestAPaintResolvesEveryStopAndNamesTheBadOne(t *testing.T) {
	var p paint
	for _, s := range []string{"brass", "soot.0", "#000000"} {
		c, _ := parseColour(s)
		p = append(p, c)
	}
	if got, err := p.resolve(testPalette()); err != nil || len(got) != 3 {
		t.Errorf("%v %v", got, err)
	}
	brass, _ := parseColour("brass")
	embr, _ := parseColour("embr")
	if _, err := (paint{brass, embr}).resolve(testPalette()); err == nil || !strings.HasPrefix(err.Error(), "[1]: ") {
		t.Errorf("%v", err)
	}
}

func TestAPaletteOverAnotherKeepsItsOwnNames(t *testing.T) {
	mine := palette{"brass": {render.Hex(0)}}
	both := mine.over(testPalette())
	if len(both) != 2 {
		t.Errorf("%d names", len(both))
	}
	brass, _ := parseColour("brass")
	if got, _ := brass.resolve(both); got != render.Hex(0) {
		t.Errorf("%v", got)
	}
	if len(testPalette().over(nil)) != 2 {
		t.Error("over nothing")
	}
}

func TestPaletteNamesAreLowercaseLetterFirst(t *testing.T) {
	for name, valid := range map[string]bool{"brass": true, "band-2": true, "a_b": true, "Brass": false, "2x": false, "": false, "bräss": false} {
		if isName(name) != valid {
			t.Errorf("%q: %v", name, !valid)
		}
	}
}
