//go:build xkbref

package xkb_test

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
	"github.com/nerdwave-nick/logis/internal/xkb/xkbref"
)

// keysymsUnderTest: every legacy keysym below 0x13ff, the special and
// function ranges, the Unicode range up to the SMP, and the vendor
// keysyms the header names.
func keysymsUnderTest() []uint32 {
	var out []uint32
	for ks := uint32(0); ks < 0x1400; ks++ {
		out = append(out, ks)
	}
	for ks := uint32(0xfe00); ks <= 0xffff; ks++ {
		out = append(out, ks)
	}
	for u := uint32(0); u <= 0x2ffff; u++ {
		out = append(out, 0x01000000+u)
	}
	for ks := uint32(0x1008fe00); ks <= 0x1008ffff; ks++ {
		out = append(out, ks)
	}
	for ks := uint32(0x10081000); ks <= 0x10081300; ks++ {
		out = append(out, ks)
	}
	return out
}

func TestKeysymConversionsMatchLibxkbcommon(t *testing.T) {
	type miss struct{ ks, got, want uint32 }
	var utf, upper, lower []miss
	for _, ks := range keysymsUnderTest() {
		g := xkb.Keysym(ks)
		u := g.UTF32()
		if u < 0 {
			u = 0
		}
		if want := xkbref.KeysymToUTF32(ks); uint32(u) != want {
			utf = append(utf, miss{ks, uint32(u), want})
		}
		if got, want := uint32(g.ToUpper()), xkbref.KeysymToUpper(ks); got != want {
			upper = append(upper, miss{ks, got, want})
		}
		if got, want := uint32(g.ToLower()), xkbref.KeysymToLower(ks); got != want {
			lower = append(lower, miss{ks, got, want})
		}
	}
	report := func(what string, ms []miss) {
		if len(ms) == 0 {
			return
		}
		t.Errorf("%s: %d mismatches", what, len(ms))
		for i, m := range ms {
			if i == 40 {
				break
			}
			t.Logf("  %s(0x%x %s) = 0x%x, libxkbcommon 0x%x", what, m.ks, xkb.Keysym(m.ks).Name(), m.got, m.want)
		}
	}
	report("utf32", utf)
	report("upper", upper)
	report("lower", lower)
}

func TestUTF32ToKeysymMatchesLibxkbcommon(t *testing.T) {
	bad := 0
	for u := uint32(0); u <= 0x2ffff; u++ {
		if got, want := uint32(xkb.UTF32ToKeysym(rune(u))), xkbref.UTF32ToKeysym(u); got != want {
			if bad < 12 {
				t.Logf("U+%04X: 0x%x, libxkbcommon 0x%x", u, got, want)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%d mismatches", bad)
	}
}

func TestKeysymNamesRoundTrip(t *testing.T) {
	bad := 0
	for _, ks := range keysymsUnderTest() {
		name := xkbref.KeysymName(ks)
		if name == "" {
			continue
		}
		if got, want := uint32(xkb.KeysymFromName(name)), xkbref.KeysymFromName(name); got != want {
			if bad < 12 {
				t.Logf("%q: 0x%x, libxkbcommon 0x%x", name, got, want)
			}
			bad++
		}
	}
	for _, name := range []string{"U00E9", "U20AC", "U1F525", "0x61", "XF86_Switch_VT_1", "Udead", "0xZZ", "dead_acute", "Multi_key", "Uxyz"} {
		if got, want := uint32(xkb.KeysymFromName(name)), xkbref.KeysymFromName(name); got != want {
			t.Logf("%q: 0x%x, libxkbcommon 0x%x", name, got, want)
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%d mismatches", bad)
	}
}
