// Package unixsock is Unix stream sockets as pollable *os.File values,
// without package net. Under cgo, net links glibc's libresolv for its DNS
// resolver, a library outside the set spec §4 allows in the logis binary;
// logis only ever needs local stream sockets. Connections support Read,
// Write, Close and deadlines through the runtime poller like any net.Conn.
package unixsock

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
)

// backlog is the listen queue length (the kernel caps it at somaxconn).
const backlog = 128

// Listener is a listening Unix stream socket.
type Listener struct {
	f      *os.File
	closed atomic.Bool
}

// Listen binds a new socket at path (which must not exist) and listens.
func Listen(path string) (*Listener, error) {
	fd, err := socket()
	if err != nil {
		return nil, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		return nil, &os.PathError{Op: "bind", Path: path, Err: err}
	}
	if err := syscall.Listen(fd, backlog); err != nil {
		_ = syscall.Close(fd)
		return nil, os.NewSyscallError("listen", err)
	}
	return &Listener{f: os.NewFile(uintptr(fd), path)}, nil
}

// FromFD serves an inherited listening socket (systemd's), taking ownership
// of fd. It keeps a close-on-exec duplicate, so launched programs do not
// inherit the socket, and closes fd.
func FromFD(fd int) (*Listener, error) {
	defer func() { _ = syscall.Close(fd) }()
	if kind, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE); err != nil ||
		kind != syscall.SOCK_STREAM {
		return nil, errors.New("not a stream socket")
	}
	if on, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ACCEPTCONN); err != nil || on == 0 {
		return nil, errors.New("not a listening socket")
	}
	dup, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, os.NewSyscallError("fcntl", errno)
	}
	if err := syscall.SetNonblock(int(dup), true); err != nil {
		_ = syscall.Close(int(dup))
		return nil, os.NewSyscallError("setnonblock", err)
	}
	return &Listener{f: os.NewFile(dup, fmt.Sprintf("fd %d", fd))}, nil
}

// Accept waits for the next connection. After Close it returns an error
// matching os.ErrClosed.
func (l *Listener) Accept() (*os.File, error) {
	var nfd int
	var accept error
	raw, err := l.f.SyscallConn()
	if err == nil {
		err = raw.Read(func(fd uintptr) bool {
			nfd, _, accept = syscall.Accept4(int(fd), syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK)
			return accept != syscall.EAGAIN
		})
	}
	if err != nil && l.closed.Load() {
		return nil, fmt.Errorf("accept: %w", os.ErrClosed)
	}
	if err != nil {
		return nil, err
	}
	if accept != nil {
		return nil, os.NewSyscallError("accept4", accept)
	}
	return os.NewFile(uintptr(nfd), "unix"), nil
}

// Close stops listening; the socket's path stays for the caller to remove.
func (l *Listener) Close() error {
	l.closed.Store(true)
	return l.f.Close()
}

// Dial connects to the socket at path. With nobody listening the error
// matches syscall.ENOENT (no socket) or syscall.ECONNREFUSED (a stale one).
func Dial(path string) (*os.File, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, os.NewSyscallError("socket", err)
	}
	// Blocking, a Unix connect waits only while the listen queue is full.
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		return nil, &os.PathError{Op: "connect", Path: path, Err: err}
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, os.NewSyscallError("setnonblock", err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

func socket() (int, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX,
		syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, os.NewSyscallError("socket", err)
	}
	return fd, nil
}
