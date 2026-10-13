package main

import (
	"sync/atomic"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/trace"
)

// evdev key codes the stand-in keyboard knows: the same keys on every
// layout.
const (
	keyUp   = 103
	keyDown = 108
)

// debugKeys is the debug panel's keyboard until the input model exists:
// Escape dismisses the panel, Up and Down move its selection (Rust's
// DebugSession for Select and Dismiss); everything else is ignored, keymaps
// are closed unread. redraw asks the window for the next frame.
type debugKeys struct {
	pressed   bool
	signalled atomic.Bool
	row       int
	redraw    func()
}

func (k *debugKeys) done() bool { return k.pressed || k.signalled.Load() }

func (k *debugKeys) Keymap(_ uint32, fd int, _ uint32) { _ = syscall.Close(fd) }
func (k *debugKeys) Enter(uint32, []uint32)            {}
func (k *debugKeys) Leave(uint32)                      {}
func (k *debugKeys) Modifiers(_, _, _, _, _ uint32)    {}
func (k *debugKeys) RepeatInfo(_, _ int32)             {}

// Key acts on presses; with the test trace on it reports what each did
// (`select <row>`, `dismiss`), as Rust's debug panel does.
func (k *debugKeys) Key(_, _, key, state uint32) {
	if state != 1 {
		return
	}
	switch {
	case key == keyEscape:
		trace.Emit("dismiss")
		k.pressed = true
	case key == keyDown && k.row+1 < debugSelectable:
		k.selectRow(k.row + 1)
	case key == keyUp && k.row > 0:
		k.selectRow(k.row - 1)
	}
}

func (k *debugKeys) selectRow(row int) {
	k.row = row
	trace.Emit("select %d", row)
	k.redraw()
}
