package main

import (
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// The client side of a provider session, `logis --provider PROGRAM
// [ARG]...` (feat/logis §4.8; Rust src/client/provider.rs): the daemon
// runs the provider as if this client had, in its working directory and
// with its environment, and shows what the provider says. The client waits
// for the end: exit 0 when the provider closed its flow, 1 when the user
// dismissed it (or another session replaced it), 2 on error (the provider
// could not start, broke the protocol or departed early).

// runProvider runs a provider session for argv; configPath goes to a
// daemon this client has to start.
func runProvider(configPath string, argv []string, stderr io.Writer) int {
	conn, err := client.Connect(client.Options{
		RuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		DaemonArgs: config.NewSource(configPath).DaemonArgs(),
	})
	if err != nil {
		return fail(stderr, err.Error())
	}
	defer func() { _ = conn.Close() }()
	open := protocol.Open(protocol.ModeProvider, nil)
	launch := providerLaunch(argv)
	open.Provider = &launch
	if err := conn.Send(open); err != nil {
		return fail(stderr, err.Error())
	}
	outcome, err := conn.Await()
	switch {
	case err != nil:
		return fail(stderr, err.Error())
	case outcome.Kind == protocol.Dismissed, outcome.Kind == protocol.Replaced, outcome.Kind == protocol.Cancelled:
		return exitDismissed
	}
	return 0
}

// providerLaunch is the provider as this client would run it: its working
// directory ("/" when it has none) and environment, as bytes.
func providerLaunch(argv []string) protocol.Launch {
	l := protocol.Launch{Cwd: []byte("/")}
	if cwd, err := os.Getwd(); err == nil {
		l.Cwd = []byte(cwd)
	}
	for _, a := range argv {
		l.Argv = append(l.Argv, []byte(a))
	}
	for _, kv := range os.Environ() {
		l.Env = append(l.Env, []byte(kv))
	}
	return l
}
