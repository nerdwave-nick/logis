// Package render is logis's own GPU renderer (spec §5, D1; Rust
// src/render): SDF rounded rectangles with borders, glows and gradients
// of up to four stops, and image quads from an atlas, drawn with OpenGL
// ES 3 in premultiplied alpha. Shaders are GLSL ES 3.00 ports of Rust's
// sdf.wgsl and image.wgsl, compiled by the driver when the renderer is
// made (at start, never lazily on the first frame).
//
// A Renderer belongs to the goroutine its GL context is current on (the
// window's locked owner goroutine, or a Headless one); its GL objects are
// released by Close, never by finalizers. Drawing a frame never waits for
// the GPU: only the offscreen readback does.
package render

// Color is an sRGB colour with straight (not premultiplied) alpha,
// channels in 0..1. The GPU sees it premultiplied.
type Color struct{ R, G, B, A float32 }

// Transparent is no colour at all.
var Transparent = Color{}

// RGBA is a colour from channels in 0..1.
func RGBA(r, g, b, a float32) Color { return Color{r, g, b, a} }

// Hex is an opaque colour from 0xRRGGBB.
func Hex(rgb uint32) Color {
	return Color{
		float32((rgb>>16)&0xff) / 255,
		float32((rgb>>8)&0xff) / 255,
		float32(rgb&0xff) / 255,
		1,
	}
}

// WithAlpha is the same colour at another opacity.
func (c Color) WithAlpha(a float32) Color { c.A = a; return c }

// Premultiplied is the colour as the shaders take it: channels clamped to
// 0..1 and multiplied by the (clamped) alpha.
func (c Color) Premultiplied() [4]float32 {
	a := clamp01(c.A)
	return [4]float32{clamp01(c.R) * a, clamp01(c.G) * a, clamp01(c.B) * a, a}
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }

// MaxStops is the most stops a Paint holds (what the shader takes).
const MaxStops = 4

// Paint is how an area is painted: one colour, or a linear gradient through
// two to MaxStops evenly spaced stops from Start to End (logical pixels,
// surface coordinates), interpolated premultiplied and clamped beyond its
// ends. Rust's Paint::Solid, Paint::Linear and Paint::Ramp in one type.
type Paint struct {
	// Start and End are where the first and the last stop are reached
	// (unused by a solid paint).
	Start, End [2]float32
	stops      [MaxStops]Color
	n          int
}

// Solid paints one colour.
func Solid(c Color) Paint { return Paint{stops: [MaxStops]Color{c}, n: 1} }

// Linear is a gradient from `from` at start to `to` at end.
func Linear(start, end [2]float32, from, to Color) Paint {
	return Paint{Start: start, End: end, stops: [MaxStops]Color{from, to}, n: 2}
}

// Ramp is a gradient from start to end through the first MaxStops of
// stops, evenly spaced: one stop is Solid, two are Linear, none is
// Transparent.
func Ramp(start, end [2]float32, stops ...Color) Paint {
	switch len(stops) {
	case 0:
		return Solid(Transparent)
	case 1:
		return Solid(stops[0])
	}
	p := Paint{Start: start, End: end, n: min(len(stops), MaxStops)}
	copy(p.stops[:], stops)
	return p
}

// Stops are the paint's colours in order (one for a solid paint). The zero
// Paint is Transparent.
func (p Paint) Stops() []Color {
	if p.n == 0 {
		return []Color{Transparent}
	}
	return p.stops[:p.n]
}

// Border is a border drawn inside a shape's edge; a zero Width is none.
type Border struct {
	// Width in logical pixels.
	Width     float32
	Paint     Paint
	Highlight Highlight
}

// Highlight is a bright spot on a border at an angle around the shape's
// centre (the travelling highlight: its owner turns the angle over time).
// A transparent Color is none.
type Highlight struct {
	// Angle in radians: 0 is the right, turning clockwise (y points down),
	// so -π/2 is the top. Measured on the shape's box squashed to a square,
	// so it sweeps a wide box evenly.
	Angle float32
	// Reach is how far it reaches along the border either side of its
	// centre, in logical pixels.
	Reach float32
	// Color at the centre, fading to nothing at its reach.
	Color Color
}

// Glow is a soft glow outside a shape's edge, fading to nothing at Radius
// logical pixels; a zero Radius is none.
type Glow struct {
	Radius float32
	// Color at the edge; its alpha is the glow's strength.
	Color Color
}

// RoundedRect is a rounded rectangle with an optional border and glow, in
// logical pixels.
type RoundedRect struct {
	// Rect is left, top, width, height.
	Rect [4]float32
	// Radius is the corner radius, clamped to half the shorter side.
	Radius float32
	Fill   Paint
	Border Border
	Glow   Glow
}

// ImageQuad is an atlas image drawn texel for texel at the pixel size it
// was rasterised for, its top-left corner snapped to the nearest buffer
// pixel so it stays crisp at fractional scales.
type ImageQuad struct {
	Image ImageID
	// Origin is the top-left corner in logical pixels.
	Origin [2]float32
	// Opacity, 0 to 1 (a fading window's images fade too).
	Opacity float32
}

// Scene is everything one frame shows: a clear colour, then shapes, then
// images, each back to front. Text (a later slice) goes on top of both.
type Scene struct {
	// Clear is the colour the frame starts from (transparent for a layer
	// surface).
	Clear  Color
	Shapes []RoundedRect
	Images []ImageQuad
	// Texts are drawn last, over the shapes and images.
	Texts []Text
	// Post is the post-processing pass over the finished frame, if any
	// effect is on (Post.Any).
	Post Post
}

// instanceFloats is floats per shape instance: fifteen vec4 attributes.
const instanceFloats = 60

// pack lays a shape out as sdf's instance attributes: rect; radius, border
// width, glow radius, highlight angle; four fill stops and the fill line;
// four border stops and the border line; glow; highlight colour; fill stop
// count, border stop count, highlight width (radians).
func pack(shape RoundedRect) [instanceFloats]float32 {
	x, y := shape.Rect[0], shape.Rect[1]
	w, h := max(shape.Rect[2], 0), max(shape.Rect[3], 0)
	halfShort := min(w, h) / 2
	radius := min(max(shape.Radius, 0), halfShort)
	border := shape.Border
	borderWidth := min(max(border.Width, 0), halfShort)
	// Without a highlight: angle 0, width 1, transparent.
	angle, width, highlight := float32(0), float32(1), Transparent
	if hl := border.Highlight; hl.Color.A > 0 {
		// The reach along the edge as an angle on the squashed box: the
		// mean half extent stands in for the radius.
		angle, width, highlight = hl.Angle, max(hl.Reach, 0)/max((w+h)/4, 1), hl.Color
	}

	var out [instanceFloats]float32
	copy(out[0:4], []float32{x, y, w, h})
	copy(out[4:8], []float32{radius, borderWidth, max(shape.Glow.Radius, 0), angle})
	fill, fillCount := packPaint(shape.Fill)
	copy(out[8:28], fill[:])
	edge, edgeCount := packPaint(border.Paint)
	copy(out[28:48], edge[:])
	glow := shape.Glow.Color.Premultiplied()
	copy(out[48:52], glow[:])
	hl := highlight.Premultiplied()
	copy(out[52:56], hl[:])
	copy(out[56:60], []float32{fillCount, edgeCount, width, 0})
	return out
}

// packPaint is a paint as four premultiplied stops and its line, and how
// many of the stops it uses.
func packPaint(p Paint) ([20]float32, float32) {
	var out [20]float32
	stops := p.Stops()
	for i, stop := range stops {
		c := stop.Premultiplied()
		copy(out[i*4:i*4+4], c[:])
	}
	if len(stops) > 1 {
		copy(out[16:20], []float32{p.Start[0], p.Start[1], p.End[0], p.End[1]})
	}
	return out, float32(len(stops))
}
