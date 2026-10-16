package e2e

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Cogitator is a logis daemon in a test compositor, traced, with a virtual
// keyboard to work its window, and `logis --dmenu` / `--provider` /
// `--servitor` / `--apps` clients with piped stdout. The waits follow the
// daemon's session trace (`show <session>`, `items <n>`, `query <JSON
// string>` …), which the daemon emits once it shows sessions in its window
// (G11).
type Cogitator struct {
	t          testing.TB
	Compositor *Compositor
	Harness    *Harness
	Keyboard   *VirtualKeyboard
	Daemon     *Traced
	shownAt    uint64 // the trace time of the last `show` Shown waited for
}

// Still turns `"reduce-motion": true` on in config (a JSONC object),
// unless it names reduce-motion itself: the theme's continuous effects
// (glow pulse, travelling highlight, flicker) would keep a window drawing,
// so it would never settle and no two captures agree.
func Still(config string) string {
	if strings.Contains(config, `"reduce-motion"`) {
		return config
	}
	return strings.Replace(config, "{", `{ "reduce-motion": true,`, 1)
}

// StartCogitator starts the compositor LOGIS_E2E asks for (skipping
// without one) with opts and `logis daemon` in it with env added and
// config as its logis.jsonc ("" for {}; Still applies), and waits until it
// greets.
func StartCogitator(t testing.TB, env []string, config string, opts ...Option) *Cogitator {
	t.Helper()
	c := Start(t, Requested(t), opts...)
	if config == "" {
		config = "{}"
	}
	dir := filepath.Join(c.ConfigHome(), "logis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logis.jsonc"), []byte(Still(config)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Connect(t, c)
	kb := h.VirtualKeyboard()
	cmd := c.Logis("daemon")
	cmd.Env = append(cmd.Env, env...)
	daemon := Spawn(t, cmd)
	greet(t, filepath.Join(c.RuntimeDir(), protocol.SocketName))
	return &Cogitator{t: t, Compositor: c, Harness: h, Keyboard: kb, Daemon: daemon}
}

// greet waits until the daemon at socket welcomes a client: it binds its
// socket before the window has woken (GPU, fonts), but answers only once
// it has.
func greet(t testing.TB, socket string) {
	t.Helper()
	deadline := time.Now().Add(Patience)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(deadline)
		var reply protocol.Reply
		err = protocol.WriteFrame(conn, protocol.Hello())
		if err == nil {
			err = protocol.ReadFrame(conn, &reply)
		}
		_ = conn.Close()
		if err == nil {
			if reply.Type != protocol.TypeWelcome {
				t.Fatalf("the daemon answered %+v", reply)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon never greeted")
}

// Client is a logis client of the cogitator.
type Client struct {
	t              testing.TB
	cmd            *exec.Cmd
	Stdin          io.WriteCloser // nil once closed
	stdout, stderr bytes.Buffer
	exited         chan struct{}
}

func (c *Cogitator) client(stdin []byte, keepOpen bool, args ...string) *Client {
	c.t.Helper()
	return c.clientWith(nil, stdin, keepOpen, args...)
}

func (c *Cogitator) clientWith(env []string, stdin []byte, keepOpen bool, args ...string) *Client {
	c.t.Helper()
	cl := &Client{t: c.t, cmd: c.Compositor.Logis(args...), exited: make(chan struct{})}
	cl.cmd.Env = append(cl.cmd.Env, env...)
	cl.cmd.Stdout, cl.cmd.Stderr = &cl.stdout, &cl.stderr
	if stdin != nil {
		in, err := cl.cmd.StdinPipe()
		if err != nil {
			c.t.Fatal(err)
		}
		cl.Stdin = in
	}
	if err := cl.cmd.Start(); err != nil {
		c.t.Fatalf("logis %v does not start: %v", args, err)
	}
	go func() { _ = cl.cmd.Wait(); close(cl.exited) }()
	c.t.Cleanup(func() { _ = cl.cmd.Process.Kill(); <-cl.exited })
	if cl.Stdin != nil {
		if _, err := cl.Stdin.Write(stdin); err != nil {
			c.t.Fatal(err)
		}
		if !keepOpen {
			cl.CloseStdin()
		}
	}
	return cl
}

// Dmenu starts `logis --dmenu args…` with input on its stdin, left open.
func (c *Cogitator) Dmenu(input []byte, args ...string) *Client {
	return c.client(input, true, append([]string{"--dmenu"}, args...)...)
}

// DmenuAll is Dmenu with stdin closed after input.
func (c *Cogitator) DmenuAll(input []byte, args ...string) *Client {
	return c.client(input, false, append([]string{"--dmenu"}, args...)...)
}

// Provider starts `logis --provider argv…`.
func (c *Cogitator) Provider(argv ...string) *Client {
	return c.client(nil, false, append([]string{"--provider"}, argv...)...)
}

// ProviderWith starts `logis --provider argv…` with env added to the
// client's environment, which the provider runs with.
func (c *Cogitator) ProviderWith(env []string, argv ...string) *Client {
	return c.clientWith(env, nil, false, append([]string{"--provider"}, argv...)...)
}

// ServitorWith starts `logis --servitor` with env added to the client's
// environment, which servitor mode's provider runs with (PATH finds the
// fake servitor).
func (c *Cogitator) ServitorWith(env []string) *Client {
	return c.clientWith(env, nil, false, "--servitor")
}

// Apps starts `logis --apps`.
func (c *Cogitator) Apps() *Client { return c.client(nil, false, "--apps") }

// ClientWith starts `logis args…` (plain logis without args) with env
// added to the client's environment.
func (c *Cogitator) ClientWith(env []string, args ...string) *Client {
	return c.clientWith(env, nil, false, args...)
}

// CloseStdin closes the client's stdin (the end of a dmenu list).
func (cl *Client) CloseStdin() {
	if cl.Stdin != nil {
		_ = cl.Stdin.Close()
		cl.Stdin = nil
	}
}

// Kill ends the client at once, as a caller giving up does (its
// connection's end cancels its session), and waits until it is gone.
func (cl *Client) Kill() error {
	err := cl.cmd.Process.Kill()
	<-cl.exited
	return err
}

// Ended is how a client ended: its exit code and stdout.
type Ended struct {
	Code   int
	Stdout []byte
}

// Ended waits for the client to exit within Patience.
func (cl *Client) Ended() Ended {
	cl.t.Helper()
	cl.CloseStdin()
	select {
	case <-cl.exited:
	case <-time.After(Patience):
		cl.t.Fatal("the client never ended")
	}
	cl.t.Logf("client stderr: %s", cl.stderr.String())
	return Ended{Code: cl.cmd.ProcessState.ExitCode(), Stdout: cl.stdout.Bytes()}
}

// Shown waits until the next session is shown, focused, drawn and holding
// all items: focus, items and the first frame may come in any order after
// `show`; the sequence cursor ends after the focus.
func (c *Cogitator) Shown(items int) Focused {
	c.t.Helper()
	since := c.Daemon.WaitFor("show", Word("show")).NS
	focused := c.Daemon.WaitForNextFocus()
	want := "items " + strconv.Itoa(items)
	c.Daemon.WaitSeen(want, func(e trace.Event) bool { return e.NS >= since && e.What == want })
	c.Daemon.WaitSeen("a first frame", func(e trace.Event) bool { return e.NS >= since && e.What == "commit" })
	c.shownAt = since
	return focused
}

// OpenSettled waits until the window Shown last waited for has finished
// opening (`settled` after its `show`; the open moves even with
// reduce-motion), without moving the sequence cursor. Two equal captures
// are no proof of rest: on a slow machine the compositor can repaint twice
// between two frames of the open.
func (c *Cogitator) OpenSettled() {
	c.t.Helper()
	since := c.shownAt
	c.Daemon.WaitSeen("the open settles", func(e trace.Event) bool { return e.NS >= since && e.What == "settled" })
}

// Next is the next report of word (in sequence), without the word.
func (c *Cogitator) Next(word string) string {
	c.t.Helper()
	e := c.Daemon.WaitFor(word, Word(word))
	return strings.TrimPrefix(strings.TrimPrefix(e.What, word), " ")
}

// Tap taps key in the focused window.
func (c *Cogitator) Tap(f Focused, key uint32) { c.Keyboard.Tap(f, key) }

// Type types text and waits until the daemon reports it as the query.
func (c *Cogitator) Type(f Focused, text string) {
	c.t.Helper()
	c.Keyboard.Type(f, text)
	quoted := trace.Quote(text)
	c.Daemon.WaitFor("query "+quoted, func(e trace.Event) bool {
		return strings.HasPrefix(e.What, "query ") && strings.HasSuffix(e.What, quoted)
	})
}
