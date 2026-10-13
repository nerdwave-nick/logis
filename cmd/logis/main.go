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
	"github.com/nerdwave-nick/logis/internal/window"

	// The cgo link line (spec §4) for the libraries no binding calls yet.
	_ "github.com/nerdwave-nick/logis/internal/native"
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
		// Plain logis is the app launcher; a session that cannot host it
		// is named first (caller contract: exit 2, the reason on stderr).
		if err := window.Preflight(); err != nil {
			return fail(stderr, err.Error())
		}
		return fail(stderr, "this cogitator has no app launcher yet. It knows only --version.")
	}
	if isDebugPanel(args) {
		return debugPanel(stderr)
	}
	for i, arg := range args {
		// As in Rust, a second command is as unknown as a foreign rune.
		if i > 0 || (arg != "--version" && arg != "-V") {
			return fail(stderr, fmt.Sprintf(
				"the rune '%s' is unknown to this cogitator. It knows only --version.", arg))
		}
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
