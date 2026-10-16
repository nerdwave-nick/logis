package render_test

import (
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// Ports of the line features of Rust tests/text.rs, tests/faces.rs and
// src/render/measure.rs: columns, the ellipsis, colour spans, end
// alignment, emphasis in another face with underlines, and measuring.

func round(v float32) float32 { return float32(math.Round(float64(v))) }

func longLine(o render.Overflow) *render.Scene {
	return texts(render.NewText("MMMMMMMMMMMMMMMMMMMM", [2]float32{10, 10}, 16, white).InColumn(60, o))
}

func TestAColumnClipsItsTextAtItsEdges(t *testing.T) {
	h := withFonts(t)
	free := texts(render.NewText("MMMMMMMMMMMMMMMMMMMM", [2]float32{10, 10}, 16, white))
	for _, s := range scales() {
		f := s.Factor()
		whole := offscreen(t, h, free, 120, 40, s)
		clipped := offscreen(t, h, longLine(render.Clip), 120, 40, s)
		left, right := int(round(10*f)), int(round(70*f))
		if _, _, r, _, _ := inkBox(whole); r < right {
			t.Fatalf("@%g: the line must overflow", f)
		}
		for y := range whole.Height {
			for x := range whole.Width {
				want := [4]byte{}
				if x >= left && x < right {
					want = whole.Pixel(x, y)
				}
				if got := clipped.Pixel(x, y); got != want {
					t.Fatalf("@%g: pixel (%d, %d) is %v, want %v", f, x, y, got, want)
				}
			}
		}
	}
}

// lastMarkHeight is the height of the inked rows in the last two inked
// columns: a dot of an ellipsis, not an M.
func lastMarkHeight(img *render.Image) int {
	_, _, right, _, _ := inkBox(img)
	top, bottom := img.Height, -1
	for y := range img.Height {
		for x := right - 1; x <= right; x++ {
			if img.Pixel(x, y)[3] > 64 {
				top, bottom = min(top, y), max(bottom, y)
			}
		}
	}
	return bottom - top + 1
}

func TestAnEllipsisEndsALineWiderThanItsColumn(t *testing.T) {
	h := withFonts(t)
	for _, s := range scales() {
		f := s.Factor()
		img := offscreen(t, h, longLine(render.Ellipsis), 120, 40, s)
		_, _, right, _, n := inkBox(img)
		if n == 0 || right >= int(round(70*f)) {
			t.Fatalf("@%g: ink up to x=%d, the column ends at %v", f, right, round(70*f))
		}
		if height := lastMarkHeight(img); float32(height) > 3*f {
			t.Fatalf("@%g: the last mark is %d px tall, not a dot", f, height)
		}
	}
}

func TestALineThatFitsItsColumnIsUnchanged(t *testing.T) {
	h := withFonts(t)
	free := texts(render.NewText("Ave", [2]float32{10, 10}, 16, white))
	column := texts(render.NewText("Ave", [2]float32{10, 10}, 16, white).InColumn(60, render.Ellipsis))
	for _, s := range scales() {
		a, b := offscreen(t, h, free, 120, 40, s), offscreen(t, h, column, 120, 40, s)
		if string(a.Pixels) != string(b.Pixels) {
			t.Fatalf("@%g: the column changed a line that fits", s.Factor())
		}
	}
}

// solidHalves are the colours of fully inked pixels left and right of the
// ink's middle.
func solidHalves(img *render.Image) (left, right [][3]byte) {
	l, _, r, _, _ := inkBox(img)
	mid := (l + r) / 2
	for y := range img.Height {
		for x := range img.Width {
			if p := img.Pixel(x, y); p[3] == 255 {
				if x < mid {
					left = append(left, [3]byte{p[0], p[1], p[2]})
				} else {
					right = append(right, [3]byte{p[0], p[1], p[2]})
				}
			}
		}
	}
	return left, right
}

func nearRGB(p, want [3]byte) bool {
	for i := range p {
		if d := int(p[i]) - int(want[i]); d > 2 || d < -2 {
			return false
		}
	}
	return true
}

func TestSpansPaintTheirBytesInTheirOwnColour(t *testing.T) {
	h := withFonts(t)
	line := render.NewText("IIIIIIII", [2]float32{4, 4}, 48, white)
	line.Spans = []render.Span{{Start: 4, End: 8, Color: blue}}
	img := offscreen(t, h, texts(line), 240, 80, at(120))
	left, right := solidHalves(img)
	if len(left) < 50 || len(right) < 50 {
		t.Fatalf("%d / %d solid pixels", len(left), len(right))
	}
	for _, p := range left {
		if !nearRGB(p, [3]byte{0xff, 0xff, 0xff}) {
			t.Fatalf("left half pixel %v is not white", p)
		}
	}
	for _, p := range right {
		if !nearRGB(p, [3]byte{0x40, 0xa0, 0xff}) {
			t.Fatalf("right half pixel %v is not #40a0ff", p)
		}
	}
}

func TestAnEndAlignedLineEndsAtItsColumnEdge(t *testing.T) {
	h := withFonts(t)
	for _, s := range scales() {
		f := s.Factor()
		line := render.NewText("12/345", [2]float32{10, 8}, 16, white).InColumn(200, render.Clip)
		line.AlignEnd = true
		left, _, right, _, n := inkBox(offscreen(t, h, texts(line), 240, 40, s))
		if n == 0 || float32(right) <= 200*f || float32(right) > 210*f || float32(left) <= 120*f {
			t.Fatalf("@%g: ink from %d to %d", f, left, right)
		}
	}
}

// facesScene is tests/faces.rs's scene: a tracked label in a theme face,
// and a row with colour spans and emphasis in another face, twice, the
// second with its emphasis underlined.
func facesScene() *render.Scene {
	ink, gold, brass := render.Hex(0xf1e6cc), render.Hex(0xf2c14e), render.Hex(0xd4a017)
	label := render.NewText("INVOKE", [2]float32{8, 6}, 13, gold)
	label.Face, label.Tracking = 1, 0.2
	row := render.NewText("do-not-disturb\ton", [2]float32{8, 30}, 16, ink)
	row.Spans = []render.Span{{Start: 1, End: 2, Color: gold}, {Start: 8, End: 9, Color: gold}, {Start: 15, End: 17, Color: brass}}
	row.Emphasis = []render.Range{{Start: 1, End: 2}, {Start: 8, End: 9}}
	face := 2
	row.EmphasisFace = &face
	underlined := row
	underlined.Origin = [2]float32{8, 56}
	underlined.UnderlineEmphasis = true
	return &render.Scene{Clear: render.Hex(0x170e0b), Texts: []render.Text{label, row, underlined}}
}

var forgeFaces = []text.Face{
	{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 400},
	{Families: []string{"Cinzel", "serif"}, Weight: 600},
	{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 600},
}

func withForgeFaces(t *testing.T) *render.Headless {
	h := withFonts(t)
	if err := h.SetFaces(forgeFaces); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAnUnderlineRunsUnderTheMatchedCharactersOnly(t *testing.T) {
	h := withForgeFaces(t)
	plain := facesScene()
	plain.Texts = plain.Texts[:2]
	plain.Texts[1].Origin = [2]float32{8, 56}
	size := scale.Size{W: 220, H: 84}
	without := offscreen(t, h, plain, size.W, size.H, at(120))
	with := offscreen(t, h, facesScene(), size.W, size.H, at(120))
	differs := func(x, y int) bool { return without.Pixel(x, y) != with.Pixel(x, y) }
	var rows, columns []int
	for y := 56; y < 84; y++ {
		for x := range 220 {
			if differs(x, y) {
				rows = append(rows, y)
				break
			}
		}
	}
	if len(rows) == 0 {
		t.Fatal("no underline was drawn")
	}
	for _, y := range rows {
		if y < 66 {
			t.Fatalf("underline rows %v reach into the glyphs", rows)
		}
	}
	for x := range 220 {
		for _, y := range rows {
			if differs(x, y) {
				columns = append(columns, x)
				break
			}
		}
	}
	// Two matched characters of a 16 px monospace line: "o" and "s".
	if span := columns[len(columns)-1] - columns[0]; span >= 100 || len(columns) >= 30 {
		t.Fatalf("the underline spans %d..%d over %d columns", columns[0], columns[len(columns)-1], len(columns))
	}
}

func TestALineMeasuresItsGlyphsAdvancesInLogicalPixels(t *testing.T) {
	h := measuringFonts(t)
	width := func(s string, sc scale.Scale) float32 {
		w, ok := h.MeasureText(render.NewText(s, [2]float32{}, 16, white), sc)
		if !ok {
			t.Fatal("no measure with fonts")
		}
		return w
	}
	one, two := width("rite", at(120)), width("rite of oil", at(120))
	if one <= 16 || one >= 64 || two <= 2*one {
		t.Fatalf("rite %g, rite of oil %g", one, two)
	}
	if width("", at(120)) != 0 {
		t.Fatal("an empty line is 0 wide")
	}
	if doubled := width("rite of oil", at(240)); doubled-two > 1 || two-doubled > 1 {
		t.Fatalf("logical at any scale: %g and %g", two, doubled)
	}
}

func TestAColumnDoesNotNarrowTheMeasureAndNothingMeasuresWithoutFonts(t *testing.T) {
	h := measuringFonts(t)
	line := render.NewText("the long litany", [2]float32{}, 16, white)
	free, _ := h.MeasureText(line, at(120))
	narrow, _ := h.MeasureText(line.InColumn(10, render.Ellipsis), at(120))
	if free != narrow || free == 0 {
		t.Fatalf("free %g, in a column %g", free, narrow)
	}
	if _, ok := rendertest.HeadlessForStructure(t).MeasureText(line, at(120)); ok {
		t.Fatal("nothing measures without fonts")
	}
}
