package cogitator

import (
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// A provider's steps in the window (Rust src/daemon/window/ui.rs
// Shown::step). A next stage is not a close and an open: the window stays
// where it is, at the first stage's size, and only its list changes. The
// daemon times a provider's toast and says when it is over (untoast). A
// session that is over lingers on its toast without the keyboard, taking
// no steps or keys, until the daemon hides it.

// step applies what session id's provider asks next.
func (u *ui) step(id daemon.SessionID, step provider.Step) {
	sh := u.mine(id)
	if sh == nil || sh.lingering {
		return
	}
	p, emit := sh.paneOf(step.Tab), u.emit
	if sh.tabbed && step.Tab == provider.TabRites {
		emit = func(format string, args ...any) { u.emit("rites "+format, args...) }
	}
	changed := false
	switch step.Kind {
	case provider.StepStage:
		o := step.Options
		// The window keeps its size: the list scrolls within it.
		o.Lines, o.Width = sh.asked[0], sh.asked[1]
		_, rows := u.fitIn(o, sh.tabbed)
		o.Lines = &rows
		if sh.tabbed {
			o = firstRites(sh, p, o)
		}
		p.view, p.columns, p.count, p.waiting = list.NewView(o), scene.Columns{}, 0, false
		sh.rows = rows
		if p == sh.pane {
			sh.glide.Reset() // a new list: the band takes its row at once
		}
		emit("stage %d", id)
		u.push(p, step.Entries, emit)
		changed = true
	case provider.StepAppend:
		u.push(p, step.Entries, emit)
	case provider.StepUpdate:
		for _, c := range step.Updates {
			emit("update %d", c.Index)
			changed = p.view.Update(c.Index, c.Entry) || changed
		}
	case provider.StepRemove:
		for _, i := range step.Removed {
			emit("remove %d", i)
			changed = p.view.Remove(i) || changed
		}
	case provider.StepPreselect:
		changed = p.view.Preselect(step.Index)
	case provider.StepToast:
		notice := step.Notice
		emit("toast %s", trace.Quote(notice.Text))
		sh.toast, changed = &notice, true
	case provider.StepReady:
		p.waiting = false
		emit("ready")
	}
	if changed {
		u.surf.RequestRedraw()
	}
}

// linger: session id is over, but its toast still shows; it stays in view
// without the keyboard until hide.
func (u *ui) linger(id daemon.SessionID) {
	if sh := u.mine(id); sh != nil {
		sh.lingering = true
		u.surf.ReleaseKeyboard()
		u.emit("linger %d", id)
	}
}

// untoast: session id's toast has had its time.
func (u *ui) untoast(id daemon.SessionID) {
	if sh := u.mine(id); sh != nil && sh.toast != nil {
		sh.toast = nil
		u.surf.RequestRedraw()
		u.emit("untoast")
	}
}
