package xkb

import (
	"os"
	"testing"
)

// FuzzComposeFromString: no table text panics or hangs, and a table that
// loads can be run with any keysyms.
// Run: go test -run - -fuzz FuzzComposeFromString -fuzztime 60s -parallel 4 ./internal/xkb
func FuzzComposeFromString(f *testing.F) {
	f.Add(table)
	f.Add("include \"%L\"\n<Multi_key> <x> <x> : \"×\"\n")
	f.Add("!Shift ~Ctrl None <a> <b> : \"\\\"q\\101\\x42\" U1F525 # c\n<0x61> : eacute\n")
	if b, err := os.ReadFile("/usr/share/X11/locale/en_US.UTF-8/Compose"); err == nil && len(b) < 1<<20 {
		f.Add(string(b[:min(len(b), 64<<10)]))
	}
	f.Fuzz(func(t *testing.T, text string) {
		ct, err := ComposeFromString(ComposeEnv{}, text, "C")
		if err != nil {
			return
		}
		s := ct.NewState()
		for _, ks := range []Keysym{KeyMultiKey, 'a', 'b', KeyShiftL, 'x', KeyDeadAcute, 0x1f0000} {
			s.Feed(ks)
			s.Status()
			s.UTF8()
			s.Keysym()
			if s.Status() == ComposeComposed || s.Status() == ComposeCancelled {
				s.Reset()
			}
		}
		ct.Len()
	})
}
