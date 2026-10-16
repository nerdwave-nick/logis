// Package protocol is the client <-> daemon session protocol (feat/logis
// §4.1): length-prefixed JSON frames (see WriteFrame) over the Unix stream
// socket $XDG_RUNTIME_DIR/logis.sock. It is internal to logis and may change;
// Version says when it does.
//
// One connection carries at most one session:
//
//  1. client → hello; daemon → welcome (or error and close).
//  2. client → open (its mode, the list's options, for a provider session the
//     Launch of its provider); daemon → queued and/or visible.
//  3. dmenu sessions: client → any number of items, then end.
//  4. daemon → result, then the daemon closes the connection.
//
// The client may send cancel at any time; disconnecting cancels too. A
// protocol violation ends the connection with error.
//
// Connections are not ordered among themselves. A session opened just after
// another client hung up may be queued until the daemon has handled that
// hang-up, and then becomes visible; a cancel answered with its result is
// handled by the time the result arrives.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// Version is bumped whenever the message set changes, so that a daemon left
// running across an upgrade refuses a client of another version plainly
// ("restart the daemon", exit 2) instead of ignoring what it does not know:
//
//   - 1–3: the Rust daemon's.
//   - 4: the Go rewrite's, with the shape of 3, versioned apart so that a
//     Rust daemon left running across the switch-over refuses a Go client.
//   - 5: open's rites (plain logis's tabbed launcher, feat/logis-go
//     8cba2fc9), which a daemon of 4 ignored, serving the launcher alone;
//     welcome's notice (a daemon whose binary was replaced, 750bed66);
//     open's rites_silent (plain logis without servitor, 2aaad4b9).
const Version uint32 = 5

// SocketName is the socket's file name inside $XDG_RUNTIME_DIR.
const SocketName = "logis.sock"

// SocketPath is the daemon socket for this runtime directory, which must be
// absolute.
func SocketPath(runtimeDir string) (string, error) {
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return "", errors.New("XDG_RUNTIME_DIR is not set or not absolute")
	}
	return filepath.Join(runtimeDir, SocketName), nil
}

// Mode is what a session shows; it decides the concurrency policy.
type Mode string

const (
	// ModeDmenu: lines from the client's stdin; the choice goes back to it.
	ModeDmenu Mode = "dmenu"
	// ModeApps: desktop applications, launched by the daemon.
	ModeApps Mode = "apps"
	// ModeServitor: servitor's rites and aspects.
	ModeServitor Mode = "servitor"
	// ModeProvider: an external provider process (`logis --provider`).
	ModeProvider Mode = "provider"
)

// Interactive sessions replace the visible one; dmenu sessions queue.
func (m Mode) Interactive() bool { return m != ModeDmenu }

// UnmarshalJSON refuses modes this protocol does not know.
func (m *Mode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch mode := Mode(s); mode {
	case ModeDmenu, ModeApps, ModeServitor, ModeProvider:
		*m = mode
		return nil
	default:
		return fmt.Errorf("unknown mode '%s'", s)
	}
}

// Item is one selectable row: its original bytes, kept apart from its
// display text.
type Item struct {
	// Text is shown, and matched unless Match says otherwise.
	Text string `json:"text"`
	// Match is matched instead of Text (`--match-nth`).
	Match *string `json:"match,omitempty"`
	// Raw is the exact input, returned unchanged when the item is chosen.
	Raw []byte `json:"raw"`
}

// Launch is how the daemon starts a provider: as if the client ran it, in
// the client's working directory with the client's environment. Bytes travel
// as base64, since paths, arguments and the environment need not be UTF-8.
type Launch struct {
	Argv [][]byte `json:"argv"`
	Cwd  []byte   `json:"cwd"`
	// Env is NAME=VALUE each.
	Env [][]byte `json:"env"`
}
