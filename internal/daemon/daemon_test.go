package daemon

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// patience bounds every single wait in these tests.
const patience = 5 * time.Second

// rig is a scratch runtime directory (never the real one). Its path is short:
// a socket path must fit in 108 bytes.
type rig struct {
	t   *testing.T
	dir string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir, err := os.MkdirTemp("", "logis-d-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return &rig{t, dir}
}

func (r *rig) socket() string { return filepath.Join(r.dir, protocol.SocketName) }

// logBuffer is a log the daemon and the test share.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// running is a daemon served in-process.
type running struct {
	cancel context.CancelFunc
	done   chan int
	stdout *logBuffer
	stderr *logBuffer
	host   Host
	// hosted brings the host once the default resolver starts.
	hosted chan Host
}

// start runs a daemon with the Scripted resolver and waits until it listens.
func (r *rig) start(idle time.Duration) *running {
	r.t.Helper()
	d := r.run(Options{IdleTimeout: idle})
	waitUntil(r.t, "the daemon to listen", func() bool {
		return strings.Contains(d.stdout.String(), "listens at "+r.socket())
	})
	// The resolver started before the daemon said it listens: its host is
	// there by now, however slowly the daemon read its config and theme.
	if d.host == nil {
		select {
		case d.host = <-d.hosted:
		default:
		}
	}
	return d
}

func (r *rig) run(o Options) *running {
	ctx, cancel := context.WithCancel(context.Background())
	d := &running{cancel: cancel, done: make(chan int, 1), stdout: &logBuffer{}, stderr: &logBuffer{}}
	hosted := make(chan Host, 1)
	o.RuntimeDir, o.Stdout, o.Stderr = r.dir, d.stdout, d.stderr
	if o.Getenv == nil {
		// Never the owner's applications, frecency or session bus.
		o.Getenv = vars("XDG_DATA_HOME", filepath.Join(r.dir, "data"),
			"XDG_DATA_DIRS", filepath.Join(r.dir, "data-dirs"), "XDG_STATE_HOME", filepath.Join(r.dir, "state"))
	}
	if o.Resolver == nil {
		o.Resolver = func(h Host) (Resolver, error) {
			hosted <- h
			return NewScripted(h), nil
		}
	}
	go func() { d.done <- Run(ctx, o) }()
	r.t.Cleanup(func() {
		cancel()
		select {
		case <-d.done:
		case <-time.After(patience):
			r.t.Error("the daemon did not stop")
		}
	})
	d.hosted = hosted
	select {
	case d.host = <-hosted:
	case <-time.After(10 * time.Millisecond):
	}
	return d
}

// exit waits for the daemon's exit status.
func (d *running) exit(t *testing.T) int {
	t.Helper()
	select {
	case status := <-d.done:
		d.done <- status
		return status
	case <-time.After(patience):
		t.Fatal("the daemon did not exit")
		return -1
	}
}

func (d *running) exited() bool {
	select {
	case status := <-d.done:
		d.done <- status
		return true
	default:
		return false
	}
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// raw is a protocol client on a real socket.
type raw struct {
	t    *testing.T
	conn net.Conn
}

func (r *rig) connect() *raw {
	r.t.Helper()
	c, err := net.Dial("unix", r.socket())
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	return &raw{r.t, c}
}

func (c *raw) send(req protocol.Request) {
	c.t.Helper()
	if err := protocol.WriteFrame(c.conn, req); err != nil {
		c.t.Fatal(err)
	}
}

// recv is the next reply, or nil at end of stream.
func (c *raw) recv() *protocol.Reply {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(patience))
	var reply protocol.Reply
	if err := protocol.ReadFrame(c.conn, &reply); err != nil {
		if err == io.EOF {
			return nil
		}
		c.t.Fatalf("reading a reply: %v", err)
	}
	return &reply
}

func (c *raw) recvType() string {
	c.t.Helper()
	reply := c.recv()
	if reply == nil {
		c.t.Fatal("the daemon hung up")
	}
	return reply.Type
}

func (c *raw) greet() protocol.Reply {
	c.t.Helper()
	c.send(protocol.Hello())
	reply := c.recv()
	if reply == nil || reply.Type != protocol.TypeWelcome {
		c.t.Fatalf("greeting answered with %+v", reply)
	}
	return *reply
}

// outcome reads replies up to the result and the hang-up after it.
func (c *raw) outcome() protocol.Outcome {
	c.t.Helper()
	reply := c.recv()
	if reply == nil || reply.Type != protocol.TypeResult {
		c.t.Fatalf("got %+v, want a result", reply)
	}
	if after := c.recv(); after != nil {
		c.t.Fatalf("the daemon kept talking: %+v", after)
	}
	return *reply.Outcome
}

func (c *raw) itemsThenEnd(texts ...string) {
	c.t.Helper()
	c.send(protocol.Items(plain(texts...)))
	c.send(protocol.End())
}

// open opens a session of mode; it returns it and the daemon's first answer.
func (r *rig) open(mode protocol.Mode) (*raw, string) {
	r.t.Helper()
	c := r.connect()
	c.greet()
	c.send(protocol.Open(mode, nil))
	return c, c.recvType()
}

// probe opens a dmenu session only to see whether it would be shown at once
// or wait, then cancels it and waits for the cancellation.
func (r *rig) probe() string {
	r.t.Helper()
	c, state := r.open(protocol.ModeDmenu)
	c.send(protocol.Cancel())
	reply := c.recv()
	if state == protocol.TypeQueued && reply != nil && reply.Type == protocol.TypeVisible {
		reply = c.recv()
	}
	if reply == nil || reply.Outcome == nil || reply.Outcome.Kind != protocol.Cancelled {
		r.t.Fatalf("probe ended with %+v", reply)
	}
	return state
}

func (r *rig) waitUntilNoSessionRemains() {
	r.t.Helper()
	waitUntil(r.t, "every session to end", func() bool { return r.probe() == protocol.TypeVisible })
}
