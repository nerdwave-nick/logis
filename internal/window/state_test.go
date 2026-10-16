package window

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ported from Rust src/window/state.rs.

const s13 = 156

func size(w, h uint32) scale.Size { return scale.Size{W: w, H: h} }

// configured is a surface configured at 640×360 whose first frame was
// drawn and shown: nothing is due.
func configured() *surfaceState {
	s := newSurfaceState(size(640, 360))
	s.configure(size(640, 360))
	g, _ := s.pendingReconfigure()
	s.applied(g)
	s.drew()
	s.frameDone()
	return s
}

func TestANewRequestIsTheSizeAFreeConfigureGives(t *testing.T) {
	s := newSurfaceState(size(100, 50))
	s.configure(size(0, 0))
	s.drew()
	s.request(size(300, 200))
	s.configure(size(0, 0))
	g, _ := s.geometry()
	if g.Logical != size(300, 200) {
		t.Errorf("logical %v", g.Logical)
	}
}

func TestNothingIsDrawnBeforeTheFirstConfigure(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	if s.shouldDraw() {
		t.Error("draws before a configure")
	}
	if _, ok := s.geometry(); ok {
		t.Error("has a geometry before a configure")
	}
	if _, ok := s.pendingReconfigure(); ok {
		t.Error("reconfigures before a configure")
	}
}

func TestTheFirstConfigureDrawsAtTheConfiguredSize(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.configure(size(700, 400))
	if !s.shouldDraw() {
		t.Error("no frame due")
	}
	g, ok := s.pendingReconfigure()
	if !ok || g.Logical != size(700, 400) || g.Physical != size(700, 400) {
		t.Errorf("geometry %+v", g)
	}
}

func TestAZeroSideFallsBackToTheRequestedSide(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.configure(size(0, 500))
	if g, _ := s.geometry(); g.Logical != size(640, 500) {
		t.Errorf("logical %v", g.Logical)
	}
}

func TestAScaleChangeReconfiguresAtTheNewBufferSize(t *testing.T) {
	s := configured()
	if s.shouldDraw() {
		t.Fatal("a frame is due before the scale changed")
	}
	s.setScale(scale.From120ths(s13))
	if !s.shouldDraw() {
		t.Error("no frame due after the scale changed")
	}
	g, ok := s.pendingReconfigure()
	if !ok || g.Physical != size(832, 468) || g.Logical != size(640, 360) {
		t.Errorf("geometry %+v", g)
	}
}

func TestAnUnchangedScaleNeitherRedrawsNorReconfigures(t *testing.T) {
	s := configured()
	s.setScale(scale.One)
	if s.shouldDraw() {
		t.Error("redraws")
	}
	if _, ok := s.pendingReconfigure(); ok {
		t.Error("reconfigures")
	}
}

func TestANewConfigureAfterAnOutputChangeLeavesNoStaleSize(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.setScale(scale.FromInteger(2))
	s.configure(size(640, 360))
	g, _ := s.pendingReconfigure()
	s.applied(g)
	// Moved to a 1.3 output that also resized the surface.
	s.setScale(scale.From120ths(s13))
	s.configure(size(500, 300))
	g, ok := s.pendingReconfigure()
	if !ok || g.Logical != size(500, 300) || g.Physical != size(650, 390) {
		t.Errorf("geometry %+v", g)
	}
}

func TestAPendingFrameHoldsBackTheNextDraw(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.configure(size(640, 360))
	s.drew()
	s.requestRedraw()
	if s.shouldDraw() {
		t.Error("draws while the compositor holds the last frame")
	}
	s.frameDone()
	if !s.shouldDraw() {
		t.Error("the requested redraw was lost")
	}
}

func TestAnIdleSurfaceDrawsNothingAfterItsFrame(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.configure(size(640, 360))
	s.drew()
	s.frameDone()
	if s.shouldDraw() {
		t.Error("draws while idle")
	}
}

func TestAnAnimationDrawsOnEveryFrameCallbackUntilItStops(t *testing.T) {
	s := newSurfaceState(size(640, 360))
	s.configure(size(640, 360))
	s.setAnimating(true)
	s.drew()
	s.frameDone()
	if !s.shouldDraw() {
		t.Error("the animation stalled")
	}
	s.drew()
	s.setAnimating(false)
	s.frameDone()
	if s.shouldDraw() {
		t.Error("draws after the animation stopped")
	}
}

func TestAnInvalidatedBufferIsReconfiguredAndRedrawn(t *testing.T) {
	s := configured()
	s.invalidate()
	if !s.shouldDraw() {
		t.Error("no frame due")
	}
	if _, ok := s.pendingReconfigure(); !ok {
		t.Error("no reconfigure due")
	}
}
