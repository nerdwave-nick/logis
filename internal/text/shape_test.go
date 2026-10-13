package text_test

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/text"
)

func fontName(f *text.Fonts, id text.FontID) string {
	return filepath.Base(f.Loaded()[id].Path)
}

func TestARunAdvancesInWholePixels(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, size := range []float32{16, 20.8, 32} {
		line := f.Shape("Hail", 0, size, 0)
		if len(line.Glyphs) != 4 {
			t.Fatalf("@%g: %d glyphs", size, len(line.Glyphs))
		}
		pen := float32(0)
		for i, g := range line.Glyphs {
			if g.Font != 0 || g.ID == 0 || g.Cluster != i || g.X != pen || g.Y != 0 {
				t.Errorf("@%g: glyph %d is %+v (pen %g)", size, i, g, pen)
			}
			if g.Advance != float32(math.Round(float64(g.Advance))) || g.Advance <= 0 {
				t.Errorf("@%g: advance %g is not a whole pixel", size, g.Advance)
			}
			pen += g.Advance
		}
		if line.Width != pen || line.Width < 1.5*size || line.Width > 2.5*size {
			t.Errorf("@%g: width %g, pen %g", size, line.Width, pen)
		}
	}
}

func TestPairsAreKerned(t *testing.T) {
	f := pinned(t, "sans-serif")
	pair := f.Shape("AV", 0, 32, 0).Width
	apart := f.Shape("A", 0, 32, 0).Width + f.Shape("V", 0, 32, 0).Width
	if pair >= apart {
		t.Errorf("AV is %g px wide, A and V apart %g", pair, apart)
	}
}

func TestEachCharacterFallsBackToTheFirstLoadedFaceThatCoversIt(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Shape("a機🔥", 0, 16, 0)
	want := []string{"NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf", "NotoColorEmoji.ttf"}
	clusters := []int{0, 1, 4}
	if len(line.Glyphs) != 3 {
		t.Fatalf("%d glyphs", len(line.Glyphs))
	}
	for i, g := range line.Glyphs {
		if fontName(f, g.Font) != want[i] || g.Cluster != clusters[i] || g.ID == 0 {
			t.Errorf("glyph %d: %+v in %s", i, g, fontName(f, g.Font))
		}
	}
	// The CJK and emoji faces are taller than Noto Sans: the line is too.
	if latin := f.Shape("a", 0, 16, 0); line.Ascent <= latin.Ascent || line.Descent < latin.Descent {
		t.Errorf("ascent/descent %g/%g, Latin only %g/%g", line.Ascent, line.Descent, latin.Ascent, latin.Descent)
	}
}

// Default ignorables are hidden as HarfBuzz hides them: the font's space
// glyph with no advance, in the cluster of the grapheme they belong to.
func TestIgnorablesAreHidden(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Shape("a\u200Db\uFE0F", 0, 16, 0)
	space := f.Shape(" ", 0, 16, 0).Glyphs[0].ID
	clusters := []int{0, 0, 4, 4}
	if len(line.Glyphs) != 4 {
		t.Fatalf("glyphs %+v", line.Glyphs)
	}
	for i, g := range line.Glyphs {
		hidden := i%2 == 1
		if g.Cluster != clusters[i] || hidden != (g.ID == space && g.Advance == 0) {
			t.Errorf("glyph %d: %+v", i, g)
		}
	}
}

func TestACharacterNoFaceCoversIsTheMissingGlyphOfItsFace(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Shape("\U0010FFFD", 0, 16, 0)
	if len(line.Glyphs) != 1 || line.Glyphs[0].ID != 0 || line.Glyphs[0].Font != 0 || line.Glyphs[0].Advance <= 0 {
		t.Errorf("glyphs %+v", line.Glyphs)
	}
}

func TestTrackingWidensEveryAdvanceByItsEms(t *testing.T) {
	f := pinned(t, "sans-serif")
	plain := f.Shape("II", 0, 13, 0)
	tracked := f.Shape("II", 0, 13, 0.2)
	for i := range plain.Glyphs {
		// round(13 × (advance/em + 0.2)): within a pixel of 2.6 more.
		if d := tracked.Glyphs[i].Advance - plain.Glyphs[i].Advance; d < 2 || d > 4 {
			t.Errorf("glyph %d: %g wider", i, d)
		}
	}
}

func TestAThemeFaceShapesWithItsOwnFont(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces(forgeFaces()); err != nil {
		t.Fatal(err)
	}
	line := f.Shape("rite", 1, 16, 0)
	for _, g := range line.Glyphs {
		if fontName(f, g.Font) != "NotoSerif-SemiBold.ttf" {
			t.Errorf("glyph %+v in %s", g, fontName(f, g.Font))
		}
	}
	// Monospace: every advance is the same.
	mono := f.Shape("il", 0, 16, 0)
	if mono.Glyphs[0].Advance != mono.Glyphs[1].Advance {
		t.Errorf("monospace advances %+v", mono.Glyphs)
	}
}

func TestAGlyphImageIsItsCoverageOnTheBaseline(t *testing.T) {
	f := pinned(t, "sans-serif")
	g := f.Shape("H", 0, 16, 0).Glyphs[0]
	img, err := f.Glyph(text.GlyphKey{Font: g.Font, ID: g.ID, Size: 16})
	if err != nil {
		t.Fatal(err)
	}
	if img.Color || img.Width < 7 || img.Height < 11 || img.Top != img.Height || len(img.Pixels) != img.Width*img.Height {
		t.Errorf("H: %dx%d at %d,%d colour %v, %d bytes", img.Width, img.Height, img.Left, img.Top, img.Color, len(img.Pixels))
	}
	space := f.Shape(" ", 0, 16, 0).Glyphs[0]
	if img, err := f.Glyph(text.GlyphKey{Font: space.Font, ID: space.ID, Size: 16}); err != nil || img.Width != 0 {
		t.Errorf("a space: %+v, %v", img, err)
	}
}

func TestAColourEmojiIsScaledToTheEm(t *testing.T) {
	f := pinned(t, "sans-serif")
	g := f.Shape("🔥", 0, 16, 0).Glyphs[0]
	img, err := f.Glyph(text.GlyphKey{Font: g.Font, ID: g.ID, Size: 16})
	if err != nil {
		t.Fatal(err)
	}
	// Noto Color Emoji's 136×128 bitmaps at 109 ppem, scaled to 16 px and
	// truncated as swash does: 19×18.
	if !img.Color || img.Width != 19 || img.Height != 18 || len(img.Pixels) != 19*18*4 {
		t.Fatalf("fire: %dx%d colour %v, %d bytes", img.Width, img.Height, img.Color, len(img.Pixels))
	}
	orange := 0
	for i := 0; i < len(img.Pixels); i += 4 {
		p := img.Pixels[i : i+4]
		if p[3] > 200 && p[0] > 200 && p[2] < 120 {
			orange++
		}
	}
	if orange < 30 {
		t.Errorf("only %d orange pixels", orange)
	}
	if img.Top < 12 || img.Top > 16 || img.Left < -1 || img.Left > 2 {
		t.Errorf("fire at %d,%d", img.Left, img.Top)
	}
}

func TestTheHintingTargetChangesTheImage(t *testing.T) {
	f := pinned(t, "sans-serif")
	g := f.Shape("e", 0, 20.8, 0).Glyphs[0]
	key := text.GlyphKey{Font: g.Font, ID: g.ID, Size: 20.8}
	hinted, err := f.Glyph(key)
	if err != nil {
		t.Fatal(err)
	}
	f.SetHinting(text.Unhinted)
	plain, err := f.Glyph(key)
	if err != nil {
		t.Fatal(err)
	}
	if string(hinted.Pixels) == string(plain.Pixels) {
		t.Error("hinting changed nothing")
	}
}

func TestAColourEmojiAdvancesLikeAnyGlyph(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Shape("🔥🔥", 0, 16, 0)
	// Noto Color Emoji advances 2550/2048 em: 20 px at 16 px.
	if line.Width != 40 || line.Ascent <= 0 || line.Ascent > 32 || line.Descent > 16 {
		t.Errorf("width %g, ascent %g, descent %g", line.Width, line.Ascent, line.Descent)
	}
}
