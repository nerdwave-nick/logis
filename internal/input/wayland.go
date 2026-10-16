package input

import (
	"fmt"
	"io"
	"strings"
	"syscall"
	"time"

	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/xkb"
)

// wl_keyboard enums the adapter reads.
const (
	// KeymapXkbV1 is wl_keyboard.keymap_format.xkb_v1.
	KeymapXkbV1 = 1
	// KeyReleased and KeyPressed are wl_keyboard.key_state.
	KeyReleased = 0
	KeyPressed  = 1
)

// The wl_keyboard side of Keyboard: the window's seat calls these on its
// goroutine (window.Keyboard), and the window's repeat timer calls
// Deadline and Repeat. Every action goes to the sink as it happens.

// SetSink sends every action the keyboard produces to sink.
func (k *Keyboard) SetSink(sink func(Action)) { k.sink = sink }

// SetPressSink calls pressed on each key press the panel reads: any key
// but a bare modifier, once per press, never for its repeats.
func (k *Keyboard) SetPressSink(pressed func()) { k.pressed = pressed }

// SetLog reports refused keymaps to log.
func (k *Keyboard) SetLog(log io.Writer) { k.log = log }

// SetClock replaces time.Now (tests).
func (k *Keyboard) SetClock(now func() time.Time) { k.now = now }

func (k *Keyboard) emit(a Action, ok bool) {
	if ok && k.sink != nil {
		k.sink(a)
	}
}

// Keymap is wl_keyboard.keymap; it owns fd and closes it. A keymap that
// cannot be read or compiled leaves the last good one in force; a new one
// replaces the old at once (niri sends a second keymap mid-session once a
// virtual keyboard types), ending a Compose sequence and a repeat.
func (k *Keyboard) Keymap(format uint32, fd int, size uint32) {
	defer func() { _ = syscall.Close(fd) }()
	if format != KeymapXkbV1 || size == 0 {
		return
	}
	text, err := readKeymap(fd, size)
	var km *xkb.Keymap
	if err == nil {
		km, err = xkb.ParseKeymap(text)
	}
	if err != nil {
		if k.log != nil {
			_, _ = fmt.Fprintf(k.log, "logis: the compositor's keymap is heretical (%v); the last good one stays in force\n", err)
		}
		return
	}
	k.SetKeymap(km)
	trace.Emit("keymap")
}

// readKeymap reads the keymap text of size bytes (NUL included) from fd:
// mapped privately (wl_keyboard v7+ requires MAP_PRIVATE), or read where
// the fd cannot be mapped.
func readKeymap(fd int, size uint32) (string, error) {
	if size > xkb.MaxKeymapSize+1 {
		return "", fmt.Errorf("%d bytes, larger than the %d accepted", size, xkb.MaxKeymapSize)
	}
	var text string
	if data, err := syscall.Mmap(fd, 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE); err == nil {
		text = string(data)
		_ = syscall.Munmap(data)
	} else {
		buf := make([]byte, size)
		n := 0
		for n < len(buf) {
			m, err := syscall.Pread(fd, buf[n:], int64(n))
			if err != nil {
				return "", fmt.Errorf("reading it: %w", err)
			}
			if m == 0 {
				break
			}
			n += m
		}
		text = string(buf[:n])
	}
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	return text, nil
}

// Enter is wl_keyboard.enter on the panel: nothing from before carries
// over.
func (k *Keyboard) Enter(serial uint32, keys []uint32) { k.FocusIn() }

// Leave is wl_keyboard.leave from the panel: the focus-loss policy decides.
func (k *Keyboard) Leave(serial uint32) { k.emit(k.FocusOut()) }

// Key is wl_keyboard.key.
func (k *Keyboard) Key(serial, time, key, state uint32) {
	if state == KeyPressed {
		if k.pressed != nil && k.layout != nil && !k.layout.Modifier(key) {
			k.pressed()
		}
		k.emit(k.Press(key, k.clock()))
		return
	}
	k.Release(key)
}

// Modifiers is wl_keyboard.modifiers.
func (k *Keyboard) Modifiers(serial, depressed, latched, locked, group uint32) {
	k.SetMask(depressed, latched, locked, group)
}

// RepeatInfo is wl_keyboard.repeat_info.
func (k *Keyboard) RepeatInfo(rate, delay int32) { k.SetRepeatRate(rate, delay) }

// Repeat: the window's repeat timer fired at now; the repeats due go to
// the sink (trace: `key <code> repeated` each).
func (k *Keyboard) Repeat(now time.Time) {
	for _, r := range k.RepeatsDue(now) {
		trace.Emit("key %d repeated", r.Code)
		k.emit(r.Action, r.OK)
	}
}

func (k *Keyboard) clock() time.Time {
	if k.now != nil {
		return k.now()
	}
	return time.Now()
}
