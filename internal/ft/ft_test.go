package ft_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/ft"
)

func font(name string) string { return filepath.Join("..", "..", "tests", "fonts", name) }

// open maps a pinned font and opens its face 0; both go when the test ends.
func open(t *testing.T, name string) *ft.Face {
	t.Helper()
	lib, err := ft.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lib.Close)
	m, err := ft.MapFile(font(name))
	if err != nil {
		t.Fatal(err)
	}
	face, err := lib.Open(m, 0)
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { face.Close(); m.Close() })
	return face
}

// Glyph ids of the pinned Noto Sans subset (cmap: 'H' and 'o').
func glyph(t *testing.T, f *ft.Face, r rune) uint32 {
	t.Helper()
	id := f.CharIndex(r)
	if id == 0 {
		t.Fatalf("%q has no glyph", r)
	}
	return id
}

// rows is the bitmap's coverage rows, top to bottom.
func rows(b *ft.Bitmap) [][]byte {
	var out [][]byte
	for y := range b.Height {
		out = append(out, b.Pixels[y*b.Width:(y+1)*b.Width])
	}
	return out
}

func TestAGlyphIsACoverageMaskAboveTheBaseline(t *testing.T) {
	f := open(t, "NotoSans-Regular.ttf")
	b, err := f.Render(glyph(t, f, 'H'), 16, 0, ft.Light)
	if err != nil {
		t.Fatal(err)
	}
	// Noto Sans caps are 714/1000 em: 11.4 px at 16 px, and H stands on
	// the baseline with a small side bearing.
	if b.Color || b.Width < 7 || b.Width > 13 || b.Height < 11 || b.Height > 13 {
		t.Fatalf("H is %dx%d (colour %v)", b.Width, b.Height, b.Color)
	}
	if b.Top != b.Height || b.Left < 0 || b.Left > 3 {
		t.Errorf("H at left %d, top %d, height %d: not on the baseline", b.Left, b.Top, b.Height)
	}
	if len(b.Pixels) != b.Width*b.Height {
		t.Errorf("%d bytes for %dx%d", len(b.Pixels), b.Width, b.Height)
	}
}

// Hinting snaps horizontal edges to the pixel grid (all targets hint
// vertically): at 20.8 px the cap height is 14.85 px, so an unhinted H has
// a partly covered top row and a hinted one has none.
func TestHintingSnapsTheCapHeightToWholePixels(t *testing.T) {
	f := open(t, "NotoSans-Regular.ttf")
	id := glyph(t, f, 'H')
	partial := func(target ft.Target) int {
		b, err := f.Render(id, 20.8, 0, target)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range rows(b)[0] {
			if c > 0 && c < 200 {
				n++
			}
		}
		return n
	}
	if n := partial(ft.Unhinted); n == 0 {
		t.Errorf("unhinted: the top row is solid")
	}
	for _, target := range []ft.Target{ft.Light, ft.Normal, ft.LCD} {
		if n := partial(target); n > 2 {
			t.Errorf("%v: %d partly covered pixels in the top row", target, n)
		}
	}
}

func TestASubpixelOffsetShiftsTheCoverage(t *testing.T) {
	f := open(t, "NotoSans-Regular.ttf")
	id := glyph(t, f, 'H')
	a, err := f.Render(id, 16, 0, ft.Light)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Render(id, 16, 0.5, ft.Light)
	if err != nil {
		t.Fatal(err)
	}
	if a.Height != b.Height || string(a.Pixels) == string(b.Pixels) && a.Left == b.Left {
		t.Errorf("a half-pixel offset changed nothing")
	}
}

func TestAColourEmojiIsItsStrikesBitmap(t *testing.T) {
	f := open(t, "NotoColorEmoji.ttf")
	if !f.HasColor() {
		t.Fatal("Noto Color Emoji has no colour")
	}
	b, err := f.Render(glyph(t, f, '🔥'), 16, 0, ft.Light)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Color || b.PPEM != 109 || b.Width < 100 || b.Height < 100 {
		t.Fatalf("fire is %dx%d at %d ppem (colour %v)", b.Width, b.Height, b.PPEM, b.Color)
	}
	if len(b.Pixels) != b.Width*b.Height*4 {
		t.Fatalf("%d bytes for %dx%d RGBA", len(b.Pixels), b.Width, b.Height)
	}
	orange := 0
	for i := 0; i < len(b.Pixels); i += 4 {
		p := b.Pixels[i : i+4]
		if p[3] == 255 && p[0] > 200 && p[2] < 100 {
			orange++
		}
		if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
			t.Fatalf("pixel %v is not premultiplied", p)
		}
	}
	if orange < 1000 {
		t.Errorf("only %d orange pixels in the fire", orange)
	}
}

func TestMetricsComeFromTheFontsTables(t *testing.T) {
	m := open(t, "NotoSans-Regular.ttf").Metrics()
	// Noto Sans: 1000 units, hhea 1069/-293, OS/2 weight 400.
	if m.UnitsPerEm != 1000 || m.Ascent != 1069 || m.Descent != 293 || m.Weight != 400 {
		t.Errorf("metrics %+v", m)
	}
	if w := open(t, "NotoSerif-SemiBold.ttf").Metrics().Weight; w != 600 {
		t.Errorf("Noto Serif SemiBold weighs %d", w)
	}
}

func TestAFileThatIsNoFontIsRefused(t *testing.T) {
	lib, err := ft.New()
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	m, err := ft.MapFile(font("corpus.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if f, err := lib.Open(m, 0); err == nil {
		f.Close()
		t.Fatal("a text file opened as a font")
	}
	if _, err := ft.MapFile(font("no-such-font.ttf")); err == nil || !strings.Contains(err.Error(), "no-such-font.ttf") {
		t.Errorf("mapping a missing file: %v", err)
	}
}

func TestClosingTwiceIsHarmless(t *testing.T) {
	f := open(t, "NotoSans-Regular.ttf")
	f.Close()
	f.Close()
}
