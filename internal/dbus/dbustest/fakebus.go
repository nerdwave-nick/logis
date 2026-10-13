// Package dbustest is a fake D-Bus session bus for tests (a port of the
// Rust reference's tests/support/fake_bus.rs): a Unix socket in a scratch
// directory that takes EXTERNAL authentication, answers Hello (then
// announces NameAcquired, as a real bus does), records every other call and
// answers it as the test says. It never reaches a real bus or systemd.
package dbustest

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/dbus"
)

// An Answer is how the fake bus answers a call.
type Answer struct {
	silent bool
	body   []dbus.Value
	name   string
}

// Return answers with a method return of body.
func Return(body ...dbus.Value) Answer { return Answer{body: body} }

// Fail answers with an error of name, with message.
func Fail(name, message string) Answer {
	return Answer{name: name, body: []dbus.Value{dbus.Str(message)}}
}

// Silent answers nothing (the call expects no reply, or the bus is slow).
func Silent() Answer { return Answer{silent: true} }

// A Handler answers a call.
type Handler func(call dbus.Message) Answer

// A Bus is a running fake bus; the test's cleanup stops it and removes its
// socket.
type Bus struct {
	// Address is the bus address, as DBUS_SESSION_BUS_ADDRESS would hold it.
	Address string

	handler  Handler
	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	calls    []dbus.Message
	conns    []net.Conn
	closed   bool
}

// Start starts a bus answering calls with handler.
func Start(t testing.TB, handler Handler) *Bus {
	t.Helper()
	// Not t.TempDir: a test's name could push the path past sun_path.
	dir, err := os.MkdirTemp("", "logis-fakebus-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "bus")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	b := &Bus{Address: "unix:path=" + socket, handler: handler, listener: listener}
	b.wg.Add(1)
	go b.accept()
	t.Cleanup(func() {
		b.stop()
		_ = os.RemoveAll(dir)
	})
	return b
}

// Calls are the calls so far (not Hello), oldest first.
func (b *Bus) Calls() []dbus.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]dbus.Message(nil), b.calls...)
}

// WaitFor waits until a call to member arrived, and returns it.
func (b *Bus) WaitFor(t testing.TB, member string) dbus.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, c := range b.Calls() {
			if c.Member == member {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s call reached the bus", member)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (b *Bus) stop() {
	b.mu.Lock()
	b.closed = true
	_ = b.listener.Close()
	for _, c := range b.conns {
		_ = c.Close()
	}
	b.mu.Unlock()
	b.wg.Wait()
}

func (b *Bus) accept() {
	defer b.wg.Done()
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			_ = conn.Close()
			return
		}
		b.conns = append(b.conns, conn)
		b.wg.Add(1)
		b.mu.Unlock()
		go b.serve(conn)
	}
}

func (b *Bus) serve(conn net.Conn) {
	defer b.wg.Done()
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	if nul, err := r.ReadByte(); err != nil || nul != 0 {
		return
	}
	if !authenticate(r, conn) {
		return
	}
	serial := uint32(1000)
	for {
		call, err := read(r)
		if err != nil {
			return
		}
		serial++
		answer := Return(dbus.Str(":1.42"))
		if call.Member != "Hello" {
			b.mu.Lock()
			b.calls = append(b.calls, call)
			b.mu.Unlock()
			answer = b.handler(call)
		}
		if answer.silent {
			continue
		}
		reply := dbus.EncodeReply(serial, call.Serial, answer.name, answer.body)
		if _, err := conn.Write(reply); err != nil {
			return
		}
		if call.Member == "Hello" {
			serial++
			if _, err := conn.Write(nameAcquired(serial)); err != nil {
				return
			}
		}
	}
}

// authenticate takes the authentication exchange up to BEGIN.
func authenticate(r *bufio.Reader, w io.Writer) bool {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return false
		}
		reply := "ERROR\r\n"
		switch line = strings.TrimRight(line, "\r\n"); {
		case strings.HasPrefix(line, "AUTH EXTERNAL "):
			reply = "OK 0123456789abcdef0123456789abcdef\r\n"
		case line == "BEGIN":
			return true
		}
		if _, err := io.WriteString(w, reply); err != nil {
			return false
		}
	}
}

func read(r io.Reader) (dbus.Message, error) {
	var head [16]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return dbus.Message{}, err
	}
	n, err := dbus.TotalLen(head)
	if err != nil {
		return dbus.Message{}, err
	}
	b := make([]byte, n)
	copy(b, head[:])
	if _, err := io.ReadFull(r, b[16:]); err != nil {
		return dbus.Message{}, err
	}
	return dbus.Decode(b)
}

// nameAcquired is the NameAcquired signal a bus sends after Hello: a client
// must skip it.
func nameAcquired(serial uint32) []byte {
	b := dbus.EncodeCall(dbus.Call{
		Destination: ":1.42",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Member:      "NameAcquired",
		Body:        []dbus.Value{dbus.Str(":1.42")},
	}, serial)
	b[1] = dbus.Signal
	return b
}
