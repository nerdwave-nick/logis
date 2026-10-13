// Command logis is a grimdark Wayland launcher and dmenu-compatible picker.
//
// This build knows only --version; the other runes arrive with their tickets
// (feat/logis-go: daemon/client, dmenu, apps, servitor, CLI surface).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/version"
)

// exitError is the exit status for every error (caller contract): the message
// goes to stderr.
const exitError = 2

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one invocation and returns its exit status.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return fail(stderr, "this cogitator has no app launcher yet. It knows only --version.")
	}
	for i, arg := range args {
		// As in Rust, a second command is as unknown as a foreign rune.
		if i > 0 || (arg != "--version" && arg != "-V") {
			return fail(stderr, fmt.Sprintf(
				"the rune '%s' is unknown to this cogitator. It knows only --version.", arg))
		}
	}
	fmt.Fprintf(stdout, "logis %s\n", version.Version)
	return 0
}

func fail(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "logis: %s\n", message)
	return exitError
}
