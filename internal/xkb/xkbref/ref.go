//go:build xkbref

// Package xkbref wraps libxkbcommon through cgo: the test oracle the
// pure-Go xkb package is compared with (spec §4, test-only oracles). It
// builds only with the build tag xkbref, and only tests import it, so
// logis never links libxkbcommon:
//
//	go test -tags xkbref ./internal/xkb/...
package xkbref

/*
#cgo pkg-config: xkbcommon
#include <stdlib.h>
#include <xkbcommon/xkbcommon.h>
#include <xkbcommon/xkbcommon-compose.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Context is an xkb_context.
type Context struct{ c *C.struct_xkb_context }

// NewContext creates a context that ignores the environment's
// XKB_DEFAULT_* names. libxkbcommon objects are not thread-safe (their
// reference counts are plain integers), so nothing here uses Go
// finalizers: callers Unref explicitly, from one goroutine.
func NewContext() *Context {
	return &Context{C.xkb_context_new(C.XKB_CONTEXT_NO_ENVIRONMENT_NAMES)}
}

// Unref releases the context.
func (ctx *Context) Unref() { C.xkb_context_unref(ctx.c) }

// Unref releases the keymap.
func (k *Keymap) Unref() { C.xkb_keymap_unref(k.k) }

// Unref releases the state.
func (s *State) Unref() { C.xkb_state_unref(s.s) }

// Keymap is an xkb_keymap.
type Keymap struct {
	k   *C.struct_xkb_keymap
	ctx *Context
}

func wrapKeymap(ctx *Context, k *C.struct_xkb_keymap) (*Keymap, error) {
	if k == nil {
		return nil, errors.New("libxkbcommon could not compile the keymap")
	}
	km := &Keymap{k, ctx}
	return km, nil
}

// KeymapFromString compiles a text v1 keymap.
func (ctx *Context) KeymapFromString(s string) (*Keymap, error) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return wrapKeymap(ctx, C.xkb_keymap_new_from_string(ctx.c, cs, C.XKB_KEYMAP_FORMAT_TEXT_V1, 0))
}

// KeymapFromNames compiles a keymap from RMLVO names.
func (ctx *Context) KeymapFromNames(rules, model, layout, variant, options string) (*Keymap, error) {
	cstr := func(s string) *C.char {
		if s == "" {
			return nil
		}
		return C.CString(s)
	}
	names := C.struct_xkb_rule_names{rules: cstr(rules), model: cstr(model), layout: cstr(layout), variant: cstr(variant), options: cstr(options)}
	defer func() {
		for _, p := range []*C.char{names.rules, names.model, names.layout, names.variant, names.options} {
			C.free(unsafe.Pointer(p))
		}
	}()
	return wrapKeymap(ctx, C.xkb_keymap_new_from_names(ctx.c, &names, 0))
}

// String serialises the keymap as a compositor sends it.
func (k *Keymap) String() string {
	cs := C.xkb_keymap_get_as_string(k.k, C.XKB_KEYMAP_FORMAT_TEXT_V1)
	defer C.free(unsafe.Pointer(cs))
	return C.GoString(cs)
}

// KeyRange is the min and max keycode.
func (k *Keymap) KeyRange() (uint32, uint32) {
	return uint32(C.xkb_keymap_min_keycode(k.k)), uint32(C.xkb_keymap_max_keycode(k.k))
}

// NumMods is the number of modifiers.
func (k *Keymap) NumMods() int { return int(C.xkb_keymap_num_mods(k.k)) }

// ModName is the name of modifier i.
func (k *Keymap) ModName(i int) string {
	return C.GoString(C.xkb_keymap_mod_get_name(k.k, C.xkb_mod_index_t(i)))
}

// NumLayouts is the number of layouts.
func (k *Keymap) NumLayouts() int { return int(C.xkb_keymap_num_layouts(k.k)) }

// Repeats says whether kc repeats.
func (k *Keymap) Repeats(kc uint32) bool {
	return C.xkb_keymap_key_repeats(k.k, C.xkb_keycode_t(kc)) != 0
}

// State is an xkb_state.
type State struct {
	s  *C.struct_xkb_state
	km *Keymap
}

// NewState creates a state for k.
func (k *Keymap) NewState() *State {
	s := &State{C.xkb_state_new(k.k), k}
	return s
}

// UpdateKey presses (down) or releases kc, as a compositor does.
func (s *State) UpdateKey(kc uint32, down bool) {
	dir := C.enum_xkb_key_direction(C.XKB_KEY_UP)
	if down {
		dir = C.XKB_KEY_DOWN
	}
	C.xkb_state_update_key(s.s, C.xkb_keycode_t(kc), dir)
}

// UpdateMask is xkb_state_update_mask.
func (s *State) UpdateMask(base, latched, locked, baseGroup, latchedGroup, lockedGroup uint32) {
	C.xkb_state_update_mask(s.s, C.xkb_mod_mask_t(base), C.xkb_mod_mask_t(latched), C.xkb_mod_mask_t(locked),
		C.xkb_layout_index_t(baseGroup), C.xkb_layout_index_t(latchedGroup), C.xkb_layout_index_t(lockedGroup))
}

// Serialize returns what wl_keyboard.modifiers carries: depressed,
// latched, locked mods and the effective layout.
func (s *State) Serialize() (dep, lat, lock, group uint32) {
	return uint32(C.xkb_state_serialize_mods(s.s, C.XKB_STATE_MODS_DEPRESSED)),
		uint32(C.xkb_state_serialize_mods(s.s, C.XKB_STATE_MODS_LATCHED)),
		uint32(C.xkb_state_serialize_mods(s.s, C.XKB_STATE_MODS_LOCKED)),
		uint32(C.xkb_state_serialize_layout(s.s, C.XKB_STATE_LAYOUT_EFFECTIVE))
}

// Mods is the effective modifier mask.
func (s *State) Mods() uint32 {
	return uint32(C.xkb_state_serialize_mods(s.s, C.XKB_STATE_MODS_EFFECTIVE))
}

// OneSym is xkb_state_key_get_one_sym.
func (s *State) OneSym(kc uint32) uint32 {
	return uint32(C.xkb_state_key_get_one_sym(s.s, C.xkb_keycode_t(kc)))
}

// Syms is xkb_state_key_get_syms.
func (s *State) Syms(kc uint32) []uint32 {
	var p *C.xkb_keysym_t
	n := int(C.xkb_state_key_get_syms(s.s, C.xkb_keycode_t(kc), &p))
	if n <= 0 {
		return nil
	}
	out := make([]uint32, n)
	for i, v := range unsafe.Slice(p, n) {
		out[i] = uint32(v)
	}
	return out
}

// UTF8 is xkb_state_key_get_utf8.
func (s *State) UTF8(kc uint32) string {
	var buf [64]C.char
	n := C.xkb_state_key_get_utf8(s.s, C.xkb_keycode_t(kc), &buf[0], C.size_t(len(buf)))
	if n <= 0 {
		return ""
	}
	return C.GoStringN(&buf[0], C.int(min(int(n), len(buf)-1)))
}

// Layout is xkb_state_key_get_layout (-1 for invalid).
func (s *State) Layout(kc uint32) int {
	l := C.xkb_state_key_get_layout(s.s, C.xkb_keycode_t(kc))
	if l == C.XKB_LAYOUT_INVALID {
		return -1
	}
	return int(l)
}

// Level is xkb_state_key_get_level (-1 for invalid).
func (s *State) Level(kc uint32, layout int) int {
	l := C.xkb_state_key_get_level(s.s, C.xkb_keycode_t(kc), C.xkb_layout_index_t(layout))
	if l == C.XKB_LEVEL_INVALID {
		return -1
	}
	return int(l)
}

// Consumed is xkb_state_key_get_consumed_mods2 (gtk selects GTK mode).
func (s *State) Consumed(kc uint32, gtk bool) uint32 {
	mode := C.enum_xkb_consumed_mode(C.XKB_CONSUMED_MODE_XKB)
	if gtk {
		mode = C.XKB_CONSUMED_MODE_GTK
	}
	return uint32(C.xkb_state_key_get_consumed_mods2(s.s, C.xkb_keycode_t(kc), mode))
}

// ModIsActive is xkb_state_mod_index_is_active (effective).
func (s *State) ModIsActive(idx int) bool {
	return C.xkb_state_mod_index_is_active(s.s, C.xkb_mod_index_t(idx), C.XKB_STATE_MODS_EFFECTIVE) > 0
}

// ModIsConsumed is xkb_state_mod_index_is_consumed2.
func (s *State) ModIsConsumed(kc uint32, idx int, gtk bool) bool {
	mode := C.enum_xkb_consumed_mode(C.XKB_CONSUMED_MODE_XKB)
	if gtk {
		mode = C.XKB_CONSUMED_MODE_GTK
	}
	return C.xkb_state_mod_index_is_consumed2(s.s, C.xkb_keycode_t(kc), C.xkb_mod_index_t(idx), mode) > 0
}

// KeysymToUTF32 is xkb_keysym_to_utf32.
func KeysymToUTF32(ks uint32) uint32 { return uint32(C.xkb_keysym_to_utf32(C.xkb_keysym_t(ks))) }

// KeysymToUpper is xkb_keysym_to_upper.
func KeysymToUpper(ks uint32) uint32 { return uint32(C.xkb_keysym_to_upper(C.xkb_keysym_t(ks))) }

// KeysymToLower is xkb_keysym_to_lower.
func KeysymToLower(ks uint32) uint32 { return uint32(C.xkb_keysym_to_lower(C.xkb_keysym_t(ks))) }

// UTF32ToKeysym is xkb_utf32_to_keysym.
func UTF32ToKeysym(u uint32) uint32 { return uint32(C.xkb_utf32_to_keysym(C.uint32_t(u))) }

// KeysymFromName is xkb_keysym_from_name without flags.
func KeysymFromName(name string) uint32 {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return uint32(C.xkb_keysym_from_name(cs, 0))
}

// KeysymName is xkb_keysym_get_name.
func KeysymName(ks uint32) string {
	var buf [128]C.char
	if C.xkb_keysym_get_name(C.xkb_keysym_t(ks), &buf[0], C.size_t(len(buf))) < 0 {
		return ""
	}
	return C.GoString(&buf[0])
}
