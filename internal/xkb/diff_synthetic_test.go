//go:build xkbref

package xkb_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
	"github.com/nerdwave-nick/logis/internal/xkb/xkbref"
)

// Variants of the owner's live keymap with what compositors rarely send
// but the compiler must still read as libxkbcommon does: modifier maps by
// keysym, keys wider than their type, group redirection and clamping,
// a redefined type, explicit vmods and repeat.
var variants = map[string]struct{ types, symbols string }{
	"modmap-by-keysym": {symbols: `
	modifier_map Mod3 { 0xffed, Super_R, a, 0x61, 0xff9c };
	key <AC06> { [ h, a ] };`},
	"wider-than-type": {symbols: `
	key <AE01> { type = "ONE_LEVEL", [ 0x31, 0x21, 0x40 ] };
	key <AE02> { type = "TWO_LEVEL", [ 2, at, 3, 4, 5 ] };`},
	"groups": {symbols: `
	key <AC02> { groupsRedirect = Group2, symbols[Group1] = [ s, S ], symbols[Group2] = [ 0x6c4, 0x6e4 ] };
	key <AC03> { groupsClamp, [ d, D ], [ 0x6d7, 0x6f7 ], [ 0x7e4, 0x7c4 ] };
	key <AC04> { [ f, F ], [ 0x6c1, 0x6e1 ] };`},
	"redefined-type": {types: `
	type "TWO_LEVEL" { modifiers = Shift+Lock; map[Shift] = Level2; map[Lock] = Level2; preserve[Lock] = Lock; };`},
	"explicit-vmods-repeat": {symbols: `
	key <AC05> { vmods = LevelThree, repeat = no, [ g, G ] };
	key <AC07> { repeat = yes, [ Shift_L ] };`},
}

func TestKeymapVariantsMatchLibxkbcommon(t *testing.T) {
	b, err := os.ReadFile("testdata/keymaps/live-niri.xkb")
	if err != nil {
		t.Fatal(err)
	}
	live := string(b)
	typesEnd := strings.Index(live, "\n};\n\nxkb_compatibility")
	symbolsEnd := strings.Index(live, "\tmodifier_map Shift")
	if typesEnd < 0 || symbolsEnd < 0 {
		t.Fatal("the live keymap's sections moved")
	}
	ctx := xkbref.NewContext()
	defer ctx.Unref()
	for name, v := range variants {
		text := live[:typesEnd] + v.types + live[typesEnd:symbolsEnd] + v.symbols + "\n" + live[symbolsEnd:]
		rk, err := ctx.KeymapFromString(text)
		if err != nil {
			t.Fatalf("%s: libxkbcommon: %v", name, err)
		}
		gk, err := xkb.ParseKeymap(text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bad := diffKeymap(t, name, rk, gk); bad > 0 {
			t.Errorf("%s: %d mismatches", name, bad)
		}
		rk.Unref()
	}
}
