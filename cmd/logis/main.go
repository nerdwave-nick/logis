// Command logis is a grimdark Wayland launcher and dmenu-compatible picker.
//
// This build knows --version, --check-config [--config PATH] and daemon; the
// other runes arrive with their tickets (feat/logis-go: dmenu, apps,
// servitor, CLI surface).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/version"

	// The cgo link line (spec §4) and the wayland-scanner glue, until the
	// Wayland, renderer and font bindings use them directly.
	_ "github.com/nerdwave-nick/logis/internal/native"
	_ "github.com/nerdwave-nick/logis/internal/wlproto"
)

// exitError is the exit status for every error (caller contract): the message
// goes to stderr.
const exitError = 2

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one invocation and returns its exit status.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "daemon" {
		return runDaemon(args[1:], stdout, stderr)
	}
	configPath, args, err := takeConfig(args)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if len(args) == 0 {
		return fail(stderr, "this cogitator has no app launcher yet. It knows only "+knownRunes+".")
	}
	for i, arg := range args {
		// As in Rust, a second command is as unknown as a foreign rune.
		if i > 0 || (arg != "--version" && arg != "-V" && arg != checkConfigRune) {
			return fail(stderr, fmt.Sprintf(
				"the rune '%s' is unknown to this cogitator. It knows only %s.", arg, knownRunes))
		}
	}
	if args[0] == checkConfigRune {
		return runCheckConfig(configPath, stdout, stderr)
	}
	if _, err := fmt.Fprintf(stdout, "logis %s\n", version.Version); err != nil {
		return fail(stderr, "cannot write to stdout: "+err.Error())
	}
	return 0
}

// fail reports message on stderr and returns the error status. A message
// stderr cannot take has nowhere else to go.
func fail(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintf(stderr, "logis: %s\n", message)
	return exitError
}
