// Package xkb is logis's client-side XKB, in pure Go (spec feat/logis-go
// D3): it parses the keymap a compositor sends (xkb_keymap text v1, keysyms
// in hex or by name), mirrors the compositor's state from
// wl_keyboard.modifiers, and answers what a key means now: keysyms with
// the Caps Lock transformation, text with the Control transformation,
// consumed modifiers (XKB and GTK mode) and repeat. It also loads and runs
// Compose tables, found as libxkbcommon finds them.
//
// Semantics follow libxkbcommon 1.13; the differential tests compare the
// two (go test -tags xkbref ./internal/xkb/..., with the cgo oracle in
// xkbref, never linked into logis). A client needs neither RMLVO nor
// include resolution nor key actions: the compositor sends the keymap
// resolved and applies the actions itself.
//
// Everything parsed is untrusted: keymaps are bounded in size, nesting,
// counts and compile work (limits.go) and fail with an *Error; Compose
// tables are bounded in bytes and include depth.
package xkb
