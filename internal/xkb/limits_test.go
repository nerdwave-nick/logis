package xkb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wrap puts sections into a keymap with the keycodes a, b and c.
func wrap(sections string) string {
	return "xkb_keymap {\n xkb_keycodes { <a> = 38; <b> = 56; <c> = 54; };\n" + sections + "\n};\n"
}

func refused(t *testing.T, name, src, reason string) {
	t.Helper()
	start := time.Now()
	km, err := ParseKeymap(src)
	var e *Error
	if !errors.As(err, &e) || km != nil {
		t.Fatalf("%s: accepted (%v); want an *Error", name, err)
	}
	if !strings.Contains(e.Error(), reason) {
		t.Errorf("%s: %q does not say %q", name, e.Error(), reason)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("%s: refused only after %v", name, d)
	}
}

func TestAKeymapLargerThanTheLimitIsRefused(t *testing.T) {
	big := wrap("xkb_symbols { key <a> { [ a ] }; };" + strings.Repeat(" ", MaxKeymapSize))
	refused(t, "oversized", big, "more than the")
}

// Nesting beyond maxDepth is refused before it can exhaust the stack.
func TestDeepNestingIsRefusedNotRecursedInto(t *testing.T) {
	n := MaxKeymapSize / 8
	for name, src := range map[string]string{
		"parentheses": wrap("xkb_types { type \"T\" { modifiers = " + strings.Repeat("(", n) + "Shift" + strings.Repeat(")", n) + "; }; };"),
		"unary":       wrap("xkb_types { type \"T\" { modifiers = " + strings.Repeat("-", n) + "Shift; }; };"),
		"blocks":      "xkb_keymap " + strings.Repeat("{ x ", n) + strings.Repeat("}; ", n),
		"lists":       wrap("xkb_symbols { key <a> { " + strings.Repeat("[ ", n) + "a" + strings.Repeat(" ]", n) + " }; };"),
	} {
		refused(t, name, src, "nested deeper than")
	}
	// Real keymaps nest a few levels; maxDepth leaves room.
	ok := wrap("xkb_types { type \"T\" { modifiers = ((((Shift)))); map[((Shift))] = Level2; }; };")
	if _, err := ParseKeymap(ok); err != nil {
		t.Fatal(err)
	}
}

func TestCountsBeyondTheLimitsAreRefused(t *testing.T) {
	var interprets strings.Builder
	for i := 0; i <= maxInterprets; i++ {
		fmt.Fprintf(&interprets, "interpret Any+AnyOf(Shift) { repeat = false; };\n")
	}
	refused(t, "interprets", wrap("xkb_compat {\n"+interprets.String()+"};"), "interprets")

	var entries strings.Builder
	for i := 0; i <= maxTypeEntries; i++ {
		fmt.Fprintf(&entries, "map[%d] = Level2;\n", i)
	}
	refused(t, "type entries", wrap("xkb_types { type \"T\" { modifiers = all;\n"+entries.String()+"}; };"), "entries")

	var types strings.Builder
	for i := 0; i <= maxTypes; i++ {
		fmt.Fprintf(&types, "type \"T%d\" { modifiers = Shift; };\n", i)
	}
	refused(t, "types", wrap("xkb_types {\n"+types.String()+"};"), "key types")

	refused(t, "level", wrap(fmt.Sprintf("xkb_types { type \"T\" { map[Shift] = Level%d; }; };", maxLevel+1)), "bad level")
	if _, err := ParseKeymap(wrap(fmt.Sprintf("xkb_types { type \"T\" { map[Shift] = Level%d; }; };", maxLevel))); err != nil {
		t.Errorf("Level%d: %v", maxLevel, err)
	}
	refused(t, "group", wrap(fmt.Sprintf("xkb_symbols { key <a> { symbols[Group%d] = [ a ] }; };", maxGroups+1)), "bad group")
	refused(t, "virtual modifiers", wrap("xkb_types { virtual_modifiers "+vmodNames(maxMods)+"; };"), "modifiers")
}

func vmodNames(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("V%d", i)
	}
	return strings.Join(names, ",")
}

func TestMalformedKeymapsAreErrorsNotPanics(t *testing.T) {
	for name, src := range map[string]string{
		"empty":              "",
		"no keymap block":    "xkb_keycodes { <a> = 38; };",
		"unterminated":       "xkb_keymap { xkb_symbols { key <a> { [ a ] }; ",
		"string":             "xkb_keymap { xkb_keycodes \"abc",
		"comment":            "xkb_keymap { /* never closed",
		"key name":           "xkb_keymap { xkb_keycodes { <ab",
		"stray brace":        "xkb_keymap { }; };",
		"keycode overflow":   wrap("xkb_keycodes { <d> = 0x1ffffffff; };"),
		"keycode not number": wrap("xkb_keycodes { <d> = Shift; };"),
		"bad mask":           wrap("xkb_types { type \"T\" { modifiers = Nope; }; };"),
		"bad modmap":         wrap("xkb_symbols { modifier_map NumLock { <a> }; };"),
		"vmod name":          wrap("xkb_types { virtual_modifiers 42; };"),
		"missing semicolon":  wrap("xkb_types { type \"T\" { modifiers = Shift } };"),
	} {
		t.Run(name, func(t *testing.T) {
			if km, err := ParseKeymap(src); err == nil || km != nil {
				t.Errorf("accepted: %v", km)
			}
		})
	}
}

// The worst a keymap at the size limit can ask: as many Any interprets as
// allowed, matched against as many keysyms as fit in the rest. It is
// decided (here: refused as too complex) well within a second.
func TestTheWorstKeymapAtTheSizeLimitIsDecidedQuickly(t *testing.T) {
	var b strings.Builder
	b.WriteString("xkb_keymap {\nxkb_keycodes {\n")
	const keys = 2000
	for i := range keys {
		fmt.Fprintf(&b, "<k%d> = %d;\n", i, i+8)
	}
	b.WriteString("};\nxkb_types { type \"WIDE\" { modifiers = Shift; map[Shift] = Level2048; }; };\nxkb_compat {\n")
	for range maxInterprets - 1 {
		b.WriteString("interpret Any+Exactly(Lock) { };\n")
	}
	b.WriteString("};\nxkb_symbols {\n")
	for i := 0; b.Len() < MaxKeymapSize-200; i++ {
		fmt.Fprintf(&b, "key <k%d> { type = \"WIDE\", [ a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p ] };\n", i%keys)
	}
	b.WriteString("};\n};\n")
	start := time.Now()
	_, err := ParseKeymap(b.String())
	d := time.Since(start)
	t.Logf("%d bytes decided in %v: %v", b.Len(), d, err)
	if err == nil || !strings.Contains(err.Error(), "too complex") {
		t.Errorf("got %v, want too complex", err)
	}
	if d > time.Second && !raceEnabled {
		t.Errorf("compiling took %v", d)
	}
}

// Matching interprets costs keysyms × interprets; a keymap asking for
// more than the work budget is refused, quickly, as too complex.
func TestAKeymapTooComplexToCompileIsRefused(t *testing.T) {
	var b strings.Builder
	b.WriteString("xkb_keymap {\nxkb_keycodes {\n")
	for i := range 600 {
		fmt.Fprintf(&b, "<k%d> = %d;\n", i, i+8)
	}
	b.WriteString("};\nxkb_types { type \"WIDE\" { modifiers = Shift; map[Shift] = Level16; }; };\nxkb_compat {\n")
	for range 1000 {
		b.WriteString("interpret Any+Exactly(Lock) { };\n")
	}
	b.WriteString("};\nxkb_symbols {\n")
	for i := range 600 {
		fmt.Fprintf(&b, "key <k%d> { type = \"WIDE\", [ a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p ] };\n", i)
	}
	b.WriteString("};\n};\n")
	refused(t, "complex", b.String(), "too complex")
}

// Real keymaps use a sliver of the budget.
func TestRealKeymapsStayFarWithinTheWorkBudget(t *testing.T) {
	for _, name := range []string{"live-niri.xkb", "xkbcli/us_de--grp_win_space_toggle.xkb", "xkbcli/de-neo-.xkb"} {
		b, err := os.ReadFile(filepath.Join("testdata", "keymaps", name))
		if err != nil {
			t.Fatal(err)
		}
		c, err := compile(string(b))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d work units of %d", name, c.work, maxWork)
		if c.work > maxWork/100 {
			t.Errorf("%s: %d work units", name, c.work)
		}
	}
}
