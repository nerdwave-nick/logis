package window

import (
	"encoding/binary"

	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Keyboard receives the seat's wl_keyboard events (the input model, a
// later ticket, implements it). Enter and leave are only those of the
// visible panel. Keymap owns fd and must close it.
type Keyboard interface {
	Keymap(format uint32, fd int, size uint32)
	Enter(serial uint32, keys []uint32)
	Leave(serial uint32)
	Key(serial, time, key, state uint32)
	Modifiers(serial, depressed, latched, locked, group uint32)
	RepeatInfo(rate, delay int32)
}

// seat is the first seat the compositor offers and its keyboard while it
// has one (Rust src/window/seat.rs).
type seat struct {
	name     uint32
	seat     *wlproto.WlSeat
	keyboard *wlproto.WlKeyboard
	consumer Keyboard
	shows    func(*wlproto.WlSurface) bool
}

// bindSeat binds the first seat, if any, up to version 9 (the pointer's
// axis_value120); its keyboard follows once the seat announces it.
func bindSeat(c *connection, shows func(*wlproto.WlSurface) bool) *seat {
	s := &seat{shows: shows}
	for _, g := range c.globals {
		if g.Interface == wlproto.WlSeatInterface.Name {
			s.name = g.Name
			s.seat, _ = c.registry.Bind(g.Name, wlproto.WlSeatInterface, min(g.Version, 9)).(*wlproto.WlSeat)
			break
		}
	}
	if s.seat != nil {
		s.seat.OnCapabilities = s.capabilities
	}
	return s
}

func (s *seat) capabilities(caps uint32) {
	has := caps&wlproto.WlSeatCapabilityKeyboard != 0
	switch {
	case has && s.keyboard == nil:
		s.keyboard = s.seat.GetKeyboard()
		s.listen(s.keyboard)
	case !has && s.keyboard != nil:
		s.releaseKeyboard()
	}
}

func (s *seat) listen(k *wlproto.WlKeyboard) {
	k.OnKeymap = func(format uint32, fd int, size uint32) {
		if s.consumer == nil {
			closeFd(fd)
			return
		}
		s.consumer.Keymap(format, fd, size)
	}
	k.OnEnter = func(serial uint32, surface *wlproto.WlSurface, keys []byte) {
		if s.shows(surface) {
			trace.Emit("keyboard-enter")
		}
		if s.consumer != nil && s.shows(surface) {
			pressed := make([]uint32, 0, len(keys)/4)
			for i := 0; i+4 <= len(keys); i += 4 {
				pressed = append(pressed, binary.NativeEndian.Uint32(keys[i:]))
			}
			s.consumer.Enter(serial, pressed)
		}
	}
	k.OnLeave = func(serial uint32, surface *wlproto.WlSurface) {
		if s.shows(surface) {
			trace.Emit("keyboard-leave")
		}
		if s.consumer != nil && s.shows(surface) {
			s.consumer.Leave(serial)
		}
	}
	k.OnKey = func(serial, time, key, state uint32) {
		trace.Emit("key %d %s", key, keyState(state))
		if s.consumer != nil {
			s.consumer.Key(serial, time, key, state)
		}
	}
	k.OnModifiers = func(serial, depressed, latched, locked, group uint32) {
		trace.Emit("modifiers %d %d %d %d", depressed, latched, locked, group)
		if s.consumer != nil {
			s.consumer.Modifiers(serial, depressed, latched, locked, group)
		}
	}
	k.OnRepeatInfo = func(rate, delay int32) {
		trace.Emit("repeat-info %d %d", rate, delay)
		if s.consumer != nil {
			s.consumer.RepeatInfo(rate, delay)
		}
	}
}

// keyState names a wl_keyboard key state in the test trace.
func keyState(state uint32) string {
	if state == wlproto.WlKeyboardKeyStatePressed {
		return "pressed"
	}
	return "released"
}

func (s *seat) releaseKeyboard() {
	if s.keyboard.Version() >= 3 {
		s.keyboard.Release()
	} else {
		s.keyboard.Destroy()
	}
	s.keyboard = nil
}

// removed: the compositor withdrew global g; if it is the seat, its
// keyboard and the seat go.
func (s *seat) removed(g Global) {
	if s.seat != nil && g.Name == s.name {
		s.release()
	}
}

// release lets go of the keyboard and the seat.
func (s *seat) release() {
	if s.keyboard != nil {
		s.releaseKeyboard()
	}
	if s.seat == nil {
		return
	}
	if s.seat.Version() >= 5 {
		s.seat.Release()
	} else {
		s.seat.Destroy()
	}
	s.seat = nil
}
