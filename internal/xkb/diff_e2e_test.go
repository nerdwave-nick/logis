//go:build xkbref

package xkb_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
	"github.com/nerdwave-nick/logis/internal/xkb/xkbref"
)

// TestNestedNiriTracesMatchLibxkbcommon replays the wl_keyboard events
// cmd/e2eprobe recorded from a nested niri through both implementations
// and compares every pressed key: keysym, text, consumed modifiers and
// the Compose state after feeding it.
func TestNestedNiriTracesMatchLibxkbcommon(t *testing.T) {
	ctx := xkbref.NewContext()
	defer ctx.Unref()
	const table = "<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis\n<Multi_key> <l> <g> : \"λ\"\n<Multi_key> <e> <e> : \"é\"\n"
	for _, f := range []string{"testdata/e2e-nested-niri.json", "testdata/e2e-nested-niri-numlock.json"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tr struct {
			Keymaps []string
			Events  []struct {
				Kind   string
				Args   []uint32
				Keymap int
			}
		}
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatal(err)
		}
		rct, err := ctx.ComposeFromBuffer(table, "C")
		if err != nil {
			t.Fatal(err)
		}
		gct, _ := xkb.ComposeFromString(xkb.ComposeEnv{}, table, "C")
		rcs, gcs := rct.NewState(), gct.NewState()
		var rs *xkbref.State
		var rk *xkbref.Keymap
		var gs *xkb.State
		presses := 0
		for _, e := range tr.Events {
			switch e.Kind {
			case "keymap":
				if rs != nil {
					rs.Unref()
					rk.Unref()
				}
				rk, err = ctx.KeymapFromString(tr.Keymaps[e.Keymap])
				if err != nil {
					t.Fatal(err)
				}
				gk, err := xkb.ParseKeymap(tr.Keymaps[e.Keymap])
				if err != nil {
					t.Fatal(err)
				}
				rs, gs = rk.NewState(), xkb.NewState(gk)
			case "modifiers":
				rs.UpdateMask(e.Args[0], e.Args[1], e.Args[2], 0, 0, e.Args[3])
				gs.UpdateMask(xkb.Mask(e.Args[0]), xkb.Mask(e.Args[1]), xkb.Mask(e.Args[2]), 0, 0, e.Args[3])
			case "key":
				if e.Args[1] != 1 {
					continue
				}
				presses++
				kc := e.Args[0] + 8
				if g, w := uint32(gs.OneSym(kc)), rs.OneSym(kc); g != w {
					t.Errorf("%s key %d: sym %#x, libxkbcommon %#x", f, e.Args[0], g, w)
				}
				if g, w := gs.UTF8(kc), rs.UTF8(kc); g != w {
					t.Errorf("%s key %d: utf8 %q, libxkbcommon %q", f, e.Args[0], g, w)
				}
				if g, w := uint32(gs.Consumed(kc, xkb.ConsumedXKB)), rs.Consumed(kc, false); g != w {
					t.Errorf("%s key %d: consumed %#x, libxkbcommon %#x", f, e.Args[0], g, w)
				}
				rcs.Feed(rs.OneSym(kc))
				gcs.Feed(gs.OneSym(kc))
				if xkb.ComposeStatus(rcs.Status()) != gcs.Status() || rcs.UTF8() != gcs.UTF8() {
					t.Errorf("%s key %d: compose %d %q, libxkbcommon %d %q", f, e.Args[0], gcs.Status(), gcs.UTF8(), rcs.Status(), rcs.UTF8())
				}
				if s := rcs.Status(); s == 2 || s == 3 {
					rcs.Reset()
					gcs.Reset()
				}
			}
		}
		t.Logf("%s: %d key presses identical", f, presses)
		rs.Unref()
		rk.Unref()
		rcs.Unref()
		rct.Unref()
	}
}
