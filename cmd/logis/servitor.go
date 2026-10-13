package main

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/servitor"
)

// servitor mode (feat/logis §4.8; Rust src/main.rs): `logis --servitor`
// is a provider session whose provider is logis itself, `logis provide
// servitor`, run by the daemon as this client would have run it.

// runServitor opens servitor mode; configPath goes to a daemon this client
// has to start.
func runServitor(configPath string, stderr io.Writer) int {
	self, err := os.Executable()
	if err != nil {
		return fail(stderr, "the cogitator cannot find its own body: "+err.Error())
	}
	return runProvider(configPath, []string{self, "provide", "servitor"}, stderr)
}

// runProvide is `logis provide NAME`: be a built-in provider on stdin and
// stdout.
func runProvide(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := parseProvide(args); err != nil {
		return fail(stderr, err.Error())
	}
	// A write to a logis that is gone fails with EPIPE (the provider then
	// ends quietly) instead of killing it; a handled signal is reset for
	// the programs it starts.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	return servitor.Run(servitor.Program, stdin, stdout, stderr)
}

// parseProvide takes exactly one built-in provider: servitor.
func parseProvide(args []string) error {
	const wants = "the name of a built-in provider: servitor"
	switch {
	case len(args) == 0:
		return emptyValue("provide", wants)
	case args[0] != "servitor":
		return badValue("provide", args[0], wants)
	case len(args) > 1:
		return unknownRune(args[1])
	}
	return nil
}
