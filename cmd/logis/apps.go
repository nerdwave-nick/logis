package main

import (
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/unixsock"
	"github.com/nerdwave-nick/logis/internal/window"
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
// to start. Without a daemon to answer, a session that cannot host the
// cogitator is named first (caller contract: exit 2, the reason on
// stderr), as the daemon this client would start would name it.
func runApps(configPath string, stderr io.Writer) int {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if !daemonAnswers(runtimeDir) {
		if err := window.Preflight(); err != nil {
			return fail(stderr, err.Error())
		}
	}
	conn, err := client.Connect(client.Options{
		RuntimeDir: runtimeDir,
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

// daemonAnswers says whether something listens at the daemon's socket.
func daemonAnswers(runtimeDir string) bool {
	path, err := protocol.SocketPath(runtimeDir)
	if err != nil {
		return false
	}
	c, err := unixsock.Dial(path)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
