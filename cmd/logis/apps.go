package main

import (
	"io"
	"os"
	"os/exec"

	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/servitor"
)

// The client side of the app launcher, plain `logis` or `logis --apps`
// (feat/logis stories 5–7): the daemon shows its app index and launches the
// chosen app; the client waits for the end. Its environment goes with it:
// the session environment for the launch when no systemd user manager
// answers. Exit 0 when an app was launched, 1 when the user dismissed the
// window (or another session replaced it), 2 on error. Plain `logis` is
// the tabbed launcher (feat/logis-go 8cba2fc9): the applications and
// servitor mode's rites, Tab switching; exit 0 also when a rite was invoked
// (its provider closed its flow). Without servitor on the PATH its rites
// are silent: the tab says so (2aaad4b9).

// appsRune asks for the app launcher alone, never tabbed.
const appsRune = "--apps"

// appsOpen is the app session's open, the client's environment with it.
// Tabbed (plain logis) with servitor on the client's PATH, it names
// servitor mode's provider, this body's `provide servitor`, as its rites:
// the daemon shows the tabbed launcher (feat/logis-go 8cba2fc9). Without
// servitor its rites are silent: the tabbed launcher still, its rites' tab
// saying why (2aaad4b9). --apps (or no known body of its own) is the
// launcher alone.
func appsOpen(tabbed bool, lookPath func(string) (string, error), self string) protocol.Request {
	open := protocol.Open(protocol.ModeApps, nil)
	for _, kv := range os.Environ() {
		open.Env = append(open.Env, []byte(kv))
	}
	if !tabbed {
		return open
	}
	if _, err := lookPath(servitor.Program); err != nil {
		open.RitesSilent = true
		return open
	}
	if self == "" {
		return open
	}
	rites := providerLaunch([]string{self, "provide", "servitor"})
	open.Rites = &rites
	return open
}

// runApps runs an app session; configPath goes to a daemon this client has
// to start. A session that cannot host the cogitator is named by that
// daemon, as for every other client (caller contract: exit 2, the reason
// on stderr).
func runApps(configPath string, tabbed bool, stderr io.Writer) int {
	conn, err := connect(configPath, stderr)
	if err != nil {
		return fail(stderr, err.Error())
	}
	defer func() { _ = conn.Close() }()
	var self string
	if tabbed {
		self, _ = os.Executable() // none: the launcher alone
	}
	open := appsOpen(tabbed, exec.LookPath, self)
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
