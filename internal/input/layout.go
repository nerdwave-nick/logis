package input

import "github.com/nerdwave-nick/logis/internal/xkb"

// evdevOffset: wl_keyboard sends evdev codes, xkb keycodes are 8 higher.
const evdevOffset = 8

// Layout is the compositor's keymap and the state its modifier masks
// describe: what a key means right now (Rust src/input/layout.rs).
type Layout struct {
	km    *xkb.Keymap
	state *xkb.State
	// Shift, Control, Alt (Mod1) and Super (Mod4), as indices in the keymap.
	mods [4]int
}

// Key is what a key means in the current state.
type Key struct {
	// Keysym is what the layout produces now (levels and locks applied).
	Keysym xkb.Keysym
	// Base is the key's keysym on the first layout at its first level, for
	// shortcuts typed while another layout (say Cyrillic) is active.
	Base xkb.Keysym
	// OtherLayout: another layout than the first is active.
	OtherLayout bool
	// Text is what it types (may be a control character or empty).
	Text string
	// Mods are the active modifiers it did not consume.
	Mods Mods
}

// NewLayout is the layout of km, with no modifier active.
func NewLayout(km *xkb.Keymap) *Layout {
	l := &Layout{km: km, state: xkb.NewState(km)}
	for i, name := range []string{"Shift", "Control", "Mod1", "Mod4"} {
		l.mods[i] = km.ModIndex(name)
	}
	return l
}

// SetMask applies the compositor's modifier masks and layout group.
func (l *Layout) SetMask(depressed, latched, locked, group uint32) {
	l.state.UpdateMask(xkb.Mask(depressed), xkb.Mask(latched), xkb.Mask(locked), 0, 0, group)
}

// Key is what evdev key code means now.
func (l *Layout) Key(code uint32) Key {
	kc := code + evdevOffset
	held := func(i int) bool {
		return i >= 0 && l.state.ModIsActive(i) && !l.state.ModIsConsumed(kc, i, xkb.ConsumedXKB)
	}
	var base xkb.Keysym
	if syms := l.km.SymsByLevel(kc, 0, 0); len(syms) > 0 {
		base = syms[0]
	}
	return Key{
		Keysym:      l.state.OneSym(kc),
		Base:        base,
		OtherLayout: l.state.Layout(kc) > 0,
		Text:        l.state.UTF8(kc),
		Mods:        Mods{Shift: held(l.mods[0]), Ctrl: held(l.mods[1]), Alt: held(l.mods[2]), Logo: held(l.mods[3])},
	}
}

// Modifier says whether evdev key code is a modifier key (Shift, Control,
// …) under the current state.
func (l *Layout) Modifier(code uint32) bool { return l.state.OneSym(code + evdevOffset).IsModifier() }

// Repeats says whether the keymap lets evdev key code repeat (modifiers
// do not).
func (l *Layout) Repeats(code uint32) bool { return l.km.Repeats(code + evdevOffset) }
