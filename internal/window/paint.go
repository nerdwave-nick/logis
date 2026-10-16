package window

import (
	"errors"
	"fmt"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Frame is the frame a FramePainter draws: its geometry, when it is
// expected on screen (the time to evaluate motion at) and the interval
// between frames, and the pose of the panel's open or close transition.
type Frame struct {
	Geometry
	FrameTime
	// Pose is the transition's pose: the painter moves and fades its scene
	// into it (motion.Pose.Apply).
	Pose motion.Pose
}

// Drawing is what a FramePainter reports of its frame.
type Drawing struct {
	// Moving: the content itself still moves (the selection gliding, a
	// continuous effect), so the next frame callback must draw again.
	Moving bool
	// Backdrop is the panel's body at rest, blurred behind (moved with the
	// pose) when blur is on and offered; nil blurs nothing new.
	Backdrop *Backdrop
}

// FramePainter draws one frame into the current GL context (its viewport
// already covers the buffer), in the frame's pose.
type FramePainter func(f Frame) Drawing

// emit writes a trace event (a seam for tests).
var emit = trace.Emit

// draw draws the panel closing (or lets it go once it has faded) and the
// visible panel, if a frame is due; the panel a Show cut short goes once
// the visible one's first frame is out. A frame that fails because the
// connection broke under EGL is the compositor's loss; a closing panel
// that fails otherwise just goes.
func (w *Window) draw() error {
	if c := w.closing; c != nil {
		if c.transition.Finished(w.clock.frame(w.now()).At) {
			w.dropClosing()
		} else if err := w.drawPanel(c); err != nil {
			var surface *SurfaceError
			if !errors.As(err, &surface) {
				return err
			}
			w.dropClosing()
		}
	}
	if w.panel == nil {
		return nil
	}
	if err := w.drawPanel(w.panel); err != nil {
		return err
	}
	if w.panel.drawnOnce {
		w.cutRetired()
	}
	return nil
}

// drawPanel draws p if a frame is due; the visible panel's buffer
// (re)configurations are logged.
func (w *Window) drawPanel(p *panel) error {
	if !p.state.shouldDraw() {
		return nil
	}
	g, reconfigured := p.state.pendingReconfigure()
	if reconfigured {
		p.reconfigure(g)
	}
	if err := w.present(p); err != nil {
		if lost := w.conn.display.Err(); lost != nil {
			return disconnected(lost)
		}
		return &SurfaceError{Reason: err.Error()}
	}
	if reconfigured && p == w.panel {
		_, _ = fmt.Fprintf(w.log, "logis: cogitator glass %dx%d px for %dx%d logical at scale %g (EGL Wayland platform)\n",
			g.Physical.W, g.Physical.H, g.Logical.W, g.Logical.H, g.Scale.Factor())
	}
	return nil
}

// present draws one frame of p at its moment and presents it with a frame
// callback (and presentation feedback, when offered) requested, so the
// next frame waits for the compositor. The first frame makes the EGL
// surface, at its own buffer size: Mesa defers a wl_egl_window resize
// while it holds a back buffer, so a surface made at the requested size
// and resized before its first frame could draw that frame at the old
// size.
//
// With the test trace on it reports `commit` (the visible panel) or
// `commit closing`, and `settled` after the first frame of the visible
// panel that leaves nothing moving; a visible frame's callback, when it
// comes, `frame-done`.
func (w *Window) present(p *panel) error {
	g, _ := p.state.geometry()
	if p.presenter == nil {
		var err error
		if p.presenter, err = p.gpu.newSurface(p.surface, g.Physical); err != nil {
			return err
		}
	}
	if err := p.presenter.makeCurrent(); err != nil {
		return err
	}
	gles.Viewport(0, 0, int(g.Physical.W), int(g.Physical.H))
	// Timed once the surface is ready, which may have waited (the first
	// frame makes it), so motion starts when drawing does.
	closing := p.transition.IsClosing()
	at := w.clock.frame(w.now())
	pose := p.transition.Pose(at.At, at.Interval)
	d := p.paint(Frame{Geometry: g, FrameTime: at, Pose: pose})
	if p.blur != nil && d.Backdrop != nil {
		p.blur.follow(w.compositor, Backdrop{Rect: pose.Place(d.Backdrop.Rect), Radius: d.Backdrop.Radius})
	}
	moving := d.Moving || p.transition.Moving(at.At)
	p.state.setAnimating(moving)
	p.frame = p.surface.Frame()
	frame := p.frame
	frame.OnDone = func(uint32) {
		if !closing {
			emit("frame-done")
		}
		if p.frame == frame {
			p.frame = nil
			p.state.frameDone()
		}
	}
	w.feedback(p.surface, !closing)
	if err := p.presenter.swap(); err != nil {
		return err
	}
	// Expected on screen no sooner than now: a first frame may have taken
	// long (glyphs rasterised), and the open starts with it.
	shown := max(w.clock.frame(w.now()).At, at.At)
	w.clock.drew(shown)
	p.transition.Presented(shown, at.Interval)
	if closing {
		emit("commit closing")
	} else {
		emit("commit")
		if p.wasMoving && !moving {
			emit("settled")
		}
	}
	p.drawnOnce, p.wasMoving = true, moving
	p.state.drew()
	return nil
}

// feedback asks when the frame about to be committed on s reaches the
// screen, when the compositor offers wp_presentation: its report times the
// frames after it. With the test trace on, a visible frame's report is
// `presented` or `discarded`.
func (w *Window) feedback(s *wlproto.WlSurface, visible bool) {
	if w.presentation == nil {
		return
	}
	fb := w.presentation.Feedback(s)
	fb.OnPresented = func(secHi, secLo, nsec, refresh, _, _, _ uint32) {
		if visible {
			emit("presented")
		}
		if w.monotonic {
			secs := uint64(secHi)<<32 | uint64(secLo)
			w.clock.presented(secs*1_000_000_000+uint64(nsec), uint64(refresh))
		}
	}
	fb.OnDiscarded = func() {
		if visible {
			emit("discarded")
		}
	}
}
