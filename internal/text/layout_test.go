package text_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/grapheme"
	"github.com/nerdwave-nick/logis/internal/text"
)

// One line, set by text.Fonts.Layout: glyphs in visual order at
// whole-pixel advances, bidi runs in their direction, graphemes kept
// whole, tabs to the next stop, and in a column the end alignment and the
// ellipsis. logis never wraps a line; its line break opportunities are the
// words a column cuts at.

// sizes are the row font's size at the scales the goldens hold: 1.0, 1.3
// and 2.0.
var sizes = []float32{16, 20.8, 32}

// lines are texts of every kind a row shows.
var lines = []string{
	"The machine spirit wakes: 0123456789",
	"Ça déjà vu, naïve Ærø — Œuvre, Šťastný",
	"機械の精霊 万机之神 기계 정령",
	"logis: שָׁלוֹם and مرحبا بالعالم (42) ok",
	"Praise 🔥⚙️💀🤖 the Omnissiah 👍🏽 🧑\u200d💻",
	"שלום עולם ומלואו",
	"office fit",
	"éx",
}

// boundaries are the grapheme cluster boundaries of s, both ends included.
func boundaries(s string) map[int]bool {
	at := map[int]bool{0: true}
	for i := 0; i < len(s); {
		i += grapheme.Next(s[i:])
		at[i] = true
	}
	return at
}

// ellipsisOf is the line's ellipsis glyph, and how many it has.
func ellipsisOf(l text.Line) (text.Glyph, int) {
	var e text.Glyph
	n := 0
	for _, g := range l.Glyphs {
		if g.Ellipsis {
			e, n = g, n+1
		}
	}
	return e, n
}

// covered are the bytes the line's text glyphs show.
func covered(l text.Line) map[int]bool {
	out := map[int]bool{}
	for _, g := range l.Glyphs {
		for i := g.Cluster; i < g.End && !g.Ellipsis; i++ {
			out[i] = true
		}
	}
	return out
}

// oneDirection reports a line whose glyphs all run in its paragraph's
// direction.
func oneDirection(l text.Line) bool {
	for _, g := range l.Glyphs {
		if (g.Level%2 == 1) != l.RTL {
			return false
		}
	}
	return true
}

func whole(v float32) bool { return v == float32(math.Round(float64(v))) }

// Each glyph stands where the one before it ends: left to right in a left
// to right line, right to left in a right to left one, every advance a
// whole pixel; the line is as wide as their sum. LayoutWidth is the same
// sum before rounding, so they differ by at most half a pixel a glyph.
func TestGlyphsFollowEachOtherAtWholePixelAdvances(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, size := range sizes {
		for _, s := range lines {
			line := f.Layout(s, size, text.LineOptions{})
			var sum float32
			for i, g := range line.Glyphs {
				if !whole(g.Advance) || !whole(g.X) {
					t.Errorf("@%g %q: glyph %d at %g advances %g", size, s, i, g.X, g.Advance)
				}
				sum += g.Advance
				if i == 0 {
					continue
				}
				prev := line.Glyphs[i-1]
				if line.RTL && g.X+g.Advance != prev.X || !line.RTL && g.X != prev.X+prev.Advance {
					t.Errorf("@%g %q: glyph %d at %g+%g after %g+%g", size, s, i, g.X, g.Advance, prev.X, prev.Advance)
				}
			}
			if line.Width != sum {
				t.Errorf("@%g %q: width %g, advances %g", size, s, line.Width, sum)
			}
			if d := math.Abs(float64(line.Width - line.LayoutWidth)); d > 0.5*float64(len(line.Glyphs)) {
				t.Errorf("@%g %q: width %g, layout width %g", size, s, line.Width, line.LayoutWidth)
			}
		}
	}
}

// An empty line has no glyphs and no extent; spaces, leading and trailing
// ones too, are laid out and counted in the width.
func TestSpacesAreLaidOutAndAnEmptyLineIsEmpty(t *testing.T) {
	f := pinned(t, "sans-serif")
	if empty := f.Layout("", 16, text.LineOptions{}); len(empty.Glyphs) != 0 || empty.Width != 0 || empty.Ascent != 0 || empty.RTL {
		t.Errorf("an empty line: %+v", empty)
	}
	space := f.Layout(" ", 16, text.LineOptions{})
	if len(space.Glyphs) != 1 || space.Width <= 0 || space.Ascent <= 0 {
		t.Fatalf("a space: %+v", space)
	}
	padded := f.Layout("  lead and trail  ", 16, text.LineOptions{})
	bare := f.Layout("lead and trail", 16, text.LineOptions{})
	if len(padded.Glyphs) != len(bare.Glyphs)+4 || padded.Width != bare.Width+4*space.Width {
		t.Errorf("padded %d glyphs %g wide, bare %d %g wide, a space %g", len(padded.Glyphs), padded.Width,
			len(bare.Glyphs), bare.Width, space.Width)
	}
	if first, last := padded.Glyphs[0], padded.Glyphs[len(padded.Glyphs)-1]; first.X != 0 || last.Cluster != 17 || last.Advance != space.Width {
		t.Errorf("leading %+v, trailing %+v", first, last)
	}
}

// A tab reaches the next stop: stops are 8 spaces apart from the line's
// start, measured before advances are rounded, so the glyph after it
// stands at the stop give or take the rounding of what precedes it.
func TestATabReachesTheNextStopOfEightSpaces(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, size := range sizes {
		tab := 8 * f.Layout(" ", size, text.LineOptions{}).LayoutWidth
		for _, c := range []struct {
			text  string
			after int // the byte that follows the tabs
		}{
			{"a\tb", 2}, {"\tx", 1}, {"abc\t\tdef", 5}, {"do-not-disturb\ton", 15},
		} {
			prefix := f.Layout(trimTabs(c.text[:c.after]), size, text.LineOptions{})
			before, drift := prefix.LayoutWidth, prefix.Width-prefix.LayoutWidth
			stop := (float32(math.Floor(float64(before/tab))) + 1) * tab
			if tabs := c.after - len(trimTabs(c.text[:c.after])); tabs == 2 {
				stop += tab
			}
			line := f.Layout(c.text, size, text.LineOptions{})
			for _, g := range line.Glyphs {
				if g.Cluster == c.after && math.Abs(float64(g.X-drift-stop)) > 1 {
					t.Errorf("@%g %q: %q at %g, want the stop at %g", size, c.text, c.text[c.after], g.X, stop)
				}
			}
		}
	}
}

func trimTabs(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\t' {
		s = s[:len(s)-1]
	}
	return s
}

// A right to left run in a left to right line stands where it is read,
// its letters right to left (descending byte order across the page) at an
// odd level, the left to right words around it in place.
func TestARightToLeftRunInALeftToRightLineReadsRightToLeftInPlace(t *testing.T) {
	f := pinned(t, "sans-serif")
	s := "abc שלום עולם def"
	from, to := len("abc "), len("abc שלום עולם") // the Hebrew run
	line := f.Layout(s, 16, text.LineOptions{})
	if line.RTL {
		t.Fatal("a line that starts in Latin is left to right")
	}
	var order []int
	for _, g := range line.Glyphs {
		order = append(order, g.Cluster)
		if hebrew := g.Cluster >= from && g.Cluster < to; hebrew != (g.Level%2 == 1) {
			t.Errorf("glyph of byte %d at level %d", g.Cluster, g.Level)
		}
	}
	run := order[from : len(order)-len(" def")]
	if !slices.Equal(order[:from], []int{0, 1, 2, 3}) || !slices.Equal(order[len(order)-4:], []int{to, to + 1, to + 2, to + 3}) ||
		!slices.IsSortedFunc(run, func(a, b int) int { return b - a }) || run[0] != to-2 || run[len(run)-1] != from {
		t.Errorf("visual order of bytes %v", order)
	}
}

// A right to left line starts at its right edge (a column's right edge in
// a column) and its words read right to left; a Latin word in it is left
// to right inside.
func TestARightToLeftLineStartsAtItsRightEdge(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, opt := range []text.LineOptions{{}, {Column: true, Width: 200}} {
		line := f.Layout("שלום abc", 16, opt)
		if !line.RTL {
			t.Fatal("a line that starts in Hebrew is right to left")
		}
		right := float32(math.Round(float64(line.LayoutWidth)))
		if opt.Column {
			right = opt.Width
		}
		x := map[int]float32{}
		for _, g := range line.Glyphs {
			x[g.Cluster] = g.X
		}
		first := line.Glyphs[0]
		if first.Cluster != 0 || first.X+first.Advance != right {
			t.Errorf("%+v: the first letter %+v, want its end at %g", opt, first, right)
		}
		if !(x[0] > x[2] && x[2] > x[8] && x[8] > x[11] && x[9] < x[10] && x[10] < x[11]) {
			t.Errorf("%+v: positions %v", opt, x)
		}
	}
}

// Digits at a right to left line's start are kept and stand at its right
// end, read first, themselves left to right.
func TestALeadingNumberInARightToLeftLineIsKept(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Layout("123 שלום", 16, text.LineOptions{})
	if !line.RTL {
		t.Fatal("a Hebrew line is right to left")
	}
	digits := map[int]float32{}
	var hebrewRight float32
	for _, g := range line.Glyphs {
		switch {
		case g.Cluster < 3:
			digits[g.Cluster] = g.X
		case g.Cluster >= 4:
			hebrewRight = max(hebrewRight, g.X+g.Advance)
		}
	}
	if len(digits) != 3 || !(hebrewRight < digits[0] && digits[0] < digits[1] && digits[1] < digits[2]) {
		t.Fatalf("digits at %v, Hebrew up to %g: %+v", digits, hebrewRight, line.Glyphs)
	}
	if line.LayoutWidth < 50 {
		t.Fatalf("the line is measured without its digits: %g", line.LayoutWidth)
	}
}

// Brackets around a number in a right to left run take the run's
// direction: mirrored, enclosing the number, which reads left to right.
// The line is as tall as its tallest font.
func TestBracketsAroundANumberInARightToLeftRunMirrorAndEncloseIt(t *testing.T) {
	f := pinned(t, "sans-serif")
	ltr := f.Layout("(42)", 16, text.LineOptions{})
	open, closing := ltr.Glyphs[0].ID, ltr.Glyphs[3].ID
	for _, s := range []string{"مرحبا (42) ok", "שלום (42) ok"} {
		line := f.Layout(s, 16, text.LineOptions{})
		at := len(s) - len("(42) ok")
		g := map[int]text.Glyph{}
		for _, gl := range line.Glyphs {
			g[gl.Cluster] = gl
		}
		o, four, two, c := g[at], g[at+1], g[at+2], g[at+3]
		if !line.RTL || o.ID != closing || c.ID != open || o.Level%2 != 1 || c.Level%2 != 1 {
			t.Errorf("%q: brackets %+v and %+v", s, o, c)
		}
		if !(o.X > two.X && two.X > four.X && four.X > c.X) {
			t.Errorf("%q: ( at %g, 4 at %g, 2 at %g, ) at %g", s, o.X, four.X, two.X, c.X)
		}
	}
	arabic := f.Layout("مرحبا (42) ok", 16, text.LineOptions{})
	if arabic.Ascent <= ltr.Ascent || arabic.Descent <= ltr.Descent {
		t.Errorf("ascent/descent %g/%g, Latin only %g/%g", arabic.Ascent, arabic.Descent, ltr.Ascent, ltr.Descent)
	}
}

// Every glyph shows whole graphemes: its byte range starts and ends on
// grapheme boundaries, and together the glyphs show every byte. A cut
// line keeps to the same boundaries, its ellipsis too.
func TestEveryGlyphShowsWholeGraphemes(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, s := range append(slices.Clone(lines), "a‍b️", "👍🏽 🧑‍💻 🔥🔥") {
		bounds := boundaries(s)
		for w := float32(0); w <= 420; w += 7 {
			opt := text.LineOptions{Column: w < 420, Width: w, Ellipsis: true}
			line := f.Layout(s, 16, opt)
			for _, g := range line.Glyphs {
				if !bounds[g.Cluster] || !bounds[g.End] || g.Cluster > g.End || g.Cluster == g.End && !g.Ellipsis {
					t.Errorf("%q in %g px: glyph %+v splits a grapheme", s, w, g)
				}
			}
			if !opt.Column && len(covered(line)) != len(s) {
				t.Errorf("%q: the glyphs show %d of %d bytes", s, len(covered(line)), len(s))
			}
		}
	}
}

// A grapheme of several code points is one glyph, or glyphs that share
// its whole range: a skin tone, a ZWJ sequence, a base with combining
// marks; a ligature covers all the letters it joins.
func TestAGraphemeOrALigatureIsShownAsOne(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, c := range []struct {
		text   string
		ranges [][2]int
	}{
		{"👍🏽 🧑\u200d💻", [][2]int{{0, 8}, {8, 9}, {9, 20}}},
		{"éx", [][2]int{{0, 3}, {3, 4}}},
		{"office fit", [][2]int{{0, 1}, {1, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 9}, {9, 10}}},
		{"שָׁלוֹם", [][2]int{{0, 6}, {6, 8}, {8, 12}, {12, 14}}},
	} {
		var got [][2]int
		for _, g := range f.Layout(c.text, 16, text.LineOptions{}).Glyphs {
			if r := [2]int{g.Cluster, g.End}; !slices.Contains(got, r) {
				got = append(got, r)
			}
		}
		slices.SortFunc(got, func(a, b [2]int) int { return a[0] - b[0] })
		if !slices.Equal(got, c.ranges) {
			t.Errorf("%q: glyph ranges %v, want %v", c.text, got, c.ranges)
		}
	}
}

// ASCII operators around a break opportunity ("->", "!=") keep their own
// glyphs where the font has no ligature for them (none of the pinned fonts
// has one, so joining them into one word for such a ligature is not
// observable here).
func TestOperatorsAroundABreakKeepTheirOwnGlyphs(t *testing.T) {
	f := pinned(t, "sans-serif")
	s := "x -> y != z"
	line := f.Layout(s, 16, text.LineOptions{})
	if len(line.Glyphs) != len(s) {
		t.Fatalf("%d glyphs for %d characters", len(line.Glyphs), len(s))
	}
	for i, g := range line.Glyphs {
		if want := f.Font(g.Font).GlyphIndex(rune(s[i])); g.Cluster != i || g.ID != want {
			t.Errorf("glyph %d: %+v, want glyph %d", i, g, want)
		}
	}
}

// A line that fits its column is set as without one; a right to left one
// is moved to the column's right edge.
func TestALineThatFitsItsColumnIsSetAsWithoutOne(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, s := range []string{"Ave", "12/345", "The machine spirit wakes: 0123456789", "שלום", "abc שלום"} {
		free := f.Layout(s, 16, text.LineOptions{})
		for _, ellipsis := range []bool{false, true} {
			col := f.Layout(s, 16, text.LineOptions{Column: true, Width: 400, Ellipsis: ellipsis})
			shift := float32(0)
			if free.RTL {
				shift = 400 - float32(math.Round(float64(free.LayoutWidth)))
			}
			if len(col.Glyphs) != len(free.Glyphs) || col.Width != free.Width {
				t.Fatalf("%q: %d glyphs %g wide in the column, %d %g free", s, len(col.Glyphs), col.Width, len(free.Glyphs), free.Width)
			}
			for i, g := range col.Glyphs {
				want := free.Glyphs[i]
				want.X += shift
				if g != want {
					t.Errorf("%q: glyph %d %+v, want %+v", s, i, g, want)
				}
			}
		}
	}
}

// A line too wide for its column is cut: it ends in one "…" where the
// reading stops (right in a left to right line, left in a right to left
// one), whose empty byte range is the cut; what stays visible is the
// text up to the cut in reading order, and the line is laid out within
// the column. A column with no room for a glyph beside the ellipsis shows
// the ellipsis alone, cut at the text's start.
func TestATooWideLineIsCutWithOneEllipsisWithinItsColumn(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, size := range sizes {
		dots := f.Layout("…", size, text.LineOptions{})
		for _, s := range lines {
			free := f.Layout(s, size, text.LineOptions{})
			for w := float32(0); w < free.LayoutWidth; w += 9 {
				line := f.Layout(s, size, text.LineOptions{Column: true, Width: w, Ellipsis: true})
				e, n := ellipsisOf(line)
				if n != 1 || e.ID != dots.Glyphs[0].ID || e.Cluster != e.End {
					t.Errorf("@%g %q in %g px: ellipsis %+v ×%d", size, s, w, e, n)
					continue
				}
				if w < dots.LayoutWidth && len(line.Glyphs) != 1 {
					t.Errorf("@%g %q in %g px: %d glyphs beside the ellipsis", size, s, w, len(line.Glyphs)-1)
				}
				if len(line.Glyphs) == 1 {
					if e.Cluster != 0 {
						t.Errorf("@%g %q in %g px: the ellipsis alone is cut at %d, want the start", size, s, w, e.Cluster)
					}
					continue
				}
				if line.LayoutWidth > w {
					t.Errorf("@%g %q in %g px: laid out %g wide", size, s, w, line.LayoutWidth)
				}
				shown := covered(line)
				for i := range e.Cluster {
					if !shown[i] {
						t.Errorf("@%g %q in %g px: byte %d before the cut at %d is hidden", size, s, w, i, e.Cluster)
						break
					}
				}
				if len(shown) != e.Cluster {
					t.Errorf("@%g %q in %g px: %d bytes shown, cut at %d", size, s, w, len(shown), e.Cluster)
				}
				// In a line of one direction the glyphs are in reading order.
				if oneDirection(line) && !line.Glyphs[len(line.Glyphs)-1].Ellipsis {
					t.Errorf("@%g %q in %g px: the ellipsis at %g is not where reading stops", size, s, w, e.X)
				}
			}
		}
	}
}

// A cut keeps every glyph that fits beside the ellipsis, inside a word
// too: one more would not fit.
func TestACutKeepsAsManyGlyphsAsFitBesideTheEllipsis(t *testing.T) {
	f := pinned(t, "sans-serif")
	s := "The machine spirit wakes: 0123456789"
	for _, size := range sizes {
		free := f.Layout(s, size, text.LineOptions{})
		dots := f.Layout("…", size, text.LineOptions{}).LayoutWidth
		for w := dots; w < free.LayoutWidth; w += 5 {
			line := f.Layout(s, size, text.LineOptions{Column: true, Width: w, Ellipsis: true})
			e, _ := ellipsisOf(line)
			kept := f.Layout(s[:e.Cluster], size, text.LineOptions{}).LayoutWidth
			more := f.Layout(s[:e.Cluster+1], size, text.LineOptions{}).LayoutWidth
			if kept+dots > w+0.01 || more+dots <= w {
				t.Errorf("@%g in %g px: cut after %q (%g + %g)", size, w, s[:e.Cluster], kept, dots)
			}
		}
	}
	if e, _ := ellipsisOf(f.Layout(s, 16, text.LineOptions{Column: true, Width: 60, Ellipsis: true})); s[e.Cluster-1] != 'm' {
		t.Errorf("in 60 px the cut is at %d, want inside \"machine\"", e.Cluster)
	}
}

// Without an ellipsis a column cuts nothing: the line is set whole (the
// renderer clips it).
func TestAColumnWithoutAnEllipsisCutsNothing(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, s := range []string{"The machine spirit wakes: 0123456789", "MMMMMMMMMMMMMMMMMMMM", "12/345"} {
		free := f.Layout(s, 16, text.LineOptions{})
		for _, w := range []float32{0, 8, 60} {
			col := f.Layout(s, 16, text.LineOptions{Column: true, Width: w})
			if !slices.Equal(col.Glyphs, free.Glyphs) {
				t.Errorf("%q in %g px: %d glyphs, %d free", s, w, len(col.Glyphs), len(free.Glyphs))
			}
		}
	}
}

// AlignEnd sets a line against its column's end edge: the right edge
// for a left to right line, the left edge for a right to left one; a cut
// line too.
func TestAlignEndSetsTheLineAgainstTheColumnsEndEdge(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, c := range []struct {
		text     string
		width    float32
		ellipsis bool
	}{
		{"12/345", 200, false}, {"abc שלום", 200, false}, {"שלום", 200, false},
		{"12/345", 30, true}, {"The machine spirit wakes: 0123456789", 60, true}, {"שלום עולם ומלואו", 60, true},
	} {
		line := f.Layout(c.text, 16, text.LineOptions{Column: true, Width: c.width, Ellipsis: c.ellipsis, AlignEnd: true})
		left, right := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, g := range line.Glyphs {
			left, right = min(left, g.X), max(right, g.X+g.Advance)
		}
		edge, at := c.width, right
		if line.RTL {
			edge, at = 0, left
		}
		if math.Abs(float64(at-edge)) > 1 {
			t.Errorf("%q in %g px: from %g to %g, want against %g", c.text, c.width, left, right, edge)
		}
	}
}

// A cut right to left line starts at its column's right edge and ends in
// the ellipsis at the left, within the column.
func TestACutRightToLeftLineEndsInTheEllipsisAtTheLeft(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Layout("שלום עולם ומלואו", 16, text.LineOptions{Column: true, Width: 60, Ellipsis: true})
	first, last := line.Glyphs[0], line.Glyphs[len(line.Glyphs)-1]
	if !line.RTL || first.Cluster != 0 || first.X+first.Advance != 60 || !last.Ellipsis || last.X < 0 || last.Level%2 != 1 {
		t.Errorf("glyphs %+v", line.Glyphs)
	}
}

// A match at a word's start sets only its own letters in the emphasis face.
func TestAnEmphasisAtAWordsStartKeepsToItsLetters(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces(forgeFaces()); err != nil {
		t.Fatal(err)
	}
	plain := f.Layout("of", 16, text.LineOptions{})
	line := f.Layout("of", 16, text.LineOptions{Faces: []text.FaceSpan{{Start: 0, End: 1, Face: 2}}})
	if len(line.Glyphs) != 2 || len(plain.Glyphs) != 2 {
		t.Fatalf("glyphs %v / %v", line.Glyphs, plain.Glyphs)
	}
	if line.Glyphs[0].Font == plain.Glyphs[0].Font {
		t.Errorf("the matched o is in the row's font %d", line.Glyphs[0].Font)
	}
	if line.Glyphs[1].Font != plain.Glyphs[1].Font {
		t.Errorf("the f is in font %d, want the row's %d", line.Glyphs[1].Font, plain.Glyphs[1].Font)
	}
}

// Every emphasis span sets exactly its bytes in its face, inside words
// and across them, cut or not; the rest keeps the row's face.
func TestEveryEmphasisSpanSetsExactlyItsBytesInItsFace(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces(forgeFaces()); err != nil {
		t.Fatal(err)
	}
	row := f.Layout("x", 16, text.LineOptions{}).Glyphs[0]
	for _, c := range []struct {
		text  string
		spans []text.FaceSpan
		width float32
	}{
		{"proof", []text.FaceSpan{{Start: 2, End: 3, Face: 2}}, 0},
		{"rite of oil", []text.FaceSpan{{Start: 0, End: 4, Face: 1}}, 0},
		{"do-not-disturb\ton", []text.FaceSpan{{Start: 1, End: 2, Face: 2}, {Start: 8, End: 9, Face: 2}}, 0},
		{"do-not-disturb\ton", []text.FaceSpan{{Start: 1, End: 2, Face: 2}, {Start: 8, End: 9, Face: 2}}, 70},
		{"Hail the machine", []text.FaceSpan{{Start: 1, End: 3, Face: 2}}, 40},
	} {
		face := map[int]text.FontID{}
		for _, sp := range c.spans {
			g := f.Layout(c.text[sp.Start:sp.End], 16, text.LineOptions{Face: sp.Face}).Glyphs[0]
			for i := sp.Start; i < sp.End; i++ {
				face[i] = g.Font
			}
		}
		line := f.Layout(c.text, 16, text.LineOptions{Faces: c.spans, Column: c.width > 0, Width: c.width, Ellipsis: true})
		for _, g := range line.Glyphs {
			want, ok := face[g.Cluster]
			if !ok {
				want = row.Font
			}
			if !g.Ellipsis && g.Font != want {
				t.Errorf("%q in %g px: byte %d in font %d, want %d", c.text, c.width, g.Cluster, g.Font, want)
			}
		}
	}
}
