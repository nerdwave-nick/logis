package scene

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The columns' widths, measured from the rows in view, each row once,
// growing and never shrinking, afresh for another font.

// ruler measures ten pixels a character, counting what it measured;
// nothing while off.
type ruler struct {
	lines []string
	off   bool
}

func (r *ruler) Width(t render.Text) (float32, bool) {
	if r.off {
		return 0, false
	}
	r.lines = append(r.lines, t.Text)
	return 10 * float32(utf8.RuneCountInString(t.Text)), true
}

func rowsLook(texts ...string) *list.Look {
	l := &list.Look{Lines: 9}
	for _, t := range texts {
		l.Rows = append(l.Rows, list.Row{Text: t})
	}
	return l
}

func fitWith(c *Columns, r *ruler, th *theme.Theme, texts ...string) []float32 {
	l := rowsLook(texts...)
	c.Fit(l, th, r)
	return l.Columns
}

func TestEachColumnIsItsWidestTextButTheLastIsNeverMeasured(t *testing.T) {
	r := &ruler{}
	got := fitWith(&Columns{}, r, forge(),
		"do-not-disturb\ton\tSilence the vox", "vox\tauto\tWho hears", "lone")
	if !slices.Equal(got, []float32{140, 40}) {
		t.Errorf("widths %v, want [140 40]", got)
	}
	for _, l := range r.lines {
		if strings.Contains(l, "vox ") || l == "lone" {
			t.Errorf("last columns are not measured: %q", r.lines)
		}
	}
}

func TestARowIsMeasuredOnce(t *testing.T) {
	c, r, th := &Columns{}, &ruler{}, forge()
	fitWith(c, r, th, "a\tb", "cc\td")
	first := len(r.lines)
	fitWith(c, r, th, "a\tb", "cc\td")
	fitWith(c, r, th, "cc\td", "eee\tf")
	if len(r.lines) != first+1 {
		t.Errorf("measured %q; only eee is new", r.lines)
	}
}

func TestWidthsGrowWithWiderRowsAndNeverShrinkWhileFiltering(t *testing.T) {
	c, r, th := &Columns{}, &ruler{}, forge()
	steps := []struct {
		rows []string
		want []float32
	}{
		{[]string{"abcd\tx"}, []float32{40}},
		// A keystroke narrows the list to a narrower row: nothing moves.
		{[]string{"ab\tx"}, []float32{40}},
		// A wider row comes into view, with a further column.
		{[]string{"abcdef\txy\tz"}, []float32{60, 20}},
		{[]string{"a\tb"}, []float32{60, 20}},
	}
	for i, s := range steps {
		if got := fitWith(c, r, th, s.rows...); !slices.Equal(got, s.want) {
			t.Errorf("step %d %q: widths %v, want %v", i, s.rows, got, s.want)
		}
	}
}

func TestAnotherRowFontStartsAfresh(t *testing.T) {
	c, r, th := &Columns{}, &ruler{}, forge()
	fitWith(c, r, th, "abcdef\tx")
	bigger := *th
	bigger.Row.Font.Size += 4
	if got := fitWith(c, r, &bigger, "ab\tx"); !slices.Equal(got, []float32{20}) {
		t.Errorf("widths %v, want [20]", got)
	}
}

// The measures' key holds the row face's families and weight, so a theme
// whose row face changes (same index) starts afresh too.
func TestAnotherRowFaceStartsAfresh(t *testing.T) {
	c, r, th := &Columns{}, &ruler{}, forge()
	fitWith(c, r, th, "abcdef\tx")
	other := *th
	other.Faces = slices.Clone(th.Faces)
	other.Faces[th.Row.Font.Face].Weight += 100
	if got := fitWith(c, r, &other, "ab\tx"); !slices.Equal(got, []float32{20}) {
		t.Errorf("widths %v, want [20]", got)
	}
}

func TestWithoutFontsRowsAreEstimatedAndMeasuredOnceFontsCome(t *testing.T) {
	c, r, th := &Columns{}, &ruler{off: true}, forge()
	want := []float32{4 * advance(th.Row.Font)}
	if got := fitWith(c, r, th, "abcd\tx"); !slices.Equal(got, want) {
		t.Errorf("estimated %v, want %v", got, want)
	}
	r.off = false
	if got := fitWith(c, r, th, "abcd\tx"); !slices.Equal(got, []float32{40}) {
		t.Errorf("measured %v, want [40]", got)
	}
	if !slices.Equal(r.lines, []string{"abcd"}) {
		t.Errorf("measured %q, want [abcd] once fonts came", r.lines)
	}
}

// remembered: past 4096 rows the memory starts again; the widths stay and a
// row forgotten is only measured again.
func TestManyRowsForgetTheirMeasuresButNotTheWidths(t *testing.T) {
	c, r, th := &Columns{}, &ruler{}, forge()
	fitWith(c, r, th, "abcdefgh\tx")
	for i := range 4096 {
		fitWith(c, r, th, fmt.Sprintf("%d\tx", i))
	}
	before := len(r.lines)
	if got := fitWith(c, r, th, "abcdefgh\tx"); !slices.Equal(got, []float32{80}) {
		t.Errorf("widths %v, want [80]", got)
	}
	if len(r.lines) != before+1 {
		t.Errorf("a forgotten row is measured again: %d measures, want %d", len(r.lines), before+1)
	}
	if got := fitWith(c, r, th, "x\ty"); !slices.Equal(got, []float32{80}) {
		t.Errorf("widths shrank to %v", got)
	}
}

// Measured in the row font: its size, face and tracking, no column.
func TestCellsAreMeasuredInTheRowFont(t *testing.T) {
	th := forge()
	var seen []render.Text
	m := MeasureFunc(func(t render.Text) (float32, bool) {
		seen = append(seen, t)
		return 1, true
	})
	l := rowsLook("abc\tx")
	(&Columns{}).Fit(l, th, m)
	if len(seen) != 1 {
		t.Fatalf("measured %d texts, want 1", len(seen))
	}
	f := th.Row.Font
	if got := seen[0]; got.Text != "abc" || got.Size != f.Size || got.Face != f.Face || got.Tracking != f.Tracking {
		t.Errorf("measured %+v in %+v", got, f)
	}
}
