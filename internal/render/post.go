package render

import "github.com/nerdwave-nick/logis/internal/effects"

// MaxRegions is the most screen regions a Post takes besides the body.
const MaxRegions = 4

// Post is the post-processing over a finished frame: the glitch pass
// (Glitch), then the CRT pass's effects (effects.CRT, every control the
// theme sets), on the panel's body and on up to MaxRegions further screens
// of glass (the tab strip's plates). Outside them the frame stays as drawn (the glow), bent with the
// panel when it is curved. Rust v1 had seven strengths here; the named
// controls diverge from it on purpose (cd816303).
type Post struct {
	// Rect is the body the effects cover: left, top, width, height
	// (logical); Radius its corner radius.
	Rect   [4]float32
	Radius float32
	// Inset is the edge inside the body that is not glass (the panel's
	// border; logical): the tube corners round the glass within it.
	Inset float32
	// Regions are further screens the effects cover as they cover the
	// body, in the body's phase (scanlines, mask and grain run on from
	// it); the first MaxRegions count.
	Regions []Region
	// CRT are the effects; start from effects.Defaults, whose controls
	// reproduce v1's look, and turn strengths up.
	CRT effects.CRT
	// Time is seconds, for whatever moves (grain, flicker, roll, wobble):
	// a frozen time gives a frozen frame.
	Time float32
	// Glitch is the glitch pass's controls (33cfcf44, new beside Rust v1):
	// it runs before the CRT pass on the same screens, and not at all at a
	// strength of 0. Moment is where it stands: the seconds since the
	// panel opened and the burst intensity (internal/glitch); a frozen
	// moment gives a frozen frame. A later pass that wants the glitch's
	// intensity takes Moment.Event (the burst) or Glitch.Intensity(Moment).
	Glitch effects.Glitch
	Moment effects.Moment
}

// Region is a rounded rectangle of glass: left, top, width, height and
// corner radius, logical pixels.
type Region struct {
	Rect   [4]float32
	Radius float32
}

// Any reports whether any effect is on, so a pass runs at all.
func (p Post) Any() bool { return p.CRT.Any() || p.Glitch.On() }
