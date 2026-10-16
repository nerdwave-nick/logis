package input

import (
	"io"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/xkb"
)

// Keyboard is the keyboard as the visible session sees it: keymap,
// modifiers, Compose, repeat and the focus-loss policy (Rust
// src/input/keyboard.rs). Its model methods (Press, SetMask, …) return
// what a key does; wayland.go feeds it the wl_keyboard events and hands
// the actions to the session's sink. It is owned by the window's
// goroutine.
type Keyboard struct {
	layout    *Layout
	compose   *Compose
	repeat    *Repeat
	focusLoss config.FocusLoss
	sink      func(Action)
	pressed   func()
	log       io.Writer
	now       func() time.Time
}

// Repeated is one key repeat: the repeating key and what it does under the
// current modifiers.
type Repeated struct {
	Code   uint32
	Action Action
	OK     bool // whether it does anything
}

// NewKeyboard is a keyboard without a keymap yet, composing with compose
// (nil: no Compose).
func NewKeyboard(compose *Compose) *Keyboard {
	return &Keyboard{compose: compose, repeat: NewRepeat()}
}

// SetFocusLoss sets what losing keyboard focus does.
func (k *Keyboard) SetFocusLoss(p config.FocusLoss) { k.focusLoss = p }

// SetKeymap installs the compositor's (new) keymap: modifiers start over,
// and a sequence or a repeat in progress ends.
func (k *Keyboard) SetKeymap(km *xkb.Keymap) {
	k.layout = NewLayout(km)
	k.Reset()
}

// SetMask applies the compositor's modifier masks and layout group.
func (k *Keyboard) SetMask(depressed, latched, locked, group uint32) {
	if k.layout != nil {
		k.layout.SetMask(depressed, latched, locked, group)
	}
}

// SetRepeatRate applies the compositor's repeat info (rate 0 disables).
func (k *Keyboard) SetRepeatRate(rate, delay int32) { k.repeat.SetRate(rate, delay) }

// Reset ends a Compose sequence or a repeat in progress.
func (k *Keyboard) Reset() {
	k.repeat.Reset()
	if k.compose != nil {
		k.compose.Reset()
	}
}

// FocusIn: the panel gained keyboard focus; nothing from before carries
// over.
func (k *Keyboard) FocusIn() { k.Reset() }

// FocusOut: the panel lost keyboard focus. Repeat and Compose stop, and
// the session is dismissed unless the policy keeps it.
func (k *Keyboard) FocusOut() (Action, bool) {
	k.Reset()
	return ActDismiss, k.focusLoss == config.FocusDismiss
}

// Press: evdev key code was pressed at now.
func (k *Keyboard) Press(code uint32, now time.Time) (Action, bool) {
	if k.layout == nil {
		return Action{}, false
	}
	key := k.layout.Key(code)
	fed, text := Pass, ""
	if k.compose != nil {
		fed, text = k.compose.Feed(key.Keysym)
	}
	switch {
	case key.Keysym.IsModifier():
		// A modifier neither starts nor stops a repeat.
	case fed == Pass && k.layout.Repeats(code):
		k.repeat.Press(code, now)
	default:
		k.repeat.Reset()
	}
	switch fed {
	case Swallowed:
		return Action{}, false
	case Composed:
		return Insert(text), text != ""
	}
	return resolve(key)
}

// Release: evdev key code was released.
func (k *Keyboard) Release(code uint32) { k.repeat.Release(code) }

// Deadline is when the next repeat is due; false while no key repeats, so
// the window's timer stays disarmed.
func (k *Keyboard) Deadline() (time.Time, bool) { return k.repeat.Deadline() }

// RepeatsDue are the repeats due at now, each with what the key does under
// the current modifiers (Compose is not fed by repeats).
func (k *Keyboard) RepeatsDue(now time.Time) []Repeated {
	code, n := k.repeat.Due(now)
	if n == 0 || k.layout == nil {
		return nil
	}
	r := Repeated{Code: code}
	r.Action, r.OK = resolve(k.layout.Key(code))
	out := make([]Repeated, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// resolve is a key's binding with consumed-modifier semantics, else its
// text. While another layout than the first is active, a key that is no
// binding there is tried as the first layout's key, so Ctrl+G works on a
// Cyrillic layout.
func resolve(key Key) (Action, bool) {
	if a, ok := Binding(key.Keysym, key.Mods); ok {
		return a, true
	}
	if key.OtherLayout {
		if a, ok := Binding(key.Base, key.Mods); ok {
			return a, true
		}
	}
	return Typed(key.Text, key.Mods)
}
