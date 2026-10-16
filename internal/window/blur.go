package window

import (
	"math"
	"slices"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Blur behind the cogitator (spec §4.2, ext-background-effect-v1, used when
// offered): the compositor blurs what lies behind the panel's body, a
// rounded rectangle. A wl_region is a set of rectangles in whole
// surface-local (logical) pixels, so the rounded corners are stepped one
// logical pixel row at a time, always inside the curve: the blur never
// shows outside the body. The region follows the body as it moves and as
// the surface is resized or rescaled. A compositor without the protocol
// simply gets no blur.

// Backdrop is the area behind a panel to blur: its body, in logical pixels.
type Backdrop struct {
	// Rect is left, top, width, height.
	Rect [4]float32
	// Radius is the corner radius.
	Radius float32
}

// regionRects are the rectangles (x, y, width, height; whole logical
// pixels) that fill b without leaving it: each corner row is inset to
// where the arc runs at that row's outer edge.
func regionRects(b Backdrop) [][4]int32 {
	x, y, w, h := b.Rect[0], b.Rect[1], b.Rect[2], b.Rect[3]
	left, top := ceil(x), ceil(y)
	right, bottom := floor(x+w), floor(y+h)
	if right <= left || bottom <= top {
		return nil
	}
	r := min(max(b.Radius, 0), min(w, h)/2)
	rows := int(min(ceil(r), floor((bottom-top)/2)))
	rect := func(l, t, r, b float32) [4]int32 { return [4]int32{int32(l), int32(t), int32(r - l), int32(b - t)} }
	// Row i's outer edge lies i logical pixels inside the body's edge.
	inset := func(i int) float32 {
		dy := max(r-float32(i), 0)
		return ceil(r - float32(math.Sqrt(float64(max(r*r-dy*dy, 0)))))
	}
	out := make([][4]int32, 0, 2*rows+1)
	for i := range rows {
		d, row := inset(i), top+float32(i)
		out = append(out, rect(left+d, row, right-d, row+1))
	}
	if from, to := top+float32(rows), bottom-float32(rows); to > from {
		out = append(out, rect(left, from, right, to))
	}
	for i := rows - 1; i >= 0; i-- {
		d, row := inset(i), bottom-1-float32(i)
		out = append(out, rect(left+d, row, right-d, row+1))
	}
	kept := out[:0]
	for _, r := range out {
		if r[2] > 0 && r[3] > 0 {
			kept = append(kept, r)
		}
	}
	return kept
}

func ceil(v float32) float32  { return float32(math.Ceil(float64(v))) }
func floor(v float32) float32 { return float32(math.Floor(float64(v))) }

// panelBlur is one surface's blur: its effect object and the region last
// set.
type panelBlur struct {
	effect *wlproto.ExtBackgroundEffectSurfaceV1
	set    [][4]int32
	ok     bool
}

// newPanelBlur is the effect object for surface, with no region yet.
func newPanelBlur(effects *wlproto.ExtBackgroundEffectManagerV1, surface *wlproto.WlSurface) *panelBlur {
	return &panelBlur{effect: effects.GetBackgroundEffect(surface)}
}

// follow sets the blur region to b for the next commit, unless it is
// already that.
func (b *panelBlur) follow(compositor *wlproto.WlCompositor, backdrop Backdrop) {
	rects := regionRects(backdrop)
	if b.ok && slices.Equal(b.set, rects) {
		return
	}
	region := compositor.CreateRegion()
	for _, r := range rects {
		region.Add(r[0], r[1], r[2], r[3])
	}
	b.effect.SetBlurRegion(region)
	region.Destroy()
	emit("blur %d", len(rects))
	b.set, b.ok = rects, true
}

// destroy lets the effect go: the region goes with the next commit (or
// with the surface).
func (b *panelBlur) destroy() { b.effect.Destroy() }
