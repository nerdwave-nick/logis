package main

import (
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/config"
)

// connect reaches the daemon (starting it with configPath's daemon
// arguments when none answers) for a session. Its notice, a daemon whose
// binary was replaced since it started asking to be restarted, goes to
// stderr; the session goes on.
func connect(configPath string, stderr io.Writer) (*client.Conn, error) {
	conn, err := client.Connect(client.Options{
		RuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		DaemonArgs: config.NewSource(configPath).DaemonArgs(),
	})
	if err == nil && conn.Notice != "" {
		_, _ = io.WriteString(stderr, "logis: "+conn.Notice+"\n")
	}
	return conn, err
}
