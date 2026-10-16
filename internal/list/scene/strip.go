package scene

import (
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
// empty tab's hint) and how many of its items the query matches. Silent:
// the tab has no provider to ask (the rites where its caller found no
// servitor): its plate counts "—", and the tab says why.
type Tab struct {
	Label  string
	Count  int
	Icon   TabIcon
	Silent bool
}

// SilentRites is what silent rites' tab says in its first row, and
// SilentRitesWhy in the rows below it: the cause, plainly enough to act
// on (logis started from a keybind has the compositor's PATH).
const SilentRites = "cannot find servitor — the rites are silent"

var SilentRitesWhy = [2]string{
	"no servitor on the PATH logis was summoned with",
	"a keybind's PATH can differ from your shell's",
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
	switch {
	case l.Lines == 0:
	case st.Tabs[st.Active].Silent:
		silentTab(sc, TabbedGrid(logical, l.Lines, t), l.Lines, t)
	case st.Empty:
		emptyTab(sc, st, TabbedGrid(logical, l.Lines, t), t)
	}
	sc.Post.Regions = plates[:]
	return sc
}
