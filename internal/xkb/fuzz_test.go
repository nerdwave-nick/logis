package xkb

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseKeymap: no input panics, hangs or exhausts memory, and a
// keymap that parses answers every question about every key.
// Run: go test -run - -fuzz FuzzParseKeymap -fuzztime 60s -parallel 4 ./internal/xkb
func FuzzParseKeymap(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "keymaps", "*.xkb"))
	more, _ := filepath.Glob(filepath.Join("testdata", "keymaps", "xkbcli", "*.xkb"))
	for _, name := range append(files, more...) {
		b, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b))
	}
	for _, s := range []string{
		wrap("xkb_types { virtual_modifiers NumLock, Hyper=0x4000; type \"T\" { modifiers = Shift+Lock; map[Shift] = Level2; preserve[Lock] = Lock; }; };"),
		wrap("xkb_compat { interpret.repeat = false; interpret Num_Lock+AnyOf(all) { virtualModifier = NumLock; useModMapMods = level1; }; };"),
		wrap("xkb_symbols { name[Group1] = \"x\"; key <a> { type[Group2] = \"T\", symbols[Group2] = [ { a, b }, NoSymbol ], repeat = no, groupsRedirect = Group1 }; modifier_map Control { <a>, Shift_L, 0x61 }; };"),
		"xkb_keymap { xkb_keycodes { alias <x> = <a>; minimum = 8; maximum = 255; <a> = 38; }; xkb_symbols { key <x> { [ U00E9, 0x1001e9e, 5 ] }; }; };",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		km, err := ParseKeymap(src)
		if err != nil {
			return
		}
		for _, m := range []Mask{0, 0x5, 0xffffffff} {
			for _, group := range []uint32{1, 0xffffffff} {
				s := NewState(km)
				s.UpdateMask(m, m>>3, m>>1, 0, 0, group)
				for kc := range km.keys {
					s.Syms(kc)
					s.OneSym(kc)
					s.UTF8(kc)
					s.Consumed(kc, ConsumedXKB)
					s.Consumed(kc, ConsumedGTK)
					s.ModIsConsumed(kc, ModShift, ConsumedXKB)
					s.ModIsActive(km.NumMods() - 1)
					km.Repeats(kc)
				}
			}
		}
	})
}
