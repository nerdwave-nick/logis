package scene

import (
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// Plain logis's tab strip (feat/logis-go 8cba2fc9): two equal plates above
// the panel, Applications left and Rites right, fixed in place; the active
// one is lit. The panel keeps its own size below the strip, so the surface
// grows by the strip.

func phosphor(t *testing.T) *theme.Theme {
	t.Helper()
	th, err := theme.Themes{}.Load("cogitator-phosphor")
	if err != nil {
		t.Fatal(err)
	}
	return &th
}

func strip(active int) Strip {
	return Strip{Tabs: [2]Tab{{Label: "applications", Count: 22, Icon: AppsIcon}, {Label: "rites", Count: 7, Icon: RitesIcon}}, Active: active}
}

func TestTheStripAddsItsHeightAboveTheFittedPanel(t *testing.T) {
	th := forge()
	if got := StripHeight(th); got != 30 {
		t.Fatalf("strip %v, want 30 (plate 38 + 8 above the body + 16 of the 32 margin left above it)", got)
	}
	plain, rows := Fit(list.DefaultOptions(), th)
	tabbed, tabbedRows := FitTabbed(list.DefaultOptions(), th, 0)
	if tabbed.W != plain.W || tabbed.H != plain.H+30 || tabbedRows != rows {
		t.Errorf("%v with %d rows, want %v + 30 with %d", tabbed, tabbedRows, plain, rows)
	}
	// On a short output the strip's height comes off the room the rows
	// have.
	short, shortRows := FitTabbed(list.DefaultOptions(), th, 400)
	_, want := FitIn(list.DefaultOptions(), th, 370)
	if short.H != 400 || shortRows != want {
		t.Errorf("on a 400 output: %v with %d rows, want 400 high with %d", short, shortRows, want)
	}
}

func TestThePanelLiesUnderTheStripAndItsRowsWithIt(t *testing.T) {
	th := forge()
	size, rows := FitTabbed(list.DefaultOptions(), th, 0)
	body, _ := TabbedBody(size, th)
	if body[1] != th.Window.Margin+30 || body[3] != float32(size.H)-30-2*th.Window.Margin {
		t.Errorf("body %v on %v", body, size)
	}
	g := TabbedGrid(size, rows, th)
	plain, plainRows := Fit(list.DefaultOptions(), th)
	pg := GridOf(plain, plainRows, th)
	if g.First != pg.First+30 || g.Pitch != pg.Pitch || g.Rows != pg.Rows {
		t.Errorf("grid %+v, want %+v 30 lower", g, pg)
	}
	// The pointer finds the rows below the strip, and none on it.
	if row, ok := g.RowAt(400, float64(g.Top(3)+g.Pitch/2)); !ok || row != 3 {
		t.Errorf("row %d (%v)", row, ok)
	}
	if _, ok := g.RowAt(400, 30); ok {
		t.Error("a row on the strip")
	}
	band, ok := TabbedBandTop(look(3, 2), size, th)
	if !ok || band != g.Top(2) {
		t.Errorf("band at %v (%v), want %v", band, ok, g.Top(2))
	}
}

func TestThePlatesAreFixedEqualAndApartFromTheBody(t *testing.T) {
	th := forge()
	size, _ := FitTabbed(list.DefaultOptions(), th, 0)
	body, _ := TabbedBody(size, th)
	plates := Plates(size, th)
	apps, rites := plates[0], plates[1]
	if apps.Rect != [4]float32{body[0], body[1] - 46, 236, 38} {
		t.Errorf("applications %v (body %v)", apps.Rect, body)
	}
	if rites.Rect != [4]float32{body[0] + 244, body[1] - 46, 236, 38} {
		t.Errorf("rites %v", rites.Rect)
	}
	if apps.Radius != 9 || Plates(size, phosphor(t))[0].Radius != 4 {
		t.Errorf("radii %v", apps.Radius)
	}
	for i, at := range [][2]float64{{float64(body[0]) + 100, float64(body[1]) - 27}, {float64(body[0]) + 300, float64(body[1]) - 27}} {
		if got, ok := PlateAt(at[0], at[1], size, th); !ok || got != i {
			t.Errorf("at %v: plate %d (%v), want %d", at, got, ok, i)
		}
	}
	for _, at := range [][2]float64{{float64(body[0]) + 240, float64(body[1]) - 27}, {float64(body[0]) + 100, float64(body[1]) + 20}, {float64(body[0]) + 100, 4}} {
		if got, ok := PlateAt(at[0], at[1], size, th); ok {
			t.Errorf("at %v: plate %d", at, got)
		}
	}
}

func TestTheStripDrawsBothPlatesTheirCountsAndTheKeyHint(t *testing.T) {
	th := forge()
	size, _ := FitTabbed(list.DefaultOptions(), th, 0)
	l := look(3, 0)
	sc := DrawTabbed(l, strip(1), size, 0, false, still(th), nil)
	for _, words := range []string{"APPLICATIONS", "RITES", "22", "7", "TAB", "SWITCH"} {
		find(t, sc, words)
	}
	// The active plate's label is in the selection's first column colour,
	// the other's in the counter's.
	if c := find(t, sc, "RITES").Color; c != th.Selection.Columns[0] {
		t.Errorf("active label %v", c)
	}
	if c := find(t, sc, "APPLICATIONS").Color; c != th.Counter.Color {
		t.Errorf("idle label %v", c)
	}
	// The plates are glass for the effects, the body lies under the strip.
	plates := Plates(size, th)
	if !slices.Equal(sc.Post.Regions, plates[:]) {
		t.Errorf("regions %v, want %v", sc.Post.Regions, plates)
	}
	body, _ := TabbedBody(size, th)
	if sc.Post.Rect != body {
		t.Errorf("post over %v, want the body %v", sc.Post.Rect, body)
	}
	// The prompt line moved down with the body.
	if q := find(t, sc, "rite"); q.Origin[1] < body[1] {
		t.Errorf("the query at %v, above the body at %v", q.Origin, body)
	}
	// phosphor's counts are in brackets, as its badges, and its key hint
	// is one word.
	ph := phosphor(t)
	sc = DrawTabbed(l, strip(0), size, 0, false, still(ph), nil)
	for _, words := range []string{"[22]", "[7]", "<TAB> SWITCH"} {
		find(t, sc, words)
	}
	if c := find(t, sc, "APPLICATIONS").Color; c != ph.Selection.Columns[0] {
		t.Errorf("phosphor's active label %v", c)
	}
}

func TestAnEmptyTabPointsToTheOther(t *testing.T) {
	th := forge()
	size, _ := FitTabbed(list.DefaultOptions(), th, 0)
	l := &list.Look{Prompt: "> ", Input: "zz", Cursor: 2, HasCursor: true, Lines: 5}
	st := strip(0)
	st.Tabs[0].Count, st.Empty = 0, true
	sc := DrawTabbed(l, st, size, 0, false, still(th), nil)
	words := find(t, sc, "nothing answers here · 7 among the rites — Tab")
	g := TabbedGrid(size, 5, th)
	if words.Origin[1] < g.Top(0) || words.Origin[1] > g.Top(1) {
		t.Errorf("at %v, not in the first row (%v)", words.Origin, g.Top(0))
	}
	st.Tabs[1].Count = 0
	find(t, DrawTabbed(l, st, size, 0, false, still(th), nil), "nothing answers here")
}

// Silent rites (2aaad4b9): plain logis whose caller has no servitor on its
// PATH still shows the strip; the rites' plate counts "—", and their tab
// says why in its first rows: servitor is not found, and a keybind's PATH
// is not always the shell's.
func TestSilentRitesCountNothingAndSayWhy(t *testing.T) {
	th := forge()
	size, _ := FitTabbed(list.DefaultOptions(), th, 0)
	l := &list.Look{Prompt: "> ", Input: "seek a rite", Lines: 5}
	st := strip(1)
	st.Tabs[1].Count, st.Tabs[1].Silent = 0, true
	sc := DrawTabbed(l, st, size, 0, false, still(th), nil)
	find(t, sc, "—")
	g := TabbedGrid(size, 5, th)
	first := find(t, sc, SilentRites)
	if first.Origin[1] < g.Top(0) || first.Origin[1] > g.Top(1) {
		t.Errorf("at %v, not in the first row (%v)", first.Origin, g.Top(0))
	}
	for i, words := range SilentRitesWhy {
		row := i + 1
		if x := find(t, sc, words); x.Origin[1] < g.Top(row) || x.Origin[1] > g.Top(row+1) {
			t.Errorf("%q at %v, not in row %d (%v)", words, x.Origin, row, g.Top(row))
		}
	}
	why := strings.Join(SilentRitesWhy[:], " ")
	for _, words := range []string{"cannot find servitor", "PATH", "keybind", "shell"} {
		if !strings.Contains(SilentRites+" "+why, words) {
			t.Errorf("the words never name %q", words)
		}
	}
	// A query typed elsewhere changes nothing: the rites stay silent, not
	// empty.
	st.Empty = true
	sc = DrawTabbed(l, st, size, 0, false, still(th), nil)
	find(t, sc, SilentRites)
	for _, x := range texts(sc) {
		if strings.HasPrefix(x, "nothing answers here") {
			t.Errorf("an empty tab's line: %q", x)
		}
	}
	// The applications' empty line never points to silent rites, and
	// says nothing of them.
	st.Active, st.Tabs[0].Count = 0, 0
	sc = DrawTabbed(l, st, size, 0, false, still(th), nil)
	find(t, sc, "nothing answers here")
	for _, x := range texts(sc) {
		if x == SilentRites || x == SilentRitesWhy[0] {
			t.Errorf("the applications say %q", x)
		}
	}
	// phosphor brackets the count as its badges.
	find(t, DrawTabbed(l, st, size, 0, false, still(phosphor(t)), nil), "[—]")
	// A two-row panel keeps the first lines.
	l.Lines = 2
	st.Active = 1
	sc = DrawTabbed(l, st, size, 0, false, still(th), nil)
	find(t, sc, SilentRites)
	find(t, sc, SilentRitesWhy[0])
	for _, x := range texts(sc) {
		if x == SilentRitesWhy[1] {
			t.Error("a line below a two-row panel")
		}
	}
}
