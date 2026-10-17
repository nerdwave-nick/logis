package scene

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// A toast shows its whole text: it wraps onto as many lines as it needs, at
// most four (the fourth then ends in "…"), its strip growing upward over
// the rows, which are hidden where it covers them.

// twentyWide measures every character a twentieth of a toast's line:
// twenty characters fill it. calls counts its measures.
type twentyWide struct {
	per   float32
	calls int
}

func newTwentyWide(th *theme.Theme, logical scale.Size) *twentyWide {
	body, _ := Body(logical, th)
	return &twentyWide{per: (body[2] - 2*toastInset - 24) / 20}
}

func (w *twentyWide) Width(t render.Text) (float32, bool) {
	w.calls++
	return float32(utf8.RuneCountInString(t.Text)) * w.per, true
}

// litany is n words "rite", one space apart: four to a line of twenty.
func litany(n int) string { return strings.TrimSpace(strings.Repeat("rite ", n)) }

// toastTexts are the toast's lines in s, top first (drawn last, inside
// its strip, the last shape), and its strip.
func toastTexts(s *render.Scene) ([]render.Text, [4]float32) {
	strip := s.Shapes[len(s.Shapes)-1].Rect
	var out []render.Text
	for _, x := range s.Texts {
		if x.Origin[0] == strip[0]+12 && x.Origin[1] >= strip[1] {
			out = append(out, x)
		}
	}
	return out, strip
}

// rowsShown counts the rows drawn in s.
func rowsShown(s *render.Scene) int {
	return len(slices.DeleteFunc(slices.Clone(s.Texts), func(x render.Text) bool { return !strings.HasPrefix(x.Text, "row ") }))
}

func fitted(th *theme.Theme, logical scale.Size, l *list.Look, m Measure) *render.Scene {
	(&ToastFit{}).Fit(l, logical, th, m)
	return Draw(l, logical, still(th))
}

func TestALongToastWrapsOntoAsManyLinesAsItNeedsGrowingUpward(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 0)
	l.Toast = &list.Notice{Text: litany(10), Level: list.Error}
	lines, strip := toastTexts(fitted(th, logical, l, newTwentyWide(th, logical)))
	got := make([]string, len(lines))
	for i, x := range lines {
		got[i] = x.Text
	}
	if !slices.Equal(got, []string{litany(4), litany(4), litany(2)}) {
		t.Fatalf("lines %q", got)
	}
	body, _ := Body(logical, th)
	pitch := th.Row.Font.Size * 1.3
	if bottom := strip[1] + strip[3]; bottom != body[1]+body[3]-toastInset {
		t.Errorf("strip bottom %v, want it where a one-line toast's is", bottom)
	}
	if strip[3] != 3*pitch+12 {
		t.Errorf("strip height %v, want %v", strip[3], 3*pitch+12)
	}
	for i, x := range lines {
		if want := strip[1] + 6 + pitch*float32(i) + pitch/2; x.Origin[1]+x.LineHeight/2 != want {
			t.Errorf("line %d centred on %v, want %v", i, x.Origin[1]+x.LineHeight/2, want)
		}
		if !x.Column || x.Width != strip[2]-24 || x.Overflow != render.Ellipsis {
			t.Errorf("line %d: column %v %v %v", i, x.Column, x.Width, x.Overflow)
		}
	}
}

func TestAToastNeedingMoreThanFourLinesEndsItsFourthInAnEllipsis(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 0)
	l.Toast = &list.Notice{Text: litany(30), Level: list.Info}
	lines, strip := toastTexts(fitted(th, logical, l, newTwentyWide(th, logical)))
	if len(lines) != 4 || lines[3].Text != litany(18) || lines[3].Overflow != render.Ellipsis {
		t.Fatalf("%+v", lines)
	}
	if strip[3] != 4*th.Row.Font.Size*1.3+12 {
		t.Errorf("strip height %v", strip[3])
	}
}

func TestALineBreakInAToastStartsANewLine(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 0)
	l.Toast = &list.Notice{Text: "the config is heretical:\nline 3", Level: list.Error}
	lines, _ := toastTexts(fitted(th, logical, l, newTwentyWide(th, logical)))
	var got []string
	for _, x := range lines {
		got = append(got, x.Text)
	}
	if !slices.Equal(got, []string{"the config is", "heretical:", "line 3"}) {
		t.Fatalf("%q", got)
	}
}

// Every row whose line a wrapped toast would cover is hidden while it
// shows.
func TestRowsUnderAWrappedToastAreHidden(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(5, 0)
	l.Toast = &list.Notice{Text: litany(10), Level: list.Error}
	s := fitted(th, logical, l, newTwentyWide(th, logical))
	_, strip := toastTexts(s)
	for _, x := range s.Texts {
		if strings.HasPrefix(x.Text, "row ") && x.Origin[1]+x.LineHeight > strip[1] {
			t.Errorf("%q reaches into the toast", x.Text)
		}
	}
	one := *l
	one.Toast = &list.Notice{Text: "rite", Level: list.Error}
	if shown, oneShown := rowsShown(s), rowsShown(fitted(th, logical, &one, newTwentyWide(th, logical))); shown >= oneShown {
		t.Errorf("%d rows shown under a three-line toast, %d under one line", shown, oneShown)
	}
}

// The strip never rises above the rule under the prompt: a short panel
// gives a toast fewer lines, its last ending in "…".
func TestAToastNeverRisesAboveTheList(t *testing.T) {
	th := forge()
	logical, _ := Fit(opts(u(1), u(40)), th)
	l := look(1, 0)
	l.Lines = 1
	l.Toast = &list.Notice{Text: litany(30), Level: list.Error}
	lines, strip := toastTexts(fitted(th, logical, l, newTwentyWide(th, logical)))
	body, _ := Body(logical, th)
	if strip[1] < body[1]+listTop(th) {
		t.Errorf("strip top %v above the list's %v", strip[1], body[1]+listTop(th))
	}
	if n := len(lines); n == 0 || n >= 4 || lines[n-1].Overflow != render.Ellipsis ||
		!strings.HasSuffix(lines[n-1].Text, litany(1)) || len(lines[n-1].Text) < len(litany(5)) {
		t.Errorf("%+v", lines)
	}
}

// A toast is measured once for its text, the panel's width and the font,
// not on every frame; unmeasured (no fonts yet), it is estimated and
// measured once fonts come.
func TestAToastIsMeasuredOnceNotEveryFrame(t *testing.T) {
	th := forge()
	logical := sized(th)
	m := newTwentyWide(th, logical)
	var f ToastFit
	l := look(3, 0)
	l.Toast = &list.Notice{Text: litany(10)}
	f.Fit(l, logical, th, NoMeasure)
	if !l.ToastFitted || len(l.ToastBreaks) == 0 {
		t.Fatalf("estimated: %v %v", l.ToastFitted, l.ToastBreaks)
	}
	f.Fit(l, logical, th, m)
	first := m.calls
	if first == 0 || !slices.Equal(l.ToastBreaks, []int{20, 40}) {
		t.Fatalf("measured %d times: %v", first, l.ToastBreaks)
	}
	again := look(3, 0)
	again.Toast = &list.Notice{Text: litany(10), Level: list.Error}
	f.Fit(again, logical, th, m)
	if m.calls != first || !slices.Equal(again.ToastBreaks, []int{20, 40}) {
		t.Errorf("measured again: %d calls, %v", m.calls-first, again.ToastBreaks)
	}
	again.Toast = &list.Notice{Text: litany(5)}
	f.Fit(again, logical, th, m)
	if m.calls == first || !slices.Equal(again.ToastBreaks, []int{20}) {
		t.Errorf("a new text: %d calls, %v", m.calls-first, again.ToastBreaks)
	}
	again.Toast = nil
	f.Fit(again, logical, th, m)
	if again.ToastFitted || again.ToastBreaks != nil {
		t.Error("no toast, yet fitted")
	}
}

// Without a fit (or fonts), the scene estimates the lines from the
// characters' average advance.
func TestAnUnfittedToastWrapsByEstimate(t *testing.T) {
	th := forge()
	logical := sized(th)
	l := look(3, 0)
	l.Toast = &list.Notice{Text: strings.Repeat("the machine spirit stirs ", 8), Level: list.Info}
	lines, _ := toastTexts(Draw(l, logical, still(th)))
	if len(lines) < 2 {
		t.Fatalf("%+v", lines)
	}
}
