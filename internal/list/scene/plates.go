package scene

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// How the tab strip's parts are drawn (strip.go): the plates, the key
// hint, the empty tab's line and silent rites' words.

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
	words := countWords(tab, t)
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

// countWords is a plate's count as shown, "—" for a silent tab:
// bracketed where the badges are.
func countWords(tab Tab, t *theme.Theme) string {
	words := strconv.Itoa(tab.Count)
	if tab.Silent {
		words = "—"
	}
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
		need := width(strings.ToUpper(tab.Label), label, labelEm) + width(countWords(tab, t), count, countEm)
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

// silentTab says in the first row that the lit tab is silent, and why in
// the rows below it, as far as the panel has rows.
func silentTab(sc *render.Scene, g Grid, lines uint32, t *theme.Theme) {
	cause, _, _ := strings.Cut(SilentRites, " — ")
	dash := " — "
	spans := []render.Span{
		{Start: 0, End: len(cause), Color: t.Counter.Color},
		{Start: len(cause), End: len(cause) + len(dash), Color: t.Query.Placeholder},
		{Start: len(cause) + len(dash), End: len(SilentRites), Color: colourAt(t.Row.Columns, 1)},
	}
	left := g.Left + t.Row.Padding
	room := max(g.Right-t.Row.Padding-left, 1)
	line := column(text(SilentRites, left, g.Top(0)+g.Pitch/2, t.Row.Font, t.Counter.Color), room, render.Ellipsis)
	line.Spans = spans
	sc.Texts = append(sc.Texts, line)
	for i, words := range SilentRitesWhy {
		if row := i + 1; uint32(row) < lines {
			sc.Texts = append(sc.Texts, column(text(words, left, g.Top(row)+g.Pitch/2, t.Row.Font, t.Query.Placeholder), room, render.Ellipsis))
		}
	}
}
