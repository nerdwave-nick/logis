package unixsock

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func scratch(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "logis-u-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// Bytes written on one end arrive on the other, both ways, and closing one
// end is the other's end of stream.
func TestAConnectionCarriesBytesBothWaysAndEndsCleanly(t *testing.T) {
	path := scratch(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	accepted := make(chan *os.File, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			t.Error(err)
		}
		accepted <- c
	}()
	client, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	if _, err := client.Write([]byte("ave")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(server, buf); err != nil || string(buf) != "ave" {
		t.Fatalf("%q, %v", buf, err)
	}
	if _, err := server.Write([]byte("imperator")); err != nil {
		t.Fatal(err)
	}
	_ = server.Close()
	got, err := io.ReadAll(client)
	if err != nil || string(got) != "imperator" {
		t.Errorf("%q, %v", got, err)
	}
	_ = client.Close()
}

// Closing the listener ends a waiting Accept with os.ErrClosed.
func TestClosingTheListenerEndsAcceptWithErrClosed(t *testing.T) {
	l, err := Listen(scratch(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := l.Accept()
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = l.Close()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("%v, want os.ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept still waits")
	}
}

// Nobody listening is ENOENT (no socket) or ECONNREFUSED (a stale one).
func TestDialingNobodyNamesWhy(t *testing.T) {
	path := scratch(t)
	if _, err := Dial(path); !errors.Is(err, syscall.ENOENT) {
		t.Errorf("no socket: %v", err)
	}
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close() // the file stays: stale
	if _, err := Dial(path); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("stale socket: %v", err)
	}
}

// A read deadline ends a read that waits in vain.
func TestAReadDeadlineEndsAWaitingRead(t *testing.T) {
	path := scratch(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("%v, want a deadline error", err)
	}
}

// An inherited listening socket (systemd's) is served like an own one;
// anything else is refused.
func TestAnInheritedListeningSocketIsServed(t *testing.T) {
	path := scratch(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := l.f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var dup int
	_ = raw.Control(func(fd uintptr) { dup, err = syscall.Dup(int(fd)) })
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	inherited, err := FromFD(dup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inherited.Close() }()
	go func() {
		if c, err := inherited.Accept(); err == nil {
			_, _ = c.Write([]byte("x"))
			_ = c.Close()
		}
	}()
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(c); string(got) != "x" {
		t.Errorf("%q", got)
	}

	file, err := os.CreateTemp("", "logis-u-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	fd, _ := syscall.Dup(int(file.Fd()))
	if _, err := FromFD(fd); err == nil {
		t.Error("a regular file was accepted as a socket")
	}
}

// A path already taken is not bound over.
func TestListeningOnATakenPathIsRefused(t *testing.T) {
	path := scratch(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if _, err := Listen(path); !errors.Is(err, syscall.EADDRINUSE) {
		t.Errorf("%v, want EADDRINUSE", err)
	}
}

// An inherited socket that does not listen is refused, and Accept after
// Close says closed.
func TestAConnectedSocketIsNoListenerAndAClosedListenerAcceptsNothing(t *testing.T) {
	path := scratch(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	fd, err := syscall.Dup(int(c.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromFD(fd); err == nil || !strings.Contains(err.Error(), "not a listening socket") {
		t.Errorf("%v", err)
	}
	_ = l.Close()
	if _, err := l.Accept(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("%v, want os.ErrClosed", err)
	}
}
