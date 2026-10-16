package scene

import (
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The panel's height follows its rows, with no empty band below the last
// row: the prompt line, the rule, a gap, the rows, a gap and the glow
// margins, exactly. On an output too short for that many rows, the panel
// takes the output's height, shows the rows that fit and spreads the
// leftover height between them.

// bodyBottomGap is the distance from the last row's bottom to the body's
// bottom on a surface of logical size showing lines rows.
func bodyBottomGap(logical scale.Size, lines uint32, th *theme.Theme) float32 {
	b, _ := Body(logical, th)
	g := GridOf(logical, lines, th)
	return b[1] + b[3] - g.Top(g.Rows)
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestThePanelsHeightFollowsTheThemesRows(t *testing.T) {
	for _, name := range theme.Bundled() {
		th, err := theme.Themes{}.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		size, rows := Fit(list.DefaultOptions(), &th)
		chrome := th.Prompt.Height + th.Rule.Thickness + 2*th.Row.Gap + 2*th.Window.Margin
		want := scale.Size{W: uint32(th.Window.Width + 2*th.Window.Margin), H: uint32(chrome + 11*th.Row.Height)}
		if rows != 11 || size != want {
			t.Fatalf("%s: %v with %d rows, want %v with 11", name, size, rows, want)
		}
		g := GridOf(size, rows, &th)
		if g.Rows != 11 || g.Pitch != th.Row.Height {
			t.Fatalf("%s: %d rows at %v", name, g.Rows, g.Pitch)
		}
		// Nothing but the theme's gap below the last row.
		if gap := bodyBottomGap(size, rows, &th); !near(gap, th.Row.Gap) {
			t.Fatalf("%s: %v below the last row, want the gap %v", name, gap, th.Row.Gap)
		}
	}
}

func TestLinesSetTheRowsAndSoTheHeight(t *testing.T) {
	th := forge()
	for _, lines := range []uint32{1, 4, 11, 30} {
		size, rows := Fit(opts(u(lines), nil), th)
		want := uint32(2*th.Window.Margin + th.Prompt.Height + th.Rule.Thickness + 2*th.Row.Gap + float32(lines)*th.Row.Height)
		if rows != lines || size.H != want {
			t.Fatalf("--lines %d: %v with %d rows, want height %d", lines, size, rows, want)
		}
		if gap := bodyBottomGap(size, rows, th); !near(gap, th.Row.Gap) {
			t.Fatalf("--lines %d: %v below the last row", lines, gap)
		}
	}
}

func TestAnOutputTallEnoughChangesNothing(t *testing.T) {
	th := forge()
	free, freeRows := Fit(list.DefaultOptions(), th)
	for _, room := range []uint32{free.H, free.H + 1, 1080} {
		if size, rows := FitIn(list.DefaultOptions(), th, room); size != free || rows != freeRows {
			t.Fatalf("on %d: %v with %d rows, want %v with %d", room, size, rows, free, freeRows)
		}
	}
}

func TestAShortOutputShowsTheRowsThatFitSpreadOverItsHeight(t *testing.T) {
	th := forge()
	// forge: margins 2·32, prompt 54, rule 1, gaps 2·12, rows 38.
	// 400 leaves 400 − 64 − 55 − 24 = 257 for the rows: 6 fit (228), and
	// the 29 left over are spread between them.
	size, rows := FitIn(list.DefaultOptions(), th, 400)
	if rows != 6 || size != (scale.Size{W: 884, H: 400}) {
		t.Fatalf("%v with %d rows, want 884x400 with 6", size, rows)
	}
	g := GridOf(size, rows, th)
	if g.Rows != 6 || !near(g.Pitch, 257.0/6) {
		t.Fatalf("%d rows at %v, want 6 at %v", g.Rows, g.Pitch, 257.0/6)
	}
	if gap := bodyBottomGap(size, rows, th); !near(gap, th.Row.Gap) {
		t.Fatalf("%v below the last row, want the gap", gap)
	}
	// --lines too: as many as fit.
	if size, rows := FitIn(opts(u(30), nil), th, 400); rows != 6 || size.H != 400 {
		t.Fatalf("--lines 30 on 400: %v with %d rows", size, rows)
	}
	// Fewer rows asked than fit: no spreading, no output height.
	if size, rows := FitIn(opts(u(3), nil), th, 400); rows != 3 || size.H != uint32(64+55+24+3*38) {
		t.Fatalf("--lines 3 on 400: %v with %d rows", size, rows)
	}
}

func TestAnOutputTooShortForOneRowStillShowsOne(t *testing.T) {
	th := forge()
	size, rows := FitIn(list.DefaultOptions(), th, 100)
	if rows != 1 || size.H != uint32(64+55+24+38) {
		t.Fatalf("%v with %d rows", size, rows)
	}
}

func TestThePromptLineAloneIgnoresTheOutput(t *testing.T) {
	th := forge()
	free, _ := Fit(opts(u(0), nil), th)
	if size, rows := FitIn(opts(u(0), nil), th, 100); size != free || rows != 0 {
		t.Fatalf("%v with %d rows, want %v", size, rows, free)
	}
}

// The pointer finds rows at the spread pitch: the middle of each row, and
// the bottom of the last one, which at the theme's row height would be
// below it.
func TestThePointerFindsRowsAtTheSpreadPitch(t *testing.T) {
	th := forge()
	size, rows := FitIn(list.DefaultOptions(), th, 400)
	g := GridOf(size, rows, th)
	first, pitch := float64(32+55+12), 257.0/6
	for i := range int(rows) {
		if row, ok := g.RowAt(100, first+(float64(i)+0.5)*pitch); !ok || row != i {
			t.Fatalf("the middle of row %d: %d %v", i, row, ok)
		}
	}
	// Just above the gap under the last row.
	if row, ok := g.RowAt(100, 400-32-12-0.5); !ok || row != int(rows)-1 {
		t.Fatalf("the last row's bottom: %d %v", row, ok)
	}
	if !near(g.RowPitch(), float32(pitch)) {
		t.Fatalf("rows pitch %v", g.RowPitch())
	}
}

// The band and the rows lie on the spread pitch too.
func TestTheBandAndTheRowsFollowTheSpreadPitch(t *testing.T) {
	th := forge()
	size, rows := FitIn(list.DefaultOptions(), th, 400)
	l := look(int(rows), int(rows)-1)
	l.Lines = rows
	s := Draw(l, size, still(th))
	pitch := float32(257.0 / 6)
	top := float32(32+55+12) + 5*pitch
	band, ok := BandTop(l, size, th)
	if !ok || !near(band, top) {
		t.Fatalf("band at %v, want %v", band, top)
	}
	var found bool
	for _, r := range s.Shapes {
		if near(r.Rect[1], top+1) && near(r.Rect[3], pitch-2) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no band %v tall at %v", pitch-2, top+1)
	}
	last := find(t, s, "row 5")
	if centre := last.Origin[1] + last.LineHeight/2; !near(centre, top+pitch/2) {
		t.Fatalf("the last row's centre %v, want %v", centre, top+pitch/2)
	}
}

// Only the leftover of a full panel is spread: on a surface with room for
// more rows than asked (a compositor's larger configure, or a fixed window)
// the rows keep the theme's height.
func TestFewerRowsThanFitKeepTheThemesPitch(t *testing.T) {
	th := forge()
	fixed := scale.Size{W: 884, H: 584} // an 820×520 body showing nine rows
	if g := GridOf(fixed, 9, th); g.Rows != 9 || g.Pitch != th.Row.Height {
		t.Fatalf("%d rows at %v", g.Rows, g.Pitch)
	}
}
