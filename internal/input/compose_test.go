package input

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
)

// Tests ported from Rust src/input/compose.rs.

// composeTable is Rust's test table: no test depends on the machine's
// locale or anyone's ~/.XCompose.
const composeTable = "\n<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis\n" +
	"<Multi_key> <e> <e> : \"é\"\n" +
	"<dead_acute> <a> : \"á\" aacute\n"

func testCompose(t testing.TB) *Compose {
	t.Helper()
	ct, err := xkb.ComposeFromString(xkb.ComposeEnv{}, composeTable, "C")
	if err != nil {
		t.Fatal(err)
	}
	return NewCompose(ct)
}

func wantFed(t *testing.T, c *Compose, ks xkb.Keysym, fed Fed, text string) {
	t.Helper()
	if got, s := c.Feed(ks); got != fed || s != text {
		t.Errorf("%v: %v %q, want %v %q", ks, got, s, fed, text)
	}
}

func TestASequenceComposesAndSwallowsItsKeys(t *testing.T) {
	c := testCompose(t)
	wantFed(t, c, xkb.KeyMultiKey, Swallowed, "")
	wantFed(t, c, 'o', Swallowed, "")
	wantFed(t, c, '"', Composed, "ö")
	wantFed(t, c, 'o', Pass, "") // the sequence is over
}

func TestShiftPressedInsideASequenceNeverCancelsIt(t *testing.T) {
	c := testCompose(t)
	c.Feed(xkb.KeyMultiKey)
	c.Feed('o')
	for _, m := range []string{"Shift_L", "Shift_R", "Control_L", "ISO_Level3_Shift"} {
		wantFed(t, c, xkb.KeysymFromName(m), Pass, "")
	}
	wantFed(t, c, '"', Composed, "ö")
}

func TestAMultiCodePointResultAndDeadKeysCompose(t *testing.T) {
	c := testCompose(t)
	c.Feed(xkb.KeyMultiKey)
	c.Feed('e')
	wantFed(t, c, 'e', Composed, "é")
	wantFed(t, c, xkb.KeyDeadAcute, Swallowed, "")
	wantFed(t, c, 'a', Composed, "á")
}

func TestAKeyOutsideTheTableCancelsAndIsSwallowed(t *testing.T) {
	c := testCompose(t)
	c.Feed(xkb.KeyMultiKey)
	wantFed(t, c, 'x', Swallowed, "")
	wantFed(t, c, 'x', Pass, "")
}

func TestResetForgetsASequenceInProgress(t *testing.T) {
	c := testCompose(t)
	c.Feed(xkb.KeyMultiKey)
	c.Feed('o')
	c.Reset()
	wantFed(t, c, '"', Pass, "")
}

// The table comes from the process's locale and environment
// (libxkbcommon's lookup); XCOMPOSEFILE wins.
func TestTheTableComesFromTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "XCompose")
	if err := os.WriteFile(file, []byte("<Multi_key> <l> <g> : \"Ψ\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XCOMPOSEFILE", file)
	t.Setenv("LC_ALL", "C")
	c := ComposeFromEnv()
	if c == nil {
		t.Fatal("no table")
	}
	c.Feed(xkb.KeyMultiKey)
	c.Feed('l')
	wantFed(t, c, 'g', Composed, "Ψ")
}
