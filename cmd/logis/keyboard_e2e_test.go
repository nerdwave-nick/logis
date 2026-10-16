package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// The keyboard model in a real compositor (Rust tests/e2e.rs, "the keyboard
// model"): the debug panel reports what each key did (`query`, `select`,
// `accept`, `dismiss`), typed by the harness's virtual keyboard with real
// modifier presses and masks, only after the panel reported focus.

// typing is a debug panel in c, focused and drawn, with a virtual keyboard
// made before it (a headless seat gains its keyboard from it).
type typing struct {
	t       *testing.T
	harness *e2e.Harness
	kb      *e2e.VirtualKeyboard
	panel   *e2e.Traced
	focused e2e.Focused
}

func startTyping(t *testing.T, c *e2e.Compositor, adjust func(*exec.Cmd)) *typing {
	t.Helper()
	h := e2e.Connect(t, c)
	kb := h.VirtualKeyboard()
	cmd := c.Logis("daemon", "--debug-panel")
	if adjust != nil {
		adjust(cmd)
	}
	panel := e2e.Spawn(t, cmd)
	focused := panel.WaitForFocus()
	panel.WaitForFirstCommit()
	return &typing{t: t, harness: h, kb: kb, panel: panel, focused: focused}
}

// code is the US key for c.
func code(t *testing.T, c rune) uint32 {
	t.Helper()
	k, _, ok := e2e.USKey(c)
	if !ok {
		t.Fatalf("no US key for %q", c)
	}
	return k
}

func (p *typing) tap(code uint32)   { p.kb.Tap(p.focused, code) }
func (p *typing) typeText(s string) { p.kb.Type(p.focused, s) }
func (p *typing) toggle(l e2e.Lock) { p.kb.Toggle(p.focused, l) }
func (p *typing) wait(what string)  { p.panel.WaitFor(what, e2e.Is(what)) }
func (p *typing) chord(m e2e.Modifier, code uint32) {
	p.kb.Hold(p.focused, m)
	p.tap(code)
	p.kb.Release(p.focused, m)
}

// next is the next report starting with word (the sequence cursor moves
// past it), without the word: what the last key did.
func (p *typing) next(word string) string {
	p.t.Helper()
	return strings.TrimPrefix(p.panel.WaitFor(word, e2e.Word(word)).What, word+" ")
}

// query is the next query report, unquoted.
func (p *typing) query() string {
	p.t.Helper()
	quoted := p.next("query")
	var q string
	if err := json.Unmarshal([]byte(quoted), &q); err != nil {
		p.t.Fatalf("a quoted query: %s", quoted)
	}
	return q
}

func (p *typing) wantQuery(want, why string) {
	p.t.Helper()
	if got := p.query(); got != want {
		p.t.Errorf("query %q, want %q (%s)", got, want, why)
	}
}

// endsWith waits until the panel reported what and exited 0.
func (p *typing) endsWith(what string) {
	p.t.Helper()
	p.wait(what)
	if status := p.panel.WaitExit(); status != 0 {
		p.t.Errorf("%s ends the panel with %d", what, status)
	}
}

func count(events []trace.Event, what string) int {
	n := 0
	for _, e := range events {
		if e.What == what {
			n++
		}
	}
	return n
}

// checkDebugPanel waits until panel committed a frame drawn after its
// latest change (a query, a selection), captures until two consecutive
// captures agree, checks the harness's opaque black backdrop around the
// panel, and compares the panel cropped from the output with golden
// e2e/<name> (Rust e2e.rs check_panel).
func checkDebugPanel(t *testing.T, h *e2e.Harness, panel *e2e.Traced, name string) {
	t.Helper()
	panel.WaitForDrawn()
	capture := h.SettledCapture()
	for _, at := range [][2]int{{0, 0}, {capture.Width - 1, capture.Height - 1}} {
		if px := capture.Pixel(at[0], at[1]); px != [4]byte{0, 0, 0, 255} {
			t.Errorf("the backdrop at %v is %v, not opaque black around the panel", at, px)
		}
	}
	g := rendertest.CheckedIn(t)
	var reference *render.Image
	if !g.Update {
		reference, _ = g.Load("e2e", name)
	}
	cropped := e2e.CropCentred(capture, int(debugSize.W), int(debugSize.H), reference)
	g.Assert(t, "e2e", name, cropped, rendertest.Capture)
}

// The debug panel takes the keyboard, receives injected keys with real
// modifier presses and masks, and shows what they did on screen (Rust
// e2e.rs the_debug_panel_takes_focus_receives_keys_and_shows_them; the
// harness's own self-test of the same flow is in internal/e2e).
func TestE2ETheDebugPanelTakesFocusReceivesKeysAndShowsThem(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	p := startTyping(t, c, nil)
	checkDebugPanel(t, p.harness, p.panel, "debug-panel")

	// A real Shift press plus its mask around the letter, as a user types.
	p.typeText("A")
	for _, step := range []string{"key 42 pressed", "modifiers 1 0 0 0", "key 30 pressed",
		"key 30 released", "key 42 released", "modifiers 0 0 0 0"} {
		p.wait(step)
	}
	p.kb.Hold(p.focused, e2e.Control)
	p.wait("modifiers 4 0 0 0")
	p.kb.Release(p.focused, e2e.Control)

	for _, row := range []string{"select 1", "select 2"} {
		p.tap(e2e.KeyDown)
		at := p.panel.WaitFor(row, e2e.Is(row))
		p.panel.WaitForCommitAfter(at.NS)
	}
	checkDebugPanel(t, p.harness, p.panel, "debug-panel-row-2")

	p.tap(e2e.KeyEsc)
	p.endsWith("dismiss")
}

func TestE2EComposeLocksAndTheKeypadTypeWhatTheLayoutSays(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	p := startTyping(t, c, nil)

	// Menu o " with a real Shift press for the quote, as a user types it.
	p.tap(e2e.KeyCompose)
	p.typeText("o\"")
	for _, step := range []string{"key 127 pressed", "key 24 pressed", "key 42 pressed", "modifiers 1 0 0 0", "key 40 pressed"} {
		p.wait(step)
	}
	p.wantQuery("ö", "Shift inside the sequence did not cancel it")
	// Nested niri has a keyboard of its own: it sent that keymap first and
	// the injector's once it typed, so the panel replaced its keymap
	// mid-session and typed on. Headless sway has no keyboard but the
	// injector's (no input devices), so it sends that keymap alone; the
	// replacement itself is internal/input's
	// TestALaterKeymapReplacesTheFirstMidSession.
	want, which := 1, "the injector's"
	if c.Kind() == e2e.NestedNiri {
		want, which = 2, "the compositor's and then the injector's"
	}
	if n := count(p.panel.Events(), "keymap"); n < want {
		t.Errorf("%d keymaps compiled, want %s", n, which)
	}

	// A sequence only the harness's XCOMPOSEFILE knows.
	p.tap(e2e.KeyCompose)
	p.typeText("lg")
	p.wantQuery("öλ", "the harness's Compose table")
	checkDebugPanel(t, p.harness, p.panel, "debug-panel-query")

	// One grapheme of two code points; Backspace deletes it whole.
	p.tap(e2e.KeyCompose)
	p.typeText("ee")
	p.wantQuery("öλé", "e and a combining acute")
	p.tap(e2e.KeyBackspace)
	p.wantQuery("öλ", "Backspace deletes the grapheme")

	// Caps Lock: a letter typed without Shift is upper case, until it is off.
	p.toggle(e2e.CapsLock)
	p.typeText("a")
	p.wantQuery("öλA", "Caps Lock")
	p.toggle(e2e.CapsLock)
	p.typeText("a")
	p.wantQuery("öλAa", "Caps Lock off")

	// Num Lock: the keypad types digits; without it KP_3 is Page Down
	// (nothing) and KP_1 is End (the cursor is there already).
	p.toggle(e2e.NumLock)
	p.tap(e2e.KeyKP1)
	p.wantQuery("öλAa1", "KP_1 with Num Lock")
	p.tap(e2e.KeyKP2)
	p.wantQuery("öλAa12", "KP_2 with Num Lock")
	p.toggle(e2e.NumLock)
	p.tap(e2e.KeyKP3)
	p.tap(e2e.KeyKP1)
	p.typeText("b")
	p.wantQuery("öλAa12b", "the keypad typed nothing without Num Lock")

	p.tap(e2e.KeyEsc)
	p.endsWith("dismiss")
}

func TestE2EEveryEditingAndSelectionKeyDoesItsWork(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	var stdout strings.Builder
	p := startTyping(t, c, func(cmd *exec.Cmd) { cmd.Stdout = &stdout })

	p.typeText("rite of")
	p.wantQuery("r", "the first key")
	for range len("rite of") - 2 {
		p.query()
	}
	p.wantQuery("rite of", "the last key")
	p.typeText("  ")
	p.query()
	p.wantQuery("rite of  ", "two spaces")
	p.chord(e2e.Control, code(t, 'w'))
	p.wantQuery("rite ", "Ctrl+W: the spaces, then the word")
	p.tap(e2e.KeyBackspace)
	p.wantQuery("rite", "Backspace")
	// The cursor keys move the cursor (each move reports the query) and
	// Delete deletes after it.
	for _, s := range []struct {
		key  uint32
		text rune
		want string
	}{
		{e2e.KeyHome, 0, "rite"},
		{e2e.KeyDelete, 0, "ite"},
		{e2e.KeyEnd, 0, "ite"},
		{e2e.KeyLeft, 0, "ite"},
		{0, 'x', "itxe"},
		{e2e.KeyRight, 0, "itxe"},
		{0, 'y', "itxey"},
	} {
		if s.key != 0 {
			p.tap(s.key)
		} else {
			p.typeText(string(s.text))
		}
		p.wantQuery(s.want, "the cursor keys and Delete")
	}
	p.chord(e2e.Control, code(t, 'u'))
	p.wantQuery("", "Ctrl+U clears")

	// Down, Tab, Ctrl+N move on; Up, Shift+Tab, Ctrl+P move back.
	for _, s := range []struct {
		key  func()
		want string
	}{
		{func() { p.tap(e2e.KeyDown) }, "1"},
		{func() { p.tap(e2e.KeyTab) }, "2"},
		{func() { p.chord(e2e.Control, code(t, 'n')) }, "3"},
		{func() { p.tap(e2e.KeyUp) }, "2"},
		{func() { p.chord(e2e.Shift, e2e.KeyTab) }, "1"},
		{func() { p.chord(e2e.Control, code(t, 'p')) }, "0"},
	} {
		s.key()
		if got := p.next("select"); got != s.want {
			t.Errorf("select %s, want %s", got, s.want)
		}
	}
	// Caps Lock changes no shortcut.
	p.toggle(e2e.CapsLock)
	p.chord(e2e.Control, code(t, 'n'))
	if got := p.next("select"); got != "1" {
		t.Errorf("Ctrl+N with Caps Lock: select %s", got)
	}
	p.toggle(e2e.CapsLock)

	p.tap(e2e.KeyEnter)
	p.endsWith("accept 1")
	if stdout.String() != debugRows[1]+"\n" {
		t.Errorf("Return printed %q, want the selected row", stdout.String())
	}
}

func TestE2EEscapeCtrlGAndCtrlBracketEachDismiss(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	for _, k := range []struct {
		name string
		ctrl bool
		code uint32
	}{
		{"Escape", false, e2e.KeyEsc},
		{"Ctrl+G", true, code(t, 'g')},
		{"Ctrl+[", true, code(t, '[')},
	} {
		p := startTyping(t, c, nil)
		if k.ctrl {
			p.chord(e2e.Control, k.code)
		} else {
			p.tap(k.code)
		}
		t.Log(k.name)
		p.endsWith("dismiss")
	}
}

func TestE2EAHeldKeyRepeatsAtTheCompositorsRateAndStopsOnRelease(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	p := startTyping(t, c, nil)
	info := strings.Fields(p.panel.WaitSeen("repeat info", e2e.Word("repeat-info")).What)
	rate, _ := strconv.Atoi(info[1])
	delay, _ := strconv.Atoi(info[2])
	t.Logf("repeat-info from the %s: rate %d, delay %d ms", c.Kind().Name(), rate, delay)
	if rate <= 0 {
		t.Fatal("the test compositors enable repeat")
	}
	x := code(t, 'x')
	p.kb.Key(p.focused, x, true)
	pressed := p.panel.WaitFor("x pressed", e2e.Is("key 45 pressed"))
	p.wantQuery("x", "the press")
	first := p.panel.WaitFor("a repeat", e2e.Is("key 45 repeated"))
	if first.NS-pressed.NS < uint64(delay)*1e6 {
		t.Errorf("the first repeat came after %v, before the %d ms delay", time.Duration(first.NS-pressed.NS), delay)
	}
	for n := 2; n <= 3; n++ {
		p.wantQuery(strings.Repeat("x", n), "a repeat")
	}
	p.kb.Key(p.focused, x, false)
	p.wait("key 45 released")
	interval := 1000 / rate
	time.Sleep(time.Duration(delay+5*interval) * time.Millisecond)
	events := p.panel.Events()
	released := slices.IndexFunc(events, func(e trace.Event) bool { return e.What == "key 45 released" })
	repeats := count(events[:released], "key 45 repeated")
	if n := count(events[released:], "key 45 repeated"); n > 0 {
		t.Errorf("%d repeats after the release", n)
	}
	last := ""
	for _, e := range events {
		if q, ok := strings.CutPrefix(e.What, "query "); ok {
			last = q
		}
	}
	var q string
	_ = json.Unmarshal([]byte(last), &q)
	if want := strings.Repeat("x", repeats+1); q != want {
		t.Errorf("the query is %q after %d repeats, want one x per press and repeat", q, repeats)
	}
	p.tap(e2e.KeyEsc)
	p.endsWith("dismiss")
}

func TestE2EHomeXComposeIsHonouredWhenXComposeFileIsUnset(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	home := filepath.Join(c.RuntimeDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".XCompose"), []byte("<Multi_key> <l> <g> : \"Ψ\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := startTyping(t, c, func(cmd *exec.Cmd) {
		cmd.Env = slices.DeleteFunc(cmd.Env, func(kv string) bool {
			return strings.HasPrefix(kv, "XCOMPOSEFILE=") || strings.HasPrefix(kv, "HOME=")
		})
		cmd.Env = append(cmd.Env, "HOME="+home)
	})
	p.tap(e2e.KeyCompose)
	p.typeText("lg")
	p.wantQuery("Ψ", "~/.XCompose")
	p.tap(e2e.KeyEsc)
	p.endsWith("dismiss")
}
