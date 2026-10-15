package scene

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// Ported from Rust src/list/scene/tests.rs for the bundled default theme;
// the phosphor theme, the post pass and personal sigil images are in
// effects_test.go.

func forge() *theme.Theme {
	t := theme.Default()
	return &t
}

func still(t *theme.Theme) Style { return Style{Theme: t} }

func look(rows, selected int) *list.Look {
	l := &list.Look{Prompt: "> ", Input: "rite", Cursor: 2, HasCursor: true, Lines: 5,
		Counter: true, Matched: rows, Total: 9}
	for i := range rows {
		l.Rows = append(l.Rows, list.Row{
			Text: "row " + string(rune('0'+i)) + "\tcol", Highlights: []list.Range{{Start: 0, End: 1}},
			Selected: i == selected,
		})
	}
	return l
}

func opts(lines, width *uint32) list.Options {
	o := list.DefaultOptions()
	o.Lines, o.Width = lines, width
	return o
}

func u(n uint32) *uint32 { return &n }

func sized(t *theme.Theme) scale.Size {
	size, _ := Fit(opts(u(5), u(40)), t)
	return size
}

func find(t *testing.T, s *render.Scene, text string) render.Text {
	t.Helper()
	for _, x := range s.Texts {
		if x.Text == text {
			return x
		}
	}
	t.Fatalf("no text %q in %v", text, texts(s))
	return render.Text{}
}

func texts(s *render.Scene) []string {
	var out []string
	for _, x := range s.Texts {
		out = append(out, x.Text)
	}
	return out
}

// Rust v1: 820×520 and nine rows. Go: the height follows the theme's
// eleven rows (fit_test.go): 55 + 2·12 + 11·38 = 497.
func TestWithoutASizeTheWindowIsTheThemes(t *testing.T) {
	th := forge()
	size, rows := Fit(list.DefaultOptions(), th)
	m := 2 * th.Window.Margin
	if size != (scale.Size{W: uint32(820 + m), H: uint32(497 + m)}) || rows != 11 {
		t.Fatalf("%v %d", size, rows)
	}
	body, radius := Body(size, th)
	if body[2] != 820 || body[3] != 497 || radius != 14 {
		t.Fatalf("%v %v", body, radius)
	}
	if g := GridOf(size, rows, th); g.Rows != 11 {
		t.Fatalf("%d rows fit", g.Rows)
	}
}

func TestLinesAndWidthInTheOptionsSizeTheWindow(t *testing.T) {
	th := forge()
	s30, rows := Fit(opts(u(15), u(30)), th)
	s60, _ := Fit(opts(u(5), u(60)), th)
	if rows != 15 {
		t.Fatalf("%d", rows)
	}
	if s60.W-s30.W != uint32(math.Round(float64(30*advance(th.Row.Font)))) {
		t.Fatalf("%d", s60.W-s30.W)
	}
	if s30.H-s60.H != uint32(10*th.Row.Height) {
		t.Fatalf("%d", s30.H-s60.H)
	}
	s0, _ := Fit(opts(u(0), nil), th)
	if s0.H != uint32(th.Prompt.Height+2*th.Window.Margin) {
		t.Fatalf("only the prompt line: %d", s0.H)
	}
}

func TestThePromptLineShowsSigilLabelQueryCaretAndCounter(t *testing.T) {
	th := forge()
	s := Draw(look(3, 0), sized(th), still(th))
	label := find(t, s, "INVOKE")
	if label.Face != th.Prompt.Font.Face || label.Tracking != 0.2 {
		t.Fatalf("%+v", label)
	}
	query := find(t, s, "rite")
	if query.Origin[0] <= label.Origin[0] {
		t.Fatal("the query is not after the label")
	}
	if c := query.Caret; c == nil || c.At != 2 || c.Shape != render.CaretBar || c.Color != render.Hex(0xf2c14e) {
		t.Fatalf("caret %+v", query.Caret)
	}
	counter := find(t, s, "3/9")
	if !counter.AlignEnd || !(query.Width < counter.Width) {
		t.Fatalf("counter %+v, query width %v", counter, query.Width)
	}
	// The gear: a ring with eight teeth and a hub, before the label.
	gear := 0
	for _, r := range s.Shapes {
		if r.Rect[0]+r.Rect[2] < label.Origin[0] && r.Rect[2] < 20 {
			gear++
		}
	}
	if gear < 10 {
		t.Fatalf("%d gear shapes", gear)
	}
}

func TestASessionPromptShowsAndTheDefaultOneGivesWayToTheLabel(t *testing.T) {
	th := forge()
	l := look(1, 0)
	l.Prompt = "pin: "
	s := Draw(l, sized(th), still(th))
	query := find(t, s, "pin: rite")
	if query.Spans[0] != (render.Span{Start: 0, End: 5, Color: th.Prompt.Color}) {
		t.Fatalf("%+v", query.Spans)
	}
	if query.Caret.At != 7 {
		t.Fatalf("caret at %d, after the prompt and 'ri'", query.Caret.At)
	}
}

func TestAPlaceholderHasNoCaretAndThePlaceholderColour(t *testing.T) {
	th := forge()
	l := look(0, 0)
	l.HasCursor, l.Input = false, "seek"
	query := find(t, Draw(l, sized(th), still(th)), "seek")
	if query.Caret != nil || !reflect.DeepEqual(query.Spans, []render.Span{{Start: 0, End: 4, Color: th.Query.Placeholder}}) {
		t.Fatalf("%+v", query)
	}
}

func TestRowsTakeColumnColoursAndSetMatchesApart(t *testing.T) {
	th := forge()
	logical := sized(th)
	s := Draw(look(3, 1), logical, still(th))
	plain := find(t, s, "row 0")
	if plain.Color != th.Row.Columns[0] || !reflect.DeepEqual(plain.Spans, []render.Span{{Start: 0, End: 1, Color: th.Match.Color}}) ||
		!reflect.DeepEqual(plain.Emphasis, []render.Range{{Start: 0, End: 1}}) || *plain.EmphasisFace != th.Match.Face ||
		plain.UnderlineEmphasis {
		t.Fatalf("%+v", plain)
	}
	if col := find(t, s, "col"); col.Color != th.Row.Columns[1] {
		t.Fatalf("%+v", col)
	}
	selected := find(t, s, "row 1")
	if selected.Color != th.Selection.Columns[0] {
		t.Fatalf("%+v", selected)
	}
	g := GridOf(logical, 5, th)
	if math.Abs(float64(selected.Origin[1]-plain.Origin[1]-g.Pitch)) > 1e-3 {
		t.Fatalf("rows %v and %v are not a pitch apart", plain.Origin, selected.Origin)
	}
	if band, ok := BandTop(look(3, 1), logical, th); !ok || band != g.Top(1) {
		t.Fatalf("band %v %v", band, ok)
	}
	if plain.Origin[1] <= th.Prompt.Height+th.Window.Margin {
		t.Fatal("a row above the rule")
	}
}

func decorated() *list.Look {
	l := look(3, 0)
	l.Glyphs = true
	l.Rows[0].Glyph = "⚙"
	l.Rows[0].Badges = []string{"current", "edict"}
	l.Rows[1].Pending = true
	return l
}

func TestBadgesEndTheRowAndAPendingRowIsDimmedAndMarked(t *testing.T) {
	th := forge()
	s := Draw(decorated(), sized(th), still(th))
	var first render.Text
	for _, x := range s.Texts {
		if x.Origin[1] == find(t, s, "row 0").Origin[1] && x.Text == "col" {
			first = x
		}
	}
	current, edict := find(t, s, "CURRENT"), find(t, s, "EDICT")
	if first.Origin[0]+first.Width > current.Origin[0] || current.Origin[0] >= edict.Origin[0] {
		t.Fatalf("col %+v, current %+v, edict %+v", first, current, edict)
	}
	pending := find(t, s, "row 1")
	if pending.Color.A != th.Pending.Color.A*th.Pending.Opacity || len(pending.Emphasis) != 0 {
		t.Fatalf("%+v", pending)
	}
	find(t, s, "DIVINING…")
	if glyph := find(t, s, "⚙"); glyph.Origin[0] >= find(t, s, "row 0").Origin[0] {
		t.Fatal("the glyph is not before the row")
	}
}

func TestAToastLiesOverTheBottomOfTheBodyInItsLevelsColour(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 0)
	l.Toast = &list.Notice{Text: "heresy in the theme", Level: list.Error}
	s := Draw(l, logical, still(th))
	words := find(t, s, "heresy in the theme")
	body, _ := Body(logical, th)
	if words.Origin[1]+words.LineHeight > body[1]+body[3] {
		t.Fatal("the toast hangs below the body")
	}
	if strip := s.Shapes[len(s.Shapes)-1]; !reflect.DeepEqual(strip.Border.Paint, render.Solid(th.Error)) {
		t.Fatalf("%+v", strip.Border)
	}
}

func TestAGlidingBandMovesAlone(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 2)
	rest := Draw(l, logical, still(th))
	top, _ := BandTop(l, logical, th)
	gliding := DrawAt(l, logical, top-10, true, still(th), nil)
	if !reflect.DeepEqual(gliding.Texts, rest.Texts) {
		t.Fatal("texts moved")
	}
	moved := 0
	for i := range gliding.Shapes {
		if !reflect.DeepEqual(gliding.Shapes[i], rest.Shapes[i]) {
			moved++
			if gliding.Shapes[i].Rect[1] != rest.Shapes[i].Rect[1]-10 {
				t.Fatalf("%v vs %v", gliding.Shapes[i].Rect, rest.Shapes[i].Rect)
			}
		}
	}
	if moved != 2 {
		t.Fatalf("%d shapes moved, want the band and its edge bar", moved)
	}
	if !reflect.DeepEqual(DrawAt(l, logical, top, true, still(th), nil), rest) {
		t.Fatal("at rest it differs")
	}
}

func TestTheBodyIsTheFirstShape(t *testing.T) {
	th := forge()
	logical := sized(th)
	rect, radius := Body(logical, th)
	s := Draw(look(3, 0), logical, still(th))
	if s.Shapes[0].Rect != rect || s.Shapes[0].Radius != radius {
		t.Fatalf("%+v", s.Shapes[0])
	}
}

func TestTheGridFindsTheRowUnderAPoint(t *testing.T) {
	th := forge()
	logical := sized(th)
	g := GridOf(logical, 5, th)
	for i := range 5 {
		if row, ok := g.RowAt(100, float64(g.Top(i)+g.Pitch/2)); !ok || row != i {
			t.Fatalf("row %d: %d %v", i, row, ok)
		}
	}
	for _, p := range [][2]float64{{100, float64(g.Top(5) + 1)}, {100, 10}, {2, float64(g.First) + 2}} {
		if _, ok := g.RowAt(p[0], p[1]); ok {
			t.Fatalf("a row at %v", p)
		}
	}
	if GridOf(logical, 2, th).Rows != 2 {
		t.Fatal("two lines asked for")
	}
}

func TestNoLinesMeansNoRuleAndNoRows(t *testing.T) {
	th := forge()
	l := look(0, 0)
	l.Lines = 0
	logical, _ := Fit(opts(u(0), nil), th)
	s := Draw(l, logical, still(th))
	ruleY := th.Window.Margin + th.Prompt.Height
	if slices.ContainsFunc(s.Shapes, func(r render.RoundedRect) bool { return r.Rect[1] == ruleY }) {
		t.Fatal("a rule")
	}
}

func TestTheGlowPulsesAndTheHighlightTravelsUntilMotionIsReduced(t *testing.T) {
	th := forge()
	logical := sized(th)
	at := func(th *theme.Theme, ms uint64) (float32, float32) {
		body := Draw(look(1, 0), logical, Style{Theme: th, At: ms * 1e6}).Shapes[0]
		return body.Glow.Color.A, body.Border.Highlight.Angle
	}
	glow0, angle0 := at(th, 0)
	glow1, angle1 := at(th, 1500)
	if glow1 >= glow0 || math.Abs(float64(angle0)+math.Pi/2) > 1e-6 || angle1 <= angle0 {
		t.Fatalf("glow %v→%v, angle %v→%v", glow0, glow1, angle0, angle1)
	}
	if !Moving(look(1, 0), th) {
		t.Fatal("the forge stands still")
	}
	s := th.Stilled()
	g0, a0 := at(&s, 0)
	g1, a1 := at(&s, 1500)
	if g0 != g1 || a0 != a1 || Moving(look(1, 0), &s) {
		t.Fatal("reduce-motion does not still them")
	}
}

func TestAPendingRowShimmersAndKeepsAStillThemeMoving(t *testing.T) {
	stilled := forge().Stilled()
	shimmering := stilled
	shimmering.Pending.Shimmer = 0.3
	l := look(2, 0)
	if Moving(l, &shimmering) {
		t.Fatal("moving without a pending row")
	}
	l.Rows[1].Pending = true
	if !Moving(l, &shimmering) || Moving(l, &stilled) {
		t.Fatal("the shimmer does not decide")
	}
	mark := func(ms uint64) float32 {
		return find(t, Draw(l, sized(&shimmering), Style{Theme: &shimmering, At: ms * 1e6}), "DIVINING…").Color.A
	}
	if mark(800) >= mark(0) {
		t.Fatal("the mark's opacity does not swing")
	}
}

func rites(columns []float32) *list.Look {
	l := &list.Look{Lines: 5, Columns: columns}
	for _, r := range []string{"do-not-disturb\ton\tSilence the vox of the desktop", "vox\tauto\tWho hears the machine", "lone"} {
		l.Rows = append(l.Rows, list.Row{Text: r})
	}
	return l
}

func TestColumnsLineUpAtTheirMeasuredWidthsAndTheLastTakesTheRest(t *testing.T) {
	th := forge()
	logical := sized(th)
	s := Draw(rites([]float32{140, 30}), logical, still(th))
	g := GridOf(logical, 5, th)
	left, right, gap := g.Left+th.Row.Padding, g.Right-th.Row.Padding, th.Row.ColumnGap
	name, vox := find(t, s, "do-not-disturb"), find(t, s, "vox")
	on, auto := find(t, s, "on"), find(t, s, "auto")
	purpose, who := find(t, s, "Silence the vox of the desktop"), find(t, s, "Who hears the machine")
	if name.Origin[0] != left || vox.Origin[0] != left || on.Origin[0] != left+140+gap || auto.Origin[0] != on.Origin[0] ||
		purpose.Origin[0] != on.Origin[0]+30+gap || who.Origin[0] != purpose.Origin[0] {
		t.Fatalf("columns do not line up: %v %v %v %v %v %v", name.Origin, vox.Origin, on.Origin, auto.Origin, purpose.Origin, who.Origin)
	}
	if name.Width != 140+gap/2 || name.Overflow != render.Ellipsis || purpose.Width != right-purpose.Origin[0] {
		t.Fatalf("widths %v %v", name.Width, purpose.Width)
	}
	if lone := find(t, s, "lone"); lone.Origin[0] != left || lone.Width != right-left {
		t.Fatalf("%+v", lone)
	}
}

func TestAColumnWiderThanItsShareOfTheRowEndsInAnEllipsis(t *testing.T) {
	th := forge()
	logical := sized(th)
	s := Draw(rites([]float32{5000, 30}), logical, still(th))
	g := GridOf(logical, 5, th)
	limit := float32(math.Floor(float64(th.Row.ColumnMax * (g.Right - g.Left - 2*th.Row.Padding))))
	name := find(t, s, "do-not-disturb")
	if name.Width != limit+th.Row.ColumnGap/2 || name.Overflow != render.Ellipsis ||
		find(t, s, "on").Origin[0] != name.Origin[0]+limit+th.Row.ColumnGap {
		t.Fatalf("%+v", name)
	}
}

func TestCellsTakeTheirColumnsColoursAndTheMatchesWithinThem(t *testing.T) {
	th := forge()
	l := rites([]float32{140, 30})
	l.Rows[1].Highlights = []list.Range{{Start: 0, End: 3}, {Start: 4, End: 6}}
	l.Rows[1].Selected = true
	l.Rows[0].Highlights = []list.Range{{Start: 1, End: 2}}
	s := Draw(l, sized(th), still(th))
	name := find(t, s, "do-not-disturb")
	if name.Color != th.Row.Columns[0] || !reflect.DeepEqual(name.Spans, []render.Span{{Start: 1, End: 2, Color: th.Match.Color}}) {
		t.Fatalf("%+v", name)
	}
	if find(t, s, "on").Color != th.Row.Columns[1] || find(t, s, "Silence the vox of the desktop").Color != th.Row.Columns[2] {
		t.Fatal("column colours")
	}
	vox := find(t, s, "vox")
	if vox.Color != th.Selection.Columns[0] || !reflect.DeepEqual(vox.Spans, []render.Span{{Start: 0, End: 3, Color: th.Match.Selected}}) {
		t.Fatalf("%+v", vox)
	}
	auto := find(t, s, "auto")
	if auto.Color != th.Selection.Columns[1] || !reflect.DeepEqual(auto.Spans, []render.Span{{Start: 0, End: 2, Color: th.Match.Selected}}) ||
		!reflect.DeepEqual(auto.Emphasis, []render.Range{{Start: 0, End: 2}}) {
		t.Fatalf("%+v", auto)
	}
	if len(find(t, s, "Who hears the machine").Spans) != 0 {
		t.Fatal("spans beyond the matches")
	}
}

func TestUnmeasuredColumnsAreEstimatedFromTheRowsInView(t *testing.T) {
	th := forge()
	s := Draw(rites(nil), sized(th), still(th))
	on, auto := find(t, s, "on"), find(t, s, "auto")
	if on.Origin[0] != auto.Origin[0] || on.Origin[0] <= find(t, s, "do-not-disturb").Origin[0]+14*8 ||
		find(t, s, "Silence the vox of the desktop").Origin[0] != find(t, s, "Who hears the machine").Origin[0] {
		t.Fatal("estimated columns do not line up")
	}
}

// Ported from Rust src/list/scene/rows.rs's tests.
func TestALineIsCutIntoColumnsAtItsTabs(t *testing.T) {
	for _, c := range []struct {
		text string
		want []list.Range
	}{
		{"ab\tcd\tef", []list.Range{rg(0, 2), rg(3, 5), rg(6, 8)}},
		{"rite", []list.Range{rg(0, 4)}},
		{"", []list.Range{rg(0, 0)}},
		{"\tx\t", []list.Range{rg(0, 0), rg(1, 2), rg(3, 3)}},
	} {
		if got := cellRanges(c.text); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: %v", c.text, got)
		}
	}
}

func TestMatchesAreCutToTheirColumn(t *testing.T) {
	h := []list.Range{rg(1, 2), rg(3, 6), rg(7, 9)}
	for _, c := range []struct {
		cell list.Range
		want []list.Range
	}{
		{rg(0, 2), []list.Range{rg(1, 2)}},
		{rg(3, 5), []list.Range{rg(0, 2)}},
		{rg(6, 8), []list.Range{rg(1, 2)}},
		{rg(9, 9), nil},
	} {
		if got := within(h, c.cell); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%v: %v", c.cell, got)
		}
	}
}

func rg(start, end int) list.Range { return list.Range{Start: start, End: end} }
