//go:build xkbref

package xkbref

/*
#include <stdlib.h>
#include <xkbcommon/xkbcommon.h>
#include <xkbcommon/xkbcommon-compose.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// ComposeTable is an xkb_compose_table.
type ComposeTable struct{ t *C.struct_xkb_compose_table }

func wrapTable(t *C.struct_xkb_compose_table) (*ComposeTable, error) {
	if t == nil {
		return nil, errors.New("libxkbcommon could not load the Compose table")
	}
	ct := &ComposeTable{t}
	return ct, nil
}

// ComposeFromLocale loads the table for locale as libxkbcommon finds it
// (XCOMPOSEFILE, XDG, ~/.XCompose, the locale's system table).
func (ctx *Context) ComposeFromLocale(locale string) (*ComposeTable, error) {
	cs := C.CString(locale)
	defer C.free(unsafe.Pointer(cs))
	return wrapTable(C.xkb_compose_table_new_from_locale(ctx.c, cs, 0))
}

// ComposeFromBuffer parses a Compose table from text.
func (ctx *Context) ComposeFromBuffer(text, locale string) (*ComposeTable, error) {
	cs, cl := C.CString(text), C.CString(locale)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cl))
	return wrapTable(C.xkb_compose_table_new_from_buffer(ctx.c, cs, C.size_t(len(text)), cl, C.XKB_COMPOSE_FORMAT_TEXT_V1, 0))
}

// Unref releases the table.
func (t *ComposeTable) Unref() { C.xkb_compose_table_unref(t.t) }

// Unref releases the state.
func (s *ComposeState) Unref() { C.xkb_compose_state_unref(s.s) }

// Entry is one Compose sequence.
type Entry struct {
	Seq    []uint32
	Keysym uint32
	UTF8   string
}

// Entries lists every sequence of the table.
func (t *ComposeTable) Entries() []Entry {
	it := C.xkb_compose_table_iterator_new(t.t)
	defer C.xkb_compose_table_iterator_free(it)
	var out []Entry
	for e := C.xkb_compose_table_iterator_next(it); e != nil; e = C.xkb_compose_table_iterator_next(it) {
		var n C.size_t
		seq := C.xkb_compose_table_entry_sequence(e, &n)
		ent := Entry{
			Keysym: uint32(C.xkb_compose_table_entry_keysym(e)),
			UTF8:   C.GoString(C.xkb_compose_table_entry_utf8(e)),
		}
		for _, ks := range unsafe.Slice(seq, int(n)) {
			ent.Seq = append(ent.Seq, uint32(ks))
		}
		out = append(out, ent)
	}
	return out
}

// ComposeState is an xkb_compose_state.
type ComposeState struct{ s *C.struct_xkb_compose_state }

// NewState creates a compose state.
func (t *ComposeTable) NewState() *ComposeState {
	s := &ComposeState{C.xkb_compose_state_new(t.t, 0)}
	return s
}

// Feed feeds a keysym; it reports whether it was accepted (not ignored).
func (s *ComposeState) Feed(ks uint32) bool {
	return C.xkb_compose_state_feed(s.s, C.xkb_keysym_t(ks)) == C.XKB_COMPOSE_FEED_ACCEPTED
}

// Status is 0 nothing, 1 composing, 2 composed, 3 cancelled.
func (s *ComposeState) Status() int { return int(C.xkb_compose_state_get_status(s.s)) }

// UTF8 is the composed text.
func (s *ComposeState) UTF8() string {
	var buf [256]C.char
	C.xkb_compose_state_get_utf8(s.s, &buf[0], C.size_t(len(buf)))
	return C.GoString(&buf[0])
}

// Keysym is the composed keysym.
func (s *ComposeState) Keysym() uint32 { return uint32(C.xkb_compose_state_get_one_sym(s.s)) }

// Reset forgets the sequence.
func (s *ComposeState) Reset() { C.xkb_compose_state_reset(s.s) }
