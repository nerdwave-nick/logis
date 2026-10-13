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
	changed := false
	switch step.Kind {
	case provider.StepStage:
		o := step.Options
		// The window keeps its size: the list scrolls within it.
		o.Lines, o.Width = sh.asked[0], sh.asked[1]
		_, rows := scene.Fit(o, u.theme)
		o.Lines = &rows
		sh.view, sh.rows, sh.count, sh.waiting = list.NewView(o), rows, 0, false
		sh.glide.Reset() // a new list: the band takes its row at once
		u.emit("stage %d", id)
		u.push(sh, step.Entries)
		changed = true
	case provider.StepAppend:
		u.push(sh, step.Entries)
	case provider.StepUpdate:
		for _, c := range step.Updates {
			u.emit("update %d", c.Index)
			changed = sh.view.Update(c.Index, c.Entry) || changed
		}
	case provider.StepRemove:
		for _, i := range step.Removed {
			u.emit("remove %d", i)
			changed = sh.view.Remove(i) || changed
		}
	case provider.StepPreselect:
		changed = sh.view.Preselect(step.Index)
	case provider.StepToast:
		notice := step.Notice
		u.emit("toast %s", trace.Quote(notice.Text))
		sh.toast, changed = &notice, true
	case provider.StepReady:
		sh.waiting = false
		u.emit("ready")
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
