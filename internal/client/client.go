// Package client is the client role of every user-facing invocation
// (feat/logis §4.1): it connects to $XDG_RUNTIME_DIR/logis.sock, starting
// the daemon itself when none answers, and greets it before a session opens.
// A client whose daemon cannot start reports the daemon's reason (the
// caller then exits 2).
package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"syscall"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/unixsock"
)

// startPatience is how long a client waits for a daemon to start and greet
// it. The first-open budget is a target for the whole path, not this limit,
// which only bounds a daemon that hangs at start.
const startPatience = 5 * time.Second

// maxHangUps is how often a daemon may hang up before greeting before the
// client gives up (a socket unit whose daemon keeps failing).
const maxHangUps = 3

// Options are what connecting needs.
type Options struct {
	// RuntimeDir is $XDG_RUNTIME_DIR; it must be absolute. A daemon this
	// client starts gets it too.
	RuntimeDir string
	// Exe is the logis binary that starts the daemon; "" is this one.
	Exe string
	// DaemonArgs follow `daemon --detach` for a daemon this client starts
	// (`--config PATH`, made absolute, once there is a config).
	DaemonArgs []string
	// Patience bounds starting and greeting a daemon; 0 is 5 s.
	Patience time.Duration
}

// Conn is a greeted connection to the daemon. One goroutine may Send while
// another receives.
type Conn struct {
	c stream
	r *bufio.Reader
	// Daemon is the daemon's process ID, from its welcome.
	Daemon int
}

// stream is a connection: a socket from unixsock, or a pipe in tests.
type stream interface {
	io.ReadWriteCloser
	SetReadDeadline(t time.Time) error
}

func newConn(c stream) *Conn { return &Conn{c: c, r: bufio.NewReader(c)} }

// Connect connects to the daemon, starting it if none answers, and greets
// it.
func Connect(o Options) (*Conn, error) {
	path, err := protocol.SocketPath(o.RuntimeDir)
	if err != nil {
		return nil, fmt.Errorf("the machine spirit has no dwelling: %w", err)
	}
	patience := o.Patience
	if patience == 0 {
		patience = startPatience
	}
	deadline := time.Now().Add(patience)
	started, hangUps := false, 0
	for {
		c, err := unixsock.Dial(path)
		switch {
		case err == nil:
			conn, err := greet(newConn(c), deadline)
			if conn != nil || err != nil {
				return conn, err
			}
			// The daemon left as we arrived (idle timeout): try again, but
			// not for ever.
			if hangUps == maxHangUps {
				return nil, fmt.Errorf("the machine spirit at %s hangs up before greeting; "+
					"if systemd starts it, see journalctl --user -u logis", path)
			}
			hangUps++
		case errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED):
			if !started {
				if err := startDaemon(o, deadline); err != nil {
					return nil, err
				}
				started = true
				continue
			}
			// Another daemon won the race to start and is not listening yet.
		default:
			return nil, fmt.Errorf("cannot reach the machine spirit at %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the machine spirit at %s did not answer within %g s",
				path, patience.Seconds())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// greet says hello. Nil (and no error) if the daemon hung up, or broke off,
// before welcoming us, as one that is just exiting does.
func greet(conn *Conn, deadline time.Time) (*Conn, error) {
	if conn.Send(protocol.Hello()) != nil {
		_ = conn.Close()
		return nil, nil
	}
	_ = conn.c.SetReadDeadline(later(deadline))
	reply, err := conn.Recv()
	switch {
	case err != nil || reply == nil:
		_ = conn.Close()
		return nil, nil
	case reply.Type == protocol.TypeError:
		_ = conn.Close()
		return nil, errors.New(reply.Message)
	case reply.Type != protocol.TypeWelcome:
		_ = conn.Close()
		return nil, outOfTurn(reply)
	}
	_ = conn.c.SetReadDeadline(time.Time{})
	conn.Daemon = reply.PID
	return conn, nil
}

// later is deadline, but at least a moment from now.
func later(deadline time.Time) time.Time {
	if soon := time.Now().Add(time.Millisecond); deadline.Before(soon) {
		return soon
	}
	return deadline
}

// Send sends one request.
func (c *Conn) Send(request protocol.Request) error {
	if err := protocol.WriteFrame(c.c, request); err != nil {
		return voiceBroke(err)
	}
	return nil
}

// Recv is the next reply; nil (and no error) when the daemon hung up.
func (c *Conn) Recv() (*protocol.Reply, error) {
	var reply protocol.Reply
	switch err := protocol.ReadFrame(c.r, &reply); {
	case err == io.EOF:
		return nil, nil
	case err != nil:
		return nil, voiceBroke(err)
	}
	return &reply, nil
}

// Await waits, after open, through queued and visible for the session's
// outcome. The daemon's error, or its silence, is an error.
func (c *Conn) Await() (protocol.Outcome, error) {
	for {
		reply, err := c.Recv()
		switch {
		case err != nil:
			return protocol.Outcome{}, err
		case reply == nil:
			return protocol.Outcome{}, errors.New("the machine spirit fell silent before answering")
		case reply.Type == protocol.TypeQueued || reply.Type == protocol.TypeVisible:
		case reply.Type == protocol.TypeResult && reply.Outcome != nil:
			return *reply.Outcome, nil
		case reply.Type == protocol.TypeError:
			return protocol.Outcome{}, errors.New(reply.Message)
		default:
			return protocol.Outcome{}, outOfTurn(reply)
		}
	}
}

// Close hangs up; a session still open is cancelled.
func (c *Conn) Close() error { return c.c.Close() }

func voiceBroke(err error) error {
	return fmt.Errorf("the machine spirit's voice broke: %w", err)
}

func outOfTurn(reply *protocol.Reply) error {
	return fmt.Errorf("the machine spirit spoke out of turn: '%s'", reply.Type)
}
