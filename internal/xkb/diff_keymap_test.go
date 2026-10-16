//go:build xkbref

package xkb_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
	"github.com/nerdwave-nick/logis/internal/xkb/xkbref"
)

// rmlvo lists the layouts the differential test compiles with
// libxkbcommon (from the system's xkeyboard-config) and serialises, as a
// compositor would, before both sides parse the text.
var rmlvo = []struct{ layout, variant, options string }{
	{"us", "", ""}, {"us", "", "compose:menu"}, {"us", "intl", ""}, {"us", "dvorak", ""},
	{"us", "colemak", ""}, {"us", "altgr-intl", ""}, {"gb", "", ""}, {"de", "", ""},
	{"de", "nodeadkeys", ""}, {"de", "neo", ""}, {"fr", "", ""}, {"fr", "bepo", ""},
	{"ch", "", ""}, {"ch", "fr", ""}, {"it", "", ""}, {"es", "", ""}, {"pt", "", ""},
	{"br", "", ""}, {"se", "", ""}, {"no", "", ""}, {"dk", "", ""}, {"fi", "", ""},
	{"pl", "", ""}, {"cz", "", ""}, {"sk", "", ""}, {"hu", "", ""}, {"ro", "", ""},
	{"tr", "", ""}, {"ru", "", ""}, {"ua", "", ""}, {"by", "", ""}, {"gr", "", ""},
	{"il", "", ""}, {"ara", "", ""}, {"ir", "", ""}, {"in", "", ""}, {"th", "", ""},
	{"jp", "", ""}, {"kr", "", ""}, {"ca", "multix", ""}, {"lt", "", ""}, {"lv", "", ""},
	{"is", "", ""}, {"hr", "", ""}, {"rs", "", ""}, {"latam", "", ""}, {"epo", "", ""},
	{"us,ru", "", "grp:alt_shift_toggle"}, {"us,de", "", "grp:win_space_toggle"},
	{"us,ua,ru", "", "grp:caps_toggle"}, {"us", "", "caps:escape"}, {"us", "", "ctrl:nocaps"},
	{"us", "", "lv3:ralt_switch,nbsp:level3"}, {"us", "", "numpad:mac"},
	{"us", "", "numpad:microsoft"}, {"us", "", "caps:swapescape,shift:both_capslock"},
	{"de", "neo", "lv5:ralt_switch"}, {"us", "", "keypad:pointerkeys"}, {"us", "", "eurosign:e,compose:ralt"},
}

type keymapCase struct {
	name string
	text string
}

func keymapCases(t *testing.T, ctx *xkbref.Context) []keymapCase {
	var out []keymapCase
	// The owner's live niri keymap, the injector's keymap a nested niri
	// sent, and keymaps serialised with keysym names (xkbcli
	// compile-keymap --format 1, as compositors on older libxkbcommon
	// send them).
	files, _ := filepath.Glob("testdata/keymaps/*.xkb")
	named, _ := filepath.Glob("testdata/keymaps/xkbcli/*.xkb")
	for _, f := range append(files, named...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, keymapCase{strings.TrimPrefix(f, "testdata/keymaps/"), string(b)})
	}
	if len(out) < 15 {
		t.Fatalf("only %d keymap files", len(out))
	}
	for _, n := range rmlvo {
		km, err := ctx.KeymapFromNames("evdev", "pc105", n.layout, n.variant, n.options)
		if err != nil {
			t.Fatalf("libxkbcommon cannot compile %v: %v", n, err)
		}
		out = append(out, keymapCase{fmt.Sprintf("%s(%s)[%s]", n.layout, n.variant, n.options), km.String()})
		km.Unref()
	}
	return out
}

type masks struct{ dep, lat, lock, group uint32 }

// stateMasks are the states compared: what a compositor serialises after
// real modifier and lock key presses (every modifier key alone, tapped,
// and in pairs, from each layout), plus every real-modifier mask with
// Caps and Num Lock locked or not.
// XKBDIFF_FULL=1 compares every real-modifier mask instead of the subset.
var full = os.Getenv("XKBDIFF_FULL") == "1"

func stateMasks(km *xkbref.Keymap) []masks {
	seen := map[masks]bool{}
	var out []masks
	add := func(m masks) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	lo, hi := km.KeyRange()
	var modKeys []uint32
	st := km.NewState()
	defer st.Unref()
	for kc := lo; kc <= hi; kc++ {
		for _, ks := range st.Syms(kc) {
			if xkb.Keysym(ks).IsModifier() || (ks >= 0xfe01 && ks <= 0xfe0f) {
				modKeys = append(modKeys, kc)
				break
			}
		}
	}
	for g := 0; g < max(km.NumLayouts(), 1); g++ {
		for d := uint32(0); d < 256; d++ {
			for _, l := range []uint32{0, 1 << 1, 1 << 4, 1<<1 | 1<<4} {
				if full || d&0xa5 == d || d < 16 { // a broad, bounded subset
					add(masks{d, 0, l, uint32(g)})
				}
			}
		}
		for _, a := range modKeys {
			for _, b := range append([]uint32{0}, modKeys...) {
				for _, tapA := range []bool{false, true} {
					s := km.NewState()
					s.UpdateMask(0, 0, 0, 0, 0, uint32(g))
					s.UpdateKey(a, true)
					if tapA {
						s.UpdateKey(a, false)
					}
					if b != 0 {
						s.UpdateKey(b, true)
					}
					d, l, k, grp := s.Serialize()
					s.Unref()
					add(masks{d, l, k, grp})
				}
			}
		}
	}
	return out
}

func TestKeymapsMatchLibxkbcommon(t *testing.T) {
	ctx := xkbref.NewContext()
	defer ctx.Unref()
	total, compared := 0, 0
	for _, c := range keymapCases(t, ctx) {
		rk, err := ctx.KeymapFromString(c.text)
		if err != nil {
			t.Errorf("%s: libxkbcommon: %v", c.name, err)
			continue
		}
		gk, err := xkb.ParseKeymap(c.text)
		if err != nil {
			t.Errorf("%s: parse: %v", c.name, err)
			continue
		}
		bad := diffKeymap(t, c.name, rk, gk)
		rk.Unref()
		total += bad
		compared++
	}
	t.Logf("%d keymaps compared, %d mismatches", compared, total)
	if total > 0 {
		t.Fail()
	}
}

func diffKeymap(t *testing.T, name string, rk *xkbref.Keymap, gk *xkb.Keymap) int {
	bad := 0
	report := func(format string, args ...any) {
		if bad < 8 {
			t.Logf("%s: "+format, append([]any{name}, args...)...)
		}
		bad++
	}
	if rk.NumMods() != gk.NumMods() {
		report("num mods %d, libxkbcommon %d", gk.NumMods(), rk.NumMods())
	}
	for i := 0; i < min(rk.NumMods(), gk.NumMods()); i++ {
		if gk.ModIndex(rk.ModName(i)) != i {
			report("mod %d %q at %d", i, rk.ModName(i), gk.ModIndex(rk.ModName(i)))
		}
	}
	if rk.NumLayouts() != gk.NumLayouts() {
		report("layouts %d, libxkbcommon %d", gk.NumLayouts(), rk.NumLayouts())
	}
	lo, hi := rk.KeyRange()
	for kc := lo; kc <= hi; kc++ {
		if rk.Repeats(kc) != gk.Repeats(kc) {
			report("key %d repeats %v, libxkbcommon %v", kc, gk.Repeats(kc), rk.Repeats(kc))
		}
	}
	checks := 0
	for _, m := range stateMasks(rk) {
		rs, gs := rk.NewState(), xkb.NewState(gk)
		defer rs.Unref()
		rs.UpdateMask(m.dep, m.lat, m.lock, 0, 0, m.group)
		gs.UpdateMask(xkb.Mask(m.dep), xkb.Mask(m.lat), xkb.Mask(m.lock), 0, 0, m.group)
		if uint32(gs.Mods()) != rs.Mods() {
			report("%+v: mods %#x, libxkbcommon %#x", m, gs.Mods(), rs.Mods())
		}
		for i := 0; i < rk.NumMods(); i++ {
			if gs.ModIsActive(i) != rs.ModIsActive(i) {
				report("%+v: mod %s active %v", m, rk.ModName(i), gs.ModIsActive(i))
			}
		}
		for kc := lo; kc <= hi; kc++ {
			checks++
			layout := rs.Layout(kc)
			if g := gs.Layout(kc); g != layout {
				report("%+v key %d: layout %d, libxkbcommon %d", m, kc, g, layout)
				continue
			}
			if layout >= 0 && gs.Level(kc, layout) != rs.Level(kc, layout) {
				report("%+v key %d: level %d, libxkbcommon %d", m, kc, gs.Level(kc, layout), rs.Level(kc, layout))
			}
			want := rs.Syms(kc)
			got := gs.Syms(kc)
			g32 := make([]uint32, len(got))
			for i, s := range got {
				g32[i] = uint32(s)
			}
			if !slices.Equal(g32, want) {
				report("%+v key %d: syms %s, libxkbcommon %s", m, kc, names(g32), names(want))
			}
			if g, w := gs.UTF8(kc), rs.UTF8(kc); g != w {
				report("%+v key %d: utf8 %q, libxkbcommon %q", m, kc, g, w)
			}
			for _, gtk := range []bool{false, true} {
				mode := xkb.ConsumedXKB
				if gtk {
					mode = xkb.ConsumedGTK
				}
				if g, w := uint32(gs.Consumed(kc, mode)), rs.Consumed(kc, gtk); g != w {
					report("%+v key %d: consumed(gtk=%v) %#x, libxkbcommon %#x", m, kc, gtk, g, w)
				}
			}
		}
	}
	if testing.Verbose() {
		t.Logf("%s: %d key states, %d mismatches", name, checks, bad)
	}
	return bad
}

func names(syms []uint32) string {
	var b []string
	for _, s := range syms {
		b = append(b, xkb.Keysym(s).Name())
	}
	return "[" + strings.Join(b, " ") + "]"
}
