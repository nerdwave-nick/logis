package window

import "github.com/nerdwave-nick/logis/internal/scale"

// Geometry is what one frame is rendered at.
type Geometry struct {
	// Logical is the surface size in logical pixels (the viewport
	// destination).
	Logical scale.Size
	// Physical is the buffer size in pixels.
	Physical scale.Size
	// Scale relates them.
	Scale scale.Scale
}

// surfaceState is the pure part of a layer surface: what size and scale
// it must render at and when it may draw. Free of Wayland and EGL, so
// every configure, scale change and frame callback sequence is
// unit-testable.
type surfaceState struct {
	requested    scale.Size
	configured   *scale.Size
	scale        scale.Scale
	appliedG     *Geometry
	dirty        bool
	framePending bool
	animating    bool
}

// newSurfaceState is a surface that asked the compositor for requested
// logical pixels.
func newSurfaceState(requested scale.Size) *surfaceState {
	return &surfaceState{requested: requested, scale: scale.One, dirty: true}
}

// request: the surface asked for another size; a configure that leaves a
// side to the client gives this one, and a frame is due.
func (s *surfaceState) request(requested scale.Size) {
	s.requested = requested
	s.dirty = true
}

// configure records a configure event. A zero side means "client's
// choice", so the requested side is used. Every configure asks for a
// redraw.
func (s *surfaceState) configure(given scale.Size) {
	pick := func(given, requested uint32) uint32 {
		if given == 0 {
			return requested
		}
		return given
	}
	c := scale.Size{W: pick(given.W, s.requested.W), H: pick(given.H, s.requested.H)}
	s.configured = &c
	s.dirty = true
}

// setScale records the compositor's preferred scale.
func (s *surfaceState) setScale(sc scale.Scale) {
	if sc != s.scale {
		s.scale = sc
		s.dirty = true
	}
}

// geometry is what the next frame must be rendered at, once configured.
func (s *surfaceState) geometry() (Geometry, bool) {
	if s.configured == nil {
		return Geometry{}, false
	}
	return Geometry{Logical: *s.configured, Physical: s.scale.Physical(*s.configured), Scale: s.scale}, true
}

// pendingReconfigure is the geometry the buffers must be resized to before
// the next frame, if they do not match it yet.
func (s *surfaceState) pendingReconfigure() (Geometry, bool) {
	g, ok := s.geometry()
	if !ok || (s.appliedG != nil && *s.appliedG == g) {
		return Geometry{}, false
	}
	return g, true
}

// applied records that the buffers now match g.
func (s *surfaceState) applied(g Geometry) { s.appliedG = &g }

// invalidate forgets the buffers' geometry (they were lost or replaced).
func (s *surfaceState) invalidate() {
	s.appliedG = nil
	s.dirty = true
}

// requestRedraw asks for one more frame (content changed).
func (s *surfaceState) requestRedraw() { s.dirty = true }

// setAnimating keeps drawing on every frame callback while an animation
// runs.
func (s *surfaceState) setAnimating(animating bool) {
	s.animating = animating
	s.dirty = s.dirty || animating
}

// shouldDraw: configured, something changed, and the compositor is not
// still holding the previous frame.
func (s *surfaceState) shouldDraw() bool {
	return s.configured != nil && s.dirty && !s.framePending
}

// drew: a frame was presented with a frame callback requested.
func (s *surfaceState) drew() {
	s.dirty = false
	s.framePending = true
}

// frameDone: the compositor's frame callback arrived.
func (s *surfaceState) frameDone() {
	s.framePending = false
	s.dirty = s.dirty || s.animating
}
