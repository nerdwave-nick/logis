package input

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/xkb"
)

// keymapFd is an fd holding text, as wl_keyboard.keymap hands one over
// (NUL-terminated); the receiver owns it.
func keymapFd(t *testing.T, text string) (int, uint32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keymap")
	if err := os.WriteFile(path, append([]byte(text), 0), 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	return fd, uint32(len(text) + 1)
}

func closed(fd int) bool {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	return errno == syscall.EBADF
}

// sinkKeyboard is a keyboard on the us keymap whose actions are collected.
func sinkKeyboard(t *testing.T) (*Keyboard, *[]Action, *strings.Builder) {
	t.Helper()
	k := NewKeyboard(testCompose(t))
	var got []Action
	var log strings.Builder
	k.SetSink(func(a Action) { got = append(got, a) })
	k.SetLog(&log)
	fd, size := keymapFd(t, keymapText(t, "us"))
	k.Keymap(KeymapXkbV1, fd, size)
	if !closed(fd) {
		t.Fatal("the keymap fd stays open")
	}
	return k, &got, &log
}

func tap(k *Keyboard, code uint32) {
	k.Key(0, 0, code, KeyPressed)
	k.Key(0, 0, code, KeyReleased)
}

func TestKeyEventsBecomeActionsForTheSink(t *testing.T) {
	k, got, _ := sinkKeyboard(t)
	k.Enter(1, nil)
	tap(k, keyA)
	k.Modifiers(0, mShift, 0, 0, 0)
	tap(k, keyA)
	k.Modifiers(0, 0, 0, 0, 0)
	tap(k, keyEsc)
	k.Leave(2)
	want := []Action{Insert("a"), Insert("A"), ActDismiss, ActDismiss}
	if len(*got) != len(want) {
		t.Fatalf("actions %+v, want %+v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Errorf("action %d: %+v, want %+v", i, (*got)[i], want[i])
		}
	}
}

// niri sends a second keymap once a virtual keyboard types: it replaces
// the first mid-session and ends a sequence and a repeat in progress.
func TestALaterKeymapReplacesTheFirstMidSession(t *testing.T) {
	k, got, _ := sinkKeyboard(t)
	k.RepeatInfo(25, 300)
	k.Key(0, 0, keyA, KeyPressed)
	if _, ok := k.Deadline(); !ok {
		t.Fatal("a held a does not repeat")
	}
	fd, size := keymapFd(t, keymapText(t, "us"))
	k.Keymap(KeymapXkbV1, fd, size)
	if _, ok := k.Deadline(); ok {
		t.Error("the repeat survived the new keymap")
	}
	k.Key(0, 0, keyA, KeyReleased)
	tap(k, keyCompose)
	tap(k, keyO)
	fd, size = keymapFd(t, keymapText(t, "us,ru"))
	k.Keymap(KeymapXkbV1, fd, size)
	*got = nil
	k.Modifiers(0, mShift, 0, 0, 0)
	tap(k, keyQuote)
	k.Modifiers(0, 0, 0, 0, 1)
	tap(k, keyG)
	want := []Action{Insert("\""), Insert("п")}
	if len(*got) != 2 || (*got)[0] != want[0] || (*got)[1] != want[1] {
		t.Errorf("after the new keymap: %+v, want %+v (no stale sequence, the ru layout)", *got, want)
	}
}

func TestABrokenKeymapKeepsTheLastGoodOne(t *testing.T) {
	k, got, log := sinkKeyboard(t)
	fd, size := keymapFd(t, "xkb_keymap { xkb_symbols { key <AC01> { [ a ] ")
	k.Keymap(KeymapXkbV1, fd, size)
	if !closed(fd) {
		t.Error("the broken keymap's fd stays open")
	}
	if !strings.Contains(log.String(), "keymap") {
		t.Errorf("nothing logged: %q", log.String())
	}
	tap(k, keyA)
	if len(*got) != 1 || (*got)[0] != Insert("a") {
		t.Errorf("after a broken keymap a types %+v", *got)
	}
}

// Only xkb_v1 keymaps of a sane size are read; every fd is closed.
func TestOnlySaneKeymapsAreReadAndEveryFdIsClosed(t *testing.T) {
	k, got, log := sinkKeyboard(t)
	for _, c := range []struct {
		name   string
		format uint32
		size   uint32
	}{
		{"no_keymap", 0, 10},
		{"empty", KeymapXkbV1, 0},
		{"larger than accepted", KeymapXkbV1, 64 << 20},
	} {
		fd, _ := keymapFd(t, "garbage")
		k.Keymap(c.format, fd, c.size)
		if !closed(fd) {
			t.Errorf("%s: the fd stays open", c.name)
		}
	}
	tap(k, keyA)
	if len(*got) != 1 || (*got)[0] != Insert("a") {
		t.Errorf("a types %+v", *got)
	}
	if !strings.Contains(log.String(), "larger") {
		t.Errorf("an oversized keymap is not reported: %q", log.String())
	}
}

func TestRepeatsDueReachTheSink(t *testing.T) {
	k, got, _ := sinkKeyboard(t)
	t0 := time.Now()
	k.SetClock(func() time.Time { return t0 })
	k.RepeatInfo(25, 300)
	k.Key(0, 0, keyA, KeyPressed)
	d, ok := k.Deadline()
	if !ok || !d.Equal(t0.Add(ms(300))) {
		t.Fatalf("deadline %v %v", d, ok)
	}
	k.Repeat(t0.Add(ms(345)))
	if len(*got) != 3 {
		t.Errorf("a pressed and repeated twice: %+v", *got)
	}
	k.Key(0, 0, keyA, KeyReleased)
	if _, ok := k.Deadline(); ok {
		t.Error("a deadline after the release")
	}
}

// harnessCompose is the E2E harness's XCOMPOSEFILE (internal/e2e).
const harnessCompose = "<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis\n" +
	"<Multi_key> <l> <g> : \"λ\"\n" +
	"<Multi_key> <e> <e> : \"e\u0301\"\n"

// nestedTrace is what research/go/xkb's e2eprobe recorded from a nested
// niri typing tests/e2e.rs's keyboard scenario through a virtual keyboard
// (two keymaps: niri's own, then the injector's, mid-session).
type nestedTrace struct {
	Keymaps []string
	Events  []struct {
		Kind   string
		Ms     int64
		Args   []uint32
		Keymap int
		Note   string
	}
}

// replay feeds a trace through the wl_keyboard adapter, firing repeats as
// the window's timer would, into a Query; it returns the query at each
// mark, the final query and the repeats.
func replay(t *testing.T, file string) (marks map[string]string, final string, repeats int) {
	b, err := os.ReadFile(filepath.Join("..", "xkb", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var tr nestedTrace
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatal(err)
	}
	ct, err := xkb.ComposeFromString(xkb.ComposeEnv{}, harnessCompose, "C")
	if err != nil {
		t.Fatal(err)
	}
	k := NewKeyboard(NewCompose(ct))
	q := &Query{}
	k.SetSink(func(a Action) {
		if a.Kind == EditQuery {
			q.Apply(a.Edit)
		}
	})
	t0 := time.Unix(0, 0)
	marks, last := map[string]string{}, ""
	for _, e := range tr.Events {
		now := t0.Add(time.Duration(e.Ms) * time.Millisecond)
		k.SetClock(func() time.Time { return now })
		for d, ok := k.Deadline(); ok && !d.After(now); d, ok = k.Deadline() {
			before := q.Text()
			k.Repeat(d)
			repeats += len(q.Text()) - len(before)
		}
		switch e.Kind {
		case "keymap":
			fd, size := keymapFd(t, tr.Keymaps[e.Keymap])
			k.Keymap(KeymapXkbV1, fd, size)
		case "repeat_info":
			k.RepeatInfo(int32(e.Args[0]), int32(e.Args[1]))
		case "enter":
			k.Enter(0, nil)
		case "leave":
			k.Leave(0)
		case "modifiers":
			k.Modifiers(0, e.Args[0], e.Args[1], e.Args[2], e.Args[3])
		case "key":
			k.Key(0, 0, e.Args[0], e.Args[1])
		case "mark":
			if last != "" {
				marks[last] = q.Text()
			}
			last = e.Note
		}
	}
	return marks, q.Text(), repeats
}

func TestTheNestedNiriTracesTypeWhatTheLayoutSays(t *testing.T) {
	for _, f := range []string{"e2e-nested-niri.json", "e2e-nested-niri-numlock.json"} {
		marks, final, repeats := replay(t, f)
		for mark, want := range map[string]string{
			"compose o Shift+quote -> ö":         "ö",
			"compose l g -> λ (XCOMPOSEFILE)":    "öλ",
			"compose e e -> e+U+0301, backspace": "öλ", // one grapheme, deleted whole
			"caps a, caps a":                     "öλAa",
			"numlock kp1 kp2, numlock kp3 kp1 b": "öλAa12b",
		} {
			if marks[mark] != want {
				t.Errorf("%s: after %q the query is %q, want %q", f, mark, marks[mark], want)
			}
		}
		// x held 502 ms at 30/s after 300 ms: x, then repeats at 300,
		// 333, 367, 400, 433, 467 and 500 ms.
		if want := "öλAa12b" + strings.Repeat("x", 1+7); final != want || repeats != 7 {
			t.Errorf("%s: final %q with %d repeats, want %q with 7", f, final, repeats, want)
		}
	}
}
