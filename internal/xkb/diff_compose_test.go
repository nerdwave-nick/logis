//go:build xkbref

package xkb_test

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
	"github.com/nerdwave-nick/logis/internal/xkb/xkbref"
)

// isolate points HOME and XDG at an empty directory and clears
// XCOMPOSEFILE, so both sides load only the system tables.
func isolate(t *testing.T) xkb.ComposeEnv {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XCOMPOSEFILE", "")
	return xkb.EnvFromOS()
}

// compareTables feeds every sequence of the reference table to both
// states, step by step, plus a cancelling key after every proper prefix.
func compareTables(t *testing.T, name string, rt *xkbref.ComposeTable, gt *xkb.ComposeTable) {
	entries := rt.Entries()
	if gt.Len() != len(entries) {
		t.Errorf("%s: %d sequences, libxkbcommon %d", name, gt.Len(), len(entries))
	}
	rs, gs := rt.NewState(), gt.NewState()
	defer rs.Unref()
	bad := 0
	check := func(what string, seq []uint32) {
		if xkb.ComposeStatus(rs.Status()) != gs.Status() || rs.UTF8() != gs.UTF8() || rs.Keysym() != uint32(gs.Keysym()) {
			if bad < 10 {
				t.Logf("%s: %s %s: status %d %q %#x, libxkbcommon %d %q %#x", name, what, names(seq),
					gs.Status(), gs.UTF8(), uint32(gs.Keysym()), rs.Status(), rs.UTF8(), rs.Keysym())
			}
			bad++
		}
	}
	const breaker = 0x1f0000 // a keysym in no sequence
	for _, e := range entries {
		rs.Reset()
		gs.Reset()
		for i, ks := range e.Seq {
			rs.Feed(ks)
			gs.Feed(xkb.Keysym(ks))
			check("step", e.Seq[:i+1])
			// A real Shift between keys never cancels.
			rs.Feed(0xffe1)
			gs.Feed(0xffe1)
			check("shift", e.Seq[:i+1])
		}
		if len(e.Seq) > 1 {
			rs.Reset()
			gs.Reset()
			rs.Feed(e.Seq[0])
			gs.Feed(xkb.Keysym(e.Seq[0]))
			rs.Feed(breaker)
			gs.Feed(breaker)
			check("cancel", e.Seq[:1])
			rs.Feed(e.Seq[0])
			gs.Feed(xkb.Keysym(e.Seq[0]))
			check("after-cancel", e.Seq[:1])
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d mismatches over %d sequences", name, bad, len(entries))
	} else {
		t.Logf("%s: %d sequences identical", name, len(entries))
	}
}

func TestSystemComposeTablesMatchLibxkbcommon(t *testing.T) {
	env := isolate(t)
	ctx := xkbref.NewContext()
	defer ctx.Unref()
	for _, locale := range []string{"en_US.UTF-8", "C", "de_DE.UTF-8", "fr_FR.UTF-8", "el_GR.UTF-8", "pt_BR.UTF-8", "ja_JP.UTF-8", "fi_FI.UTF-8", "km_KH.UTF-8"} {
		rt, err := ctx.ComposeFromLocale(locale)
		if err != nil {
			t.Logf("%s: libxkbcommon has no table (%v); skipped", locale, err)
			continue
		}
		gt, err := xkb.ComposeFromLocale(env, locale)
		if err != nil {
			t.Errorf("%s: %v", locale, err)
			rt.Unref()
			continue
		}
		compareTables(t, locale+" ("+gt.Path+")", rt, gt)
		rt.Unref()
	}
}

// tables exercise the parser: the E2E harness's table, includes,
// overrides, escapes, modifiers and keysym-only results.
var tables = map[string]string{
	"e2e":       "<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis\n<Multi_key> <l> <g> : \"λ\"\n<Multi_key> <e> <e> : \"é\"\n",
	"include-L": "include \"%L\"\n<Multi_key> <o> <quotedbl> : \"Ø\"\n<Multi_key> <x> <x> : \"×\"\n",
	"overrides": `<Multi_key> <a> : "1"
<Multi_key> <a> <b> : "2"
<Multi_key> <c> <d> : "3"
<Multi_key> <c> : "4"
<Multi_key> <e> : "5"
<Multi_key> <e> : "6" X
<Multi_key> <e> : "6" X
`,
	"escapes": `<Multi_key> <1> : "\"q\""
<Multi_key> <2> : "\\"
<Multi_key> <3> : "\101\x42\103"
<Multi_key> <4> : "a#b" # comment
<Multi_key> <plus> <plus> : "#"
<Multi_key> <5> : "\q"
<Multi_key> <6> : "\x"
`,
	"modifiers": `!Shift <a> <b> : "ab"
None <c> : "c"
~Ctrl Alt <d> <e> : "de"
<f> <g> : eacute
<U00E9> <U20AC> : U1F525
<dead_acute> <space> : "'" apostrophe
<0x61> <0x62> <0x63> : "abc"
`,
	"errors": `<Multi_key> <nosuchkeysym> : "x"
<Multi_key> <y> : nosuchkeysym
<Multi_key> <z> : "a" "b"
<Multi_key> <z> <z> : ""
<Multi_key> <w> :
<a> <b> <c> <d> <e> <f> <g> <h> <i> <j> <k> : "too long"
<a> <b> <c> <d> <e> <f> <g> <h> <i> <j> : "ten"
Bogus <q> : "q"
<Multi_key> <v> : "ok"
`,
}

func TestComposeTablesFromTextMatchLibxkbcommon(t *testing.T) {
	env := isolate(t)
	ctx := xkbref.NewContext()
	defer ctx.Unref()
	for name, text := range tables {
		rt, err := ctx.ComposeFromBuffer(text, "en_US.UTF-8")
		if err != nil {
			t.Errorf("%s: libxkbcommon: %v", name, err)
			continue
		}
		gt, err := xkb.ComposeFromString(env, text, "en_US.UTF-8")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			rt.Unref()
			continue
		}
		compareTables(t, name, rt, gt)
		rt.Unref()
	}
}
