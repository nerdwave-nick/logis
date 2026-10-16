package dbus

// A connection to a bus on its Unix socket: the address
// (DBUS_SESSION_BUS_ADDRESS, else $XDG_RUNTIME_DIR/bus), EXTERNAL
// authentication by uid, Hello, and blocking method calls with a timeout.
// Signals that arrive meanwhile are skipped.

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/unixsock"
)

// Timeout is how long one call may take before the bus counts as
// unresponsive.
const Timeout = 2 * time.Second

// ErrNoAddress says no address names a bus logis can reach.
var ErrNoAddress = errors.New("no session bus address")

// An UnreachableError says the socket could not be reached, read or written.
type UnreachableError struct{ Err error }

func (e *UnreachableError) Error() string { return "the session bus is unreachable: " + e.Err.Error() }
func (e *UnreachableError) Unwrap() error { return e.Err }

// An AuthError says the bus refused the authentication.
type AuthError struct{ Line string }

func (e *AuthError) Error() string { return "the session bus refused us: " + e.Line }

// A RemoteError is a call that failed on the other side.
type RemoteError struct{ Name, Message string }

func (e *RemoteError) Error() string { return e.Name + ": " + e.Message }

// A Conn is a connection to a bus. It belongs to one goroutine at a time.
type Conn struct {
	conn   stream
	r      *bufio.Reader
	serial uint32
}

// stream is a connected socket with deadlines (an *os.File from unixsock:
// package net would link glibc's resolver into logis).
type stream interface {
	io.ReadWriteCloser
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// Connect connects to the first reachable socket of address, authenticates
// and says Hello.
func Connect(address string) (*Conn, error) {
	last := ErrNoAddress
	for _, socket := range Sockets(address) {
		conn, err := unixsock.Dial(socket.dialName())
		if err != nil {
			last = &UnreachableError{err}
			continue
		}
		c := &Conn{conn: conn, r: bufio.NewReader(conn)}
		if err := c.greet(); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return c, nil
	}
	return nil, last
}

// greet authenticates by uid (the bus checks it against the socket's peer
// credentials) and says Hello.
func (c *Conn) greet() error {
	if err := c.conn.SetDeadline(time.Now().Add(Timeout)); err != nil {
		return &UnreachableError{err}
	}
	uid := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := io.WriteString(c.conn, "\x00AUTH EXTERNAL "+uid+"\r\n"); err != nil {
		return &UnreachableError{err}
	}
	line, err := c.line()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "OK ") {
		return &AuthError{line}
	}
	if _, err := io.WriteString(c.conn, "BEGIN\r\n"); err != nil {
		return &UnreachableError{err}
	}
	_, err = c.Call(Call{
		Destination: "org.freedesktop.DBus",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Member:      "Hello",
	})
	return err
}

// line is one line of the authentication exchange, without its CRLF.
func (c *Conn) line() (string, error) {
	var line []byte
	for !bytes.HasSuffix(line, []byte("\r\n")) {
		if len(line) > 512 {
			return "", &AuthError{"an endless line"}
		}
		b, err := c.r.ReadByte()
		if err != nil {
			return "", &UnreachableError{err}
		}
		line = append(line, b)
	}
	return strings.ToValidUTF8(string(line[:len(line)-2]), "\uFFFD"), nil
}

// Close ends the connection.
func (c *Conn) Close() error { return c.conn.Close() }

// Send sends a call without waiting; its serial.
func (c *Conn) Send(call Call) (uint32, error) {
	c.serial++
	if err := c.conn.SetWriteDeadline(time.Now().Add(Timeout)); err != nil {
		return 0, &UnreachableError{err}
	}
	if _, err := c.conn.Write(EncodeCall(call, c.serial)); err != nil {
		return 0, &UnreachableError{err}
	}
	return c.serial, nil
}

// Call calls a method and waits (at most Timeout) for its reply's body.
func (c *Conn) Call(call Call) ([]Value, error) {
	serial, err := c.Send(call)
	if err != nil {
		return nil, err
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(Timeout)); err != nil {
		return nil, &UnreachableError{err}
	}
	for {
		m, err := c.receive()
		if err != nil {
			return nil, err
		}
		if m.ReplySerial != serial || m.Kind == Signal || m.Kind == MethodCall {
			continue
		}
		if m.Kind == MethodReturn {
			return m.Body, nil
		}
		text := ""
		if len(m.Body) > 0 {
			text, _ = AsString(m.Body[0])
		}
		return nil, &RemoteError{m.ErrorName, text}
	}
}

func (c *Conn) receive() (Message, error) {
	var head [16]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return Message{}, &UnreachableError{err}
	}
	n, err := TotalLen(head)
	if err != nil {
		return Message{}, err
	}
	b := make([]byte, n)
	copy(b, head[:])
	if _, err := io.ReadFull(c.r, b[16:]); err != nil {
		return Message{}, &UnreachableError{err}
	}
	return Decode(b)
}
