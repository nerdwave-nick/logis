package main

import (
	"sync/atomic"
	"syscall"
)

// escape is the debug panel's keyboard until the input model exists: a
// press of Escape dismisses the panel; everything else is ignored, keymaps
// are closed unread.
type escape struct {
	pressed   bool
	signalled atomic.Bool
}

func (e *escape) done() bool { return e.pressed || e.signalled.Load() }

func (e *escape) Keymap(_ uint32, fd int, _ uint32) { _ = syscall.Close(fd) }
func (e *escape) Enter(uint32, []uint32)            {}
func (e *escape) Leave(uint32)                      {}
func (e *escape) Modifiers(_, _, _, _, _ uint32)    {}
func (e *escape) RepeatInfo(_, _ int32)             {}

func (e *escape) Key(_, _, key, state uint32) {
	if key == keyEscape && state == 1 {
		e.pressed = true
	}
}
