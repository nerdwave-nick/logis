package launch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// stubs makes a directory holding an executable stub for each program.
func stubs(t *testing.T, programs ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range programs {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTheConfiguredTerminalComesFirst(t *testing.T) {
	dir := stubs(t, "xdg-terminal-exec", "foot")
	got, ok := FindTerminal([]string{"forge-glass", "--"}, []string{"TERMINAL=forge-term", "PATH=" + dir})
	if !ok || !slices.Equal(got, []string{"forge-glass", "--"}) {
		t.Errorf("FindTerminal = %q, %v", got, ok)
	}
}

func TestTheCallersTERMINALComesNextWithDashE(t *testing.T) {
	dir := stubs(t, "xdg-terminal-exec", "foot")
	got, ok := FindTerminal(nil, []string{"PATH=" + dir, "TERMINAL=forge-term"})
	if !ok || !slices.Equal(got, []string{"forge-term", "-e"}) {
		t.Errorf("FindTerminal = %q, %v", got, ok)
	}
}

func TestXdgTerminalExecOnTheCallersPathComesNextAndTakesTheCommandDirectly(t *testing.T) {
	dir := stubs(t, "xdg-terminal-exec", "foot")
	got, ok := FindTerminal(nil, []string{"TERMINAL=", "PATH=" + dir})
	if !ok || !slices.Equal(got, []string{filepath.Join(dir, "xdg-terminal-exec")}) {
		t.Errorf("FindTerminal = %q, %v", got, ok)
	}
}

// Each known terminal alone on the caller's PATH, with the way it takes a
// command (verified against its --help or manual).
func TestEachKnownTerminalOnTheCallersPathRunsTheCommandItsOwnWay(t *testing.T) {
	want := map[string][]string{
		"foot":           nil,
		"kitty":          nil,
		"alacritty":      {"-e"},
		"wezterm":        {"start", "--"},
		"ghostty":        {"-e"},
		"konsole":        {"-e"},
		"gnome-terminal": {"--"},
		"xfce4-terminal": {"-x"},
		"xterm":          {"-e"},
	}
	if len(want) != len(Terminals) {
		t.Fatalf("%d known terminals, %d expected", len(Terminals), len(want))
	}
	for program, args := range want {
		dir := stubs(t, program)
		got, ok := FindTerminal(nil, []string{"PATH=" + dir})
		if !ok || !slices.Equal(got, append([]string{filepath.Join(dir, program)}, args...)) {
			t.Errorf("%s: FindTerminal = %q, %v", program, got, ok)
		}
	}
}

// The list's order decides, not the PATH's: foot before kitty before
// xterm, wherever each is.
func TestTheFirstKnownTerminalOfTheListIsTaken(t *testing.T) {
	xterm, kitty, foot := stubs(t, "xterm"), stubs(t, "kitty"), stubs(t, "foot")
	got, _ := FindTerminal(nil, []string{"PATH=" + xterm + ":" + kitty})
	if !slices.Equal(got, []string{filepath.Join(kitty, "kitty")}) {
		t.Errorf("FindTerminal = %q", got)
	}
	got, _ = FindTerminal(nil, []string{"PATH=" + xterm + ":" + kitty + ":" + foot})
	if !slices.Equal(got, []string{filepath.Join(foot, "foot")}) {
		t.Errorf("FindTerminal = %q", got)
	}
	order := make([]string, len(Terminals))
	for i, k := range Terminals {
		order[i] = k.Program
	}
	if !slices.Equal(order, []string{"foot", "kitty", "alacritty", "wezterm", "ghostty",
		"konsole", "gnome-terminal", "xfce4-terminal", "xterm"}) {
		t.Errorf("order %q", order)
	}
}

// Only the caller's PATH is searched: not the daemon's own (this test's,
// which may well hold a terminal), and not where a stub is not executable.
func TestNoTerminalIsFoundOffTheCallersPath(t *testing.T) {
	inert := t.TempDir()
	if err := os.WriteFile(filepath.Join(inert, "foot"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, env := range [][]string{nil, {"PATH="}, {"PATH=" + t.TempDir()}, {"PATH=" + inert}, {"PATH=relative"}} {
		if got, ok := FindTerminal(nil, env); ok {
			t.Errorf("%q: FindTerminal = %q", env, got)
		}
	}
}
