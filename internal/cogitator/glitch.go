package cogitator

import "github.com/nerdwave-nick/logis/internal/effects"

// The glitch as the window wears it (feat/logis-go 33cfcf44): each shown
// panel keeps a glitch clock (internal/glitch), opened by its first frame,
// which fires the open burst; a switch of tab and a key press fire theirs
// (glitchBurst, keyPressed). Each frame stands at its moment, and the panel
// draws on while the glitch moves; in between, the window sleeps until the
// next ambient episode or random burst (deadline, wake). A stilled theme
// (reduce-motion) has the glitch off.

// glitchAt is sh's glitch at frame time at, and whether it moves (the
// first frame opens its clock).
func (u *ui) glitchAt(sh *shown, at uint64) (effects.Moment, bool) {
	g := u.theme.Glitch
	if !sh.glitch.Opened() {
		sh.glitch.Open(at, g)
	}
	return sh.glitch.At(at, g), sh.glitch.Moving(at, g)
}

// glitchBurst fires the shown panel's glitch burst on event e now, if the
// theme fires one on it, and draws it. A switch of tab calls it with
// effects.EventTab (the tab strip, 8cba2fc9).
func (u *ui) glitchBurst(e effects.Events) {
	sh := u.shown
	if sh == nil {
		return
	}
	var at uint64
	if u.clock != nil {
		at = u.clock()
	}
	sh.glitch.Fire(e, at, u.theme.Glitch)
	u.surf.RequestRedraw()
}

// glitchWake is when the shown panel's glitch next starts to move while
// it stands still (CLOCK_MONOTONIC ns), until the window woke for it. The
// start is kept once reached (the glitch moves then, but the window may
// not have drawn since), and dropped when nothing is to come.
func (u *ui) glitchWake() (uint64, bool) {
	sh := u.shown
	if sh == nil || u.clock == nil {
		return 0, false
	}
	now, g := u.clock(), u.theme.Glitch
	if next, ok := sh.glitch.Next(now, g); ok {
		sh.next = next
	} else if !sh.glitch.Moving(now, g) {
		sh.next = 0
	}
	if sh.next == 0 || sh.next == sh.woke {
		return 0, false
	}
	return sh.next, true
}

// wake asks for a frame once the glitch's next start has come; that
// frame finds it moving and keeps drawing.
func (u *ui) wake() {
	next, ok := u.glitchWake()
	if !ok || u.clock() < next {
		return
	}
	u.shown.woke = next
	u.surf.RequestRedraw()
}

// keyPressed is a key pressed in the panel (not a repeat): it fires the
// shown panel's small key burst (8ad70fee), unless the session lingers
// and takes no keys.
func (u *ui) keyPressed() {
	if sh := u.shown; sh != nil && !sh.lingering {
		u.glitchBurst(effects.EventKey)
	}
}
