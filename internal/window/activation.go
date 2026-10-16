package window

import "github.com/nerdwave-nick/logis/internal/wlproto"

// xdg-activation (spec §4.2; Rust src/window/activation.rs): tokens for
// the applications the launcher starts, so that the compositor focuses
// them. A token is asked for the visible panel's surface with the seat and
// the serial of the last key press or button press (the one that chose
// the app): niri, like most compositors, issues a valid token only for a
// focused surface and a recent serial. Without the global, every request
// is answered at once with no token.

// activation is the activation global and the last press's serial.
type activation struct {
	manager *wlproto.XdgActivationV1 // nil if not offered
	// serial is the last key or button press's, if pressed.
	serial  uint32
	pressed bool
}

// press notes the serial of a key or button press.
func (a *activation) press(serial uint32) { a.serial, a.pressed = serial, true }

// RequestToken asks the compositor for a token to activate appID; done
// gets it on the window's goroutine, "" at once when the compositor offers
// no xdg-activation.
func (w *Window) RequestToken(appID string, done func(token string)) {
	a := &w.activation
	if a.manager == nil {
		done("")
		return
	}
	t := a.manager.GetActivationToken()
	t.OnDone = func(token string) {
		t.Destroy()
		done(token)
	}
	if seat := w.seat.seat; seat != nil && a.pressed {
		t.SetSerial(a.serial, seat)
	}
	t.SetAppId(appID)
	if w.panel != nil {
		t.SetSurface(w.panel.surface)
	}
	t.Commit()
	_ = w.flush() // a lost compositor shows at the next Dispatch
}

// release lets go of the activation global.
func (a *activation) release() {
	if a.manager != nil {
		a.manager.Destroy()
		a.manager = nil
	}
}
