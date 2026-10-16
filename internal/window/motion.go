package window

import (
	"time"

	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Motion and blur on the window (Rust src/window/windows/motion.rs): the
// parameters in force, the panel's open transition and its animated
// close, and the optional globals that pace and blur the panels. With the
// test trace on: `faded` once a hidden panel is gone, `cut` when a panel
// still closing is cut short by the next one, `blur <rectangles>` and
// `blur-capable true|false`.

// closeGrace is how long a closing panel may outlast its transition when
// no frame callback comes (its output went dark): then it goes without
// one.
const closeGrace = 100 * time.Millisecond

// clockMonotonic is wp_presentation's clk_id of CLOCK_MONOTONIC on Linux.
const clockMonotonic = 1

// ShowFrames shows a panel of size logical pixels drawn by paint,
// replacing any visible one at once. It is drawn once the compositor
// configures it, opening with the motion in force.
//
// A panel still closing stops where it is and goes only after the new
// panel's first frame (`cut`): destroying it makes the compositor draw a
// frame and EGL release its buffers, which would delay that first frame
// (the warm-open budget).
func (w *Window) ShowFrames(size scale.Size, paint FramePainter) error {
	if w.panel != nil {
		w.panel.destroy()
		w.panel = nil
	}
	if w.closing != nil {
		w.cutRetired()
		w.retired, w.closing = w.closing, nil
	}
	w.resetInput()
	transition := motion.Resting()
	if w.motion.Open.Duration > 0 {
		transition = motion.Opening(w.motion.OpenFrom, w.motion.Open)
	}
	w.panel = w.newPanel(size, paint, transition)
	return w.flush()
}

// Hide closes the visible panel, if any: at once without motion (or if it
// was never shown), else it hands keyboard and pointer back at once and
// fades out over the close transition (no longer Visible meanwhile). A
// later Show cuts that short. The caller answers its session before
// calling this; nothing here waits.
func (w *Window) Hide() {
	p := w.panel
	if p == nil {
		return
	}
	w.panel = nil
	w.resetInput()
	w.cutRetired()
	if w.motion.Close.Duration <= 0 || !p.drawnOnce {
		p.destroy()
		emit("faded")
		return
	}
	if w.closing != nil {
		w.dropClosing()
	}
	p.close(w.motion, w.now(), w.compositor)
	w.closing = p
	_ = w.flush() // a lost compositor shows at the next Dispatch
}

// SetMotion sets the motion of the panels shown from now on (and of their
// close). A window starts with motion.Off.
func (w *Window) SetMotion(m motion.Motion) { w.motion = m }

// SetBlur blurs behind the panels from now on (when the compositor offers
// ext-background-effect), the visible one included, or stops; the change
// reaches the compositor with the next frame.
func (w *Window) SetBlur(on bool) {
	w.blur = on
	p := w.panel
	if p == nil {
		return
	}
	switch want := on && w.effects != nil; {
	case want && p.blur == nil:
		p.blur = newPanelBlur(w.effects, p.surface)
	case !want && p.blur != nil:
		p.blur.destroy()
		p.blur = nil
	default:
		return
	}
	p.state.requestRedraw()
}

// cutRetired lets the panel a Show cut short go.
func (w *Window) cutRetired() {
	if w.retired != nil {
		w.retired.destroy()
		w.retired = nil
		emit("cut")
	}
}

// dropClosing lets the closing panel go: it has faded.
func (w *Window) dropClosing() {
	w.closing.destroy()
	w.closing = nil
	emit("faded")
}

// dropClosed lets every panel the compositor closed go at once.
func (w *Window) dropClosed() {
	if p := w.panel; p != nil && p.closed {
		p.destroy()
		w.panel = nil
		w.resetInput()
		emit("faded")
	}
	if c := w.closing; c != nil && c.closed {
		w.dropClosing()
	}
	if r := w.retired; r != nil && r.closed {
		r.destroy()
		w.retired = nil
	}
}

// dropAll lets every panel go at once.
func (w *Window) dropAll() {
	if w.panel != nil {
		w.panel.closed = true
	}
	w.dropClosed()
	if w.closing != nil {
		w.dropClosing()
	}
	w.cutRetired()
}

// motionDeadline is how long until the closing panel must go even
// without a frame callback, if one is closing.
func (w *Window) motionDeadline() (time.Duration, bool) {
	if w.closing == nil {
		return 0, false
	}
	end, ok := w.closing.transition.FinishTime()
	if !ok {
		return 0, false
	}
	due, now := end+uint64(closeGrace), w.now()
	if now >= due {
		return 0, true
	}
	return time.Duration(due - now), true
}

// expireClosing lets a closing panel that outlasted its transition and
// grace go (its frame callbacks stopped).
func (w *Window) expireClosing() {
	if left, ok := w.motionDeadline(); ok && left == 0 && w.closing.transition.Finished(w.now()) {
		w.dropClosing()
	}
}

// bindOptional binds the globals used when offered: ext-background-effect
// (the test trace reports whether the compositor can blur now; nothing
// else depends on it, a region set without the capability is simply not
// applied) and wp_presentation.
func (w *Window) bindOptional() {
	w.effects, _ = w.Bind(wlproto.ExtBackgroundEffectManagerV1Interface, 1).(*wlproto.ExtBackgroundEffectManagerV1)
	if w.effects != nil {
		w.effects.OnCapabilities = func(flags uint32) {
			emit("blur-capable %t", flags&wlproto.ExtBackgroundEffectManagerV1CapabilityBlur != 0)
		}
	}
	w.presentation, _ = w.Bind(wlproto.WpPresentationInterface, 2).(*wlproto.WpPresentation)
	if w.presentation != nil {
		w.presentation.OnClockId = func(id uint32) { w.monotonic = id == clockMonotonic }
	}
}

// releaseOptional releases them.
func (w *Window) releaseOptional() {
	if w.effects != nil {
		w.effects.Destroy()
		w.effects = nil
	}
	if w.presentation != nil {
		w.presentation.Destroy()
		w.presentation = nil
	}
}
