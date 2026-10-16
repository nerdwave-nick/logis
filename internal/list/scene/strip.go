package scene

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// Plain logis's tab strip (feat/logis-go 8cba2fc9, new beside Rust v1;
// the owner's design, effects lab v6): two equal plates above the panel,
// set apart from it, Applications left and Rites right, fixed in place;
// Tab or a click only moves which is lit. Each carries its tab's count of
// matches; a key hint ends the strip. The panel keeps its own size below
// the strip: the surface grows by StripHeight, and everything of the
// panel (body, rows, band, pointer) lies that much lower.
//
// The plates echo the theme's elements, so a personal theme dresses them
// too: the lit plate is the selection (its band over the panel's fill,
// its edge as frame, its glow or else its outline as halo, its column
// colours for words), an idle plate is the panel's fill framed in the
// rule's colour, dimmed; labels are in the prompt label's font, counts in
// the badges' (bracketed where the badges are), and the icons are drawn
// where the theme shows a sigil. Only the corners are the theme's own
// (tab.radius). The plates are glass for the effects (Post.Regions).

// The plates' sizes, logical pixels (the owner's).
const (
	// PlateWidth and PlateHeight are one plate's (narrower on a body too
	// narrow for two).
	PlateWidth, PlateHeight = 236, 38
	// plateGap is between the plates, plateAbove between them and the body.
	plateGap, plateAbove = 8, 8
	// stripTop is the most of the glow margin that stays above the plates.
	stripTop = 16
)

// TabIcon is a plate's icon.
type TabIcon int

const (
	// AppsIcon is a grid of four squares.
	AppsIcon TabIcon = iota
	// RitesIcon is a cog.
	RitesIcon
)

// Tab is one plate: its name (shown in capitals; "the <name>" in the
// empty tab's hint) and how many of its items the query matches.
type Tab struct {
	Label string
	Count int
	Icon  TabIcon
}

// Strip is the tab strip: the plates, left to right, the lit one, and
// whether the lit tab's list is empty (it then points to the other).
// Measure measures the plates' words in the frame's fonts (nil: their
// widths are estimated).
type Strip struct {
	Tabs    [2]Tab
	Active  int
	Empty   bool
	Measure Measure
}

// StripHeight is what the strip adds above the panel: the plates and
// their gap, less the part of the glow margin they take.
func StripHeight(t *theme.Theme) float32 {
	m := t.Window.Margin
	return ceil(max(PlateHeight+plateAbove+min(m, stripTop)-m, 0))
}

// FitTabbed is FitIn for a panel under the strip: its rows fit the room
// the strip leaves, and the surface is the strip taller.
func FitTabbed(o list.Options, t *theme.Theme, room uint32) (scale.Size, uint32) {
	strip := StripHeight(t)
	if room > 0 {
		room = uint32(max(float32(room)-strip, 1))
	}
	size, rows := FitIn(o, t, room)
	size.H += uint32(strip)
	return size, rows
}

// under is the panel's own surface below the strip.
func under(logical scale.Size, t *theme.Theme) scale.Size {
	return scale.Size{W: logical.W, H: logical.H - min(uint32(StripHeight(t)), logical.H)}
}

// TabbedBody is Body under the strip.
func TabbedBody(logical scale.Size, t *theme.Theme) ([4]float32, float32) {
	b, radius := Body(under(logical, t), t)
	b[1] += StripHeight(t)
	return b, radius
}

// TabbedGrid is GridOf under the strip: the pointer's view of its rows.
func TabbedGrid(logical scale.Size, lines uint32, t *theme.Theme) Grid {
	g := GridOf(under(logical, t), lines, t)
	g.First += StripHeight(t)
	return g
}

// TabbedBandTop is BandTop under the strip.
func TabbedBandTop(l *list.Look, logical scale.Size, t *theme.Theme) (float32, bool) {
	top, ok := BandTop(l, under(logical, t), t)
	return top + StripHeight(t), ok
}

// Plates are the plates on a surface of logical size, left to right: the
// first stands at the body's left edge, plateAbove above it.
func Plates(logical scale.Size, t *theme.Theme) [2]render.Region {
	body, _ := TabbedBody(logical, t)
	w := min(PlateWidth, max((body[2]-plateGap)/2, 1))
	y := body[1] - plateAbove - PlateHeight
	var out [2]render.Region
	for i := range out {
		out[i] = render.Region{Rect: [4]float32{body[0] + float32(i)*(w+plateGap), y, w, PlateHeight}, Radius: t.Tab.Radius}
	}
	return out
}

// PlateAt is the plate under the surface-local point (x, y), if any.
func PlateAt(x, y float64, logical scale.Size, t *theme.Theme) (int, bool) {
	for i, p := range Plates(logical, t) {
		r := p.Rect
		if x >= float64(r[0]) && x < float64(r[0]+r[2]) && y >= float64(r[1]) && y < float64(r[1]+r[3]) {
			return i, true
		}
	}
	return 0, false
}

// DrawTabbed is DrawAt for a panel under the strip (band in surface
// coordinates, as TabbedBandTop gives it), with the strip above.
func DrawTabbed(l *list.Look, st Strip, logical scale.Size, band float32, banded bool, s Style, icons Icons) *render.Scene {
	t := s.Theme
	drop := StripHeight(t)
	sc := DrawAt(l, under(logical, t), band-drop, banded, s, icons)
	motion.Pose{Opacity: 1, Offset: [2]float32{0, drop}}.Apply(sc)
	plates := Plates(logical, t)
	label, count := labelFonts(st, plates[0].Rect, t)
	for i, tab := range st.Tabs {
		plate(sc, plates[i], tab, i == st.Active, label, count, t)
	}
	body, _ := TabbedBody(logical, t)
	keyHint(sc, plates[1].Rect, body, t)
	if st.Empty && l.Lines > 0 {
		emptyTab(sc, st, TabbedGrid(logical, l.Lines, t), t)
	}
	sc.Post.Regions = plates[:]
	return sc
}

// plate draws one plate, lit or idle.
func plate(sc *render.Scene, r render.Region, tab Tab, lit bool, font, countFont theme.Font, t *theme.Theme) {
	x, y, w, h := r.Rect[0], r.Rect[1], r.Rect[2], r.Rect[3]
	vertical := func(stops []render.Color) render.Paint {
		return render.Ramp([2]float32{0, y}, [2]float32{0, y + h}, stops...)
	}
	base := render.RoundedRect{Rect: r.Rect, Radius: r.Radius, Fill: vertical(t.Panel.Fill)}
	sel := t.Selection
	label, count, icon := t.Counter.Color, colourAt(t.Row.Columns, 1), t.Query.Placeholder
	if !lit {
		base.Border = render.Border{Width: 1, Paint: render.Solid(idleFrame(t))}
		sc.Shapes = append(sc.Shapes, base)
	} else {
		switch {
		case sel.Glow.A > 0:
			base.Glow = render.Glow{Radius: 14, Color: sel.Glow}
		case sel.Outline.A > 0:
			base.Glow = render.Glow{Radius: 18, Color: sel.Outline}
		}
		lamp := render.RoundedRect{Rect: r.Rect, Radius: r.Radius, Fill: vertical(sel.Band)}
		if sel.Edge.A > 0 {
			lamp.Border = render.Border{Width: 1, Paint: render.Solid(sel.Edge)}
		}
		sc.Shapes = append(sc.Shapes, base, lamp)
		if hl := t.Border.Highlight; t.Border.HasHighlight {
			sc.Shapes = append(sc.Shapes, render.RoundedRect{
				Rect: [4]float32{x + r.Radius, y + 1, max(w-2*r.Radius, 0), 1}, Fill: render.Solid(hl.Color.WithAlpha(hl.Color.A * 0.6)),
			})
		}
		label, count, icon = colourAt(sel.Columns, 0), colourAt(sel.Columns, 1), t.Prompt.Sigil.Color
	}
	if tab.Count == 0 {
		count = t.Query.Placeholder
	}
	centre, left, right := y+h/2, labelLeft(x, t), x+w-16
	if t.Prompt.HasSigil {
		tabIcon(sc, tab.Icon, x+18, centre, icon)
	}
	words := countWords(tab.Count, t)
	sc.Texts = append(sc.Texts, column(text(strings.ToUpper(tab.Label), left, centre, font, label), right-left, render.Clip))
	n := column(text(words, left, centre, countFont, count), right-left, render.Clip)
	n.AlignEnd = true
	sc.Texts = append(sc.Texts, n)
}

// labelLeft is where a plate's label starts: after its icon, where the
// theme shows a sigil.
func labelLeft(x float32, t *theme.Theme) float32 {
	if t.Prompt.HasSigil {
		return x + 32
	}
	return x + 14
}

// countWords is a plate's count as shown: bracketed where the badges are.
func countWords(n int, t *theme.Theme) string {
	words := strconv.Itoa(n)
	if t.Badge.Style == theme.BadgeBracket {
		words = "[" + words + "]"
	}
	return words
}

// labelFonts are the plates' labels' and counts' fonts: the prompt
// label's, a little smaller (the owner's 12 and 26 beside the bundled
// themes' 13 and 28), and the badges', no larger than the label; both
// smaller still where a plate's label and count would not fit it
// (measured, or estimated at an average capital's advance). The same on
// both plates.
func labelFonts(st Strip, r [4]float32, t *theme.Theme) (theme.Font, theme.Font) {
	label, count := t.Prompt.Font, t.Badge.Font
	label.Size *= 0.93
	count.Size = min(count.Size, label.Size)
	room := max(r[2]-16-(labelLeft(r[0], t)-r[0])-10, 1)
	measure := st.Measure
	if measure == nil {
		measure = NoMeasure
	}
	width := func(words string, f theme.Font, perEm float32) float32 {
		if w, ok := measure.Width(text(words, 0, 0, f, render.Transparent)); ok {
			return w
		}
		return float32(utf8.RuneCountInString(words)) * f.Size * perEm
	}
	labelEm := advance(theme.Font{Size: 1, Tracking: label.Tracking}) * 1.1 // capitals run wide
	countEm := advance(theme.Font{Size: 1, Tracking: count.Tracking})
	fit := float32(1)
	for _, tab := range st.Tabs {
		need := width(strings.ToUpper(tab.Label), label, labelEm) + width(countWords(tab.Count, t), count, countEm)
		fit = min(fit, room/max(need, 1))
	}
	label.Size *= fit
	count.Size = min(count.Size, label.Size)
	return label, count
}

// idleFrame is an idle plate's frame: the rule's strongest colour, dimmed.
func idleFrame(t *theme.Theme) render.Color {
	c := render.Transparent
	for _, s := range t.Rule.Paint {
		if s.A > c.A {
			c = s
		}
	}
	return c.WithAlpha(c.A * 0.4)
}

// colourAt is cs[i], or cs's last colour where it has fewer.
func colourAt(cs []render.Color, i int) render.Color {
	if len(cs) == 0 {
		return render.Transparent
	}
	return cs[min(i, len(cs)-1)]
}

// tabIcon draws a plate's icon centred on (cx, cy).
func tabIcon(sc *render.Scene, icon TabIcon, cx, cy float32, c render.Color) {
	if icon == RitesIcon {
		gear(sc, cx, cy, 18, c)
		return
	}
	for _, d := range [][2]float32{{-5, -5}, {1, -5}, {-5, 1}, {1, 1}} {
		sc.Shapes = append(sc.Shapes, render.RoundedRect{
			Rect: [4]float32{cx + d[0], cy + d[1], 4, 4}, Fill: render.Solid(render.Transparent),
			Border: render.Border{Width: 1.6, Paint: render.Solid(c)},
		})
	}
}

// keyHint ends the strip at the body's right edge with the key that
// switches: a key cap in the badges' style, then "SWITCH"; bracketed
// badges make it one word, "<TAB> SWITCH". Where the plates leave no room
// for it, there is none.
func keyHint(sc *render.Scene, last, body [4]float32, t *theme.Theme) {
	c, b := t.Counter, t.Badge
	right, centre := body[0]+body[2]-4, last[1]+last[3]/2
	words := "SWITCH"
	if b.Style == theme.BadgeBracket {
		words = "<TAB> SWITCH"
	}
	width := float32(utf8.RuneCountInString(words)) * advance(c.Font)
	capW := 3*advance(b.Font) + 2*b.Font.Size*0.55
	need := width
	if b.Style != theme.BadgeBracket {
		need += 10 + capW
	}
	if right-need < last[0]+last[2]+16 {
		return
	}
	hint := column(text(words, right-width, centre, c.Font, c.Color), width, render.Clip)
	hint.AlignEnd = true
	sc.Texts = append(sc.Texts, hint)
	if b.Style == theme.BadgeBracket {
		return
	}
	x, h := right-width-10-capW, b.Font.Size*1.6
	keycap := render.RoundedRect{Rect: [4]float32{x, centre - h/2, capW, h}, Radius: 4, Fill: render.Solid(b.Fill)}
	if b.Style == theme.BadgeFill {
		keycap.Fill = render.Solid(b.Edge)
	} else {
		keycap.Border = render.Border{Width: 1, Paint: render.Solid(b.Edge)}
	}
	sc.Shapes = append(sc.Shapes, keycap)
	sc.Texts = append(sc.Texts, text("TAB", x+b.Font.Size*0.55, centre, b.Font, b.Ink))
}

// emptyTab says in the first row that the lit tab has nothing for the
// query, and how much the other has.
func emptyTab(sc *render.Scene, st Strip, g Grid, t *theme.Theme) {
	words := "nothing answers here"
	spans := []render.Span{{Start: 0, End: len(words), Color: t.Counter.Color}}
	if other := st.Tabs[1-st.Active]; other.Count > 0 {
		dot := " · "
		hint := strconv.Itoa(other.Count) + " among the " + other.Label + " — Tab"
		spans = append(spans,
			render.Span{Start: len(words), End: len(words) + len(dot), Color: t.Query.Placeholder},
			render.Span{Start: len(words) + len(dot), End: len(words) + len(dot) + len(hint), Color: colourAt(t.Row.Columns, 1)})
		words += dot + hint
	}
	left := g.Left + t.Row.Padding
	line := column(text(words, left, g.Top(0)+g.Pitch/2, t.Row.Font, t.Counter.Color), max(g.Right-t.Row.Padding-left, 1), render.Ellipsis)
	line.Spans = spans
	sc.Texts = append(sc.Texts, line)
}
