package render

// Post is the post-processing pass over a finished frame (Rust Post):
// effects within a rounded rectangle (the panel's body), each a strength
// from 0 (off) to 1. Outside it the frame stays as drawn (the glow).
type Post struct {
	// Rect is the area the effects cover: left, top, width, height
	// (logical); Radius its corner radius.
	Rect   [4]float32
	Radius float32
	// Scanlines are dark lines every three logical pixels; Grain film
	// grain; Flicker the brightness trembling over time; Vignette darkened
	// edges; Bloom bright parts bleeding light; Curvature curved glass;
	// Aberration red and blue apart at edges.
	Scanlines, Grain, Flicker, Vignette, Bloom, Curvature, Aberration float32
	// Time is seconds, for grain and flicker: a frozen time gives a frozen
	// frame.
	Time float32
	// Inset is the edge kept straight inside the area (the panel's
	// border): curved glass bows only what lies within it (logical).
	Inset float32
}

// Any reports whether any effect is on, so the pass runs at all.
func (p Post) Any() bool {
	for _, s := range []float32{p.Scanlines, p.Grain, p.Flicker, p.Vignette, p.Bloom, p.Curvature, p.Aberration} {
		if s > 0 {
			return true
		}
	}
	return false
}
