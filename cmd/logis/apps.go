package main

import (
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// The client side of the app launcher, plain `logis` or `logis --apps`
// (feat/logis stories 5–7; Rust src/client/apps.rs): the daemon shows its
// app index and launches the chosen app; the client waits for the end. Its
// environment goes with it: the session environment for the launch when no
// systemd user manager answers. Exit 0 when an app was launched, 1 when
// the user dismissed the window (or another session replaced it), 2 on
// error.

// appsRune asks for the app launcher, as no command at all does.
const appsRune = "--apps"

// runApps runs an app session; configPath goes to a daemon this client has
// to start. A session that cannot host the cogitator is named by that
// daemon, as for every other client (caller contract: exit 2, the reason
// on stderr).
func runApps(configPath string, stderr io.Writer) int {
	conn, err := client.Connect(client.Options{
		RuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		DaemonArgs: config.NewSource(configPath).DaemonArgs(),
	})
	if err != nil {
		return fail(stderr, err.Error())
	}
	defer func() { _ = conn.Close() }()
	open := protocol.Open(protocol.ModeApps, nil)
	for _, kv := range os.Environ() {
		open.Env = append(open.Env, []byte(kv))
	}
	if err := conn.Send(open); err != nil {
		return fail(stderr, err.Error())
	}
	outcome, err := conn.Await()
	switch {
	case err != nil:
		return fail(stderr, err.Error())
	case outcome.Kind != protocol.Closed:
		return exitDismissed
	}
	return 0
}
