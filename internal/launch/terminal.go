package launch

import "slices"

// Which terminal runs a Terminal=true application's command. A launcher
// started by the compositor or by systemd's socket usually has no
// $TERMINAL, so logis looks for an installed terminal itself:
//
//  1. the config's "terminal", as written;
//  2. the caller's $TERMINAL, with -e (xterm's convention);
//  3. xdg-terminal-exec on the caller's PATH, which takes the command
//     directly and runs it in the terminal the user chose for it;
//  4. the first of Terminals on the caller's PATH.
//
// The caller is the client that asked for the launch: its PATH, never the
// daemon's own, frozen at its start. A terminal found on the PATH is named
// by its absolute path, so that it is the one started when the session
// environment's PATH differs.

// A Terminal is a terminal emulator logis knows: its program and the
// arguments after which it takes the command to run, with the command's
// own arguments, as it documents them.
type Terminal struct {
	Program string
	Args    []string
}

// Terminals are the terminals FindTerminal looks for, in order: first the
// Wayland-native terminals people install for a Wayland compositor of their
// own choosing (foot first, the lightest, sway's default terminal; niri's
// default config starts alacritty); then the terminals of the big desktops,
// which tend to come along with their desktop rather than be chosen; xterm
// last, X11 through Xwayland, the terminal of last resort.
var Terminals = []Terminal{
	{"foot", nil},                        // foot(1): foot [OPTIONS] <command> [COMMAND OPTIONS]
	{"kitty", nil},                       // kitty(1): kitty [options] [program-to-run ...]
	{"alacritty", []string{"-e"}},        // alacritty --help: -e, --command <COMMAND>... (must be last)
	{"wezterm", []string{"start", "--"}}, // wezterm start --help: wezterm start -- bash -l
	{"ghostty", []string{"-e"}},          // ghostty docs (command): the common -e flag
	{"konsole", []string{"-e"}},          // konsole --help: -e catches all following arguments
	{"gnome-terminal", []string{"--"}},   // gnome-terminal(1): [-- PROGRAM [ARG...]]; -e is deprecated
	{"xfce4-terminal", []string{"-x"}},   // xfce4-terminal(1): -x executes the remainder of the line
	{"xterm", []string{"-e"}},            // xterm(1): -e program [arguments ...], the last option
}

// execTerminal is xdg-terminal-exec, which takes the command directly.
const execTerminal = "xdg-terminal-exec"

// FindTerminal is the program and leading arguments that run a command in
// a terminal, for a caller whose environment is env; false if there is
// none. configured is the config's terminal (nil: none). Each call
// searches the caller's PATH afresh: nothing is kept between launches.
func FindTerminal(configured, env []string) ([]string, bool) {
	if configured != nil {
		return configured, true
	}
	if term := lookup(env, "TERMINAL"); term != "" {
		return []string{term, "-e"}, true
	}
	path := lookup(env, "PATH")
	if found, ok := LookPath(path, execTerminal); ok {
		return []string{found}, true
	}
	for _, t := range Terminals {
		if found, ok := LookPath(path, t.Program); ok {
			return append([]string{found}, slices.Clone(t.Args)...), true
		}
	}
	return nil, false
}
