package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// lockName is the lock file beside the socket; whoever holds it owns the
// socket path.
const lockName = "logis.lock"

// contended is how long to wait for a daemon that holds the lock but does
// not answer yet (it is starting or shutting down).
const contended = 2 * time.Second

// listenFDsStart is systemd's first passed descriptor (SD_LISTEN_FDS_START).
const listenFDsStart = 3

// listening is the claimed socket.
type listening struct {
	ln   *net.UnixListener
	path string
	// ownsPath: the daemon bound the path itself; systemd's socket stays.
	ownsPath bool
	lock     *os.File
}

// release stops listening and gives up the path, then the lock.
func (l *listening) release() {
	_ = l.ln.Close()
	if l.ownsPath {
		_ = os.Remove(l.path)
	}
	if l.lock != nil {
		_ = l.lock.Close()
	}
}

// claim takes the socket from systemd socket activation, or claims
// <runtimeDir>/logis.sock. It returns nil (and no error) when another daemon
// already answers there.
func claim(runtimeDir string) (*listening, error) {
	path := filepath.Join(runtimeDir, protocol.SocketName)
	ln, err := activated()
	if err != nil {
		return nil, err
	}
	if ln != nil {
		lock, _ := lockSocket(runtimeDir)
		return &listening{ln: ln, path: path, lock: lock}, nil
	}
	deadline := time.Now().Add(contended)
	for {
		lock, err := lockSocket(runtimeDir)
		if err != nil {
			return nil, err
		}
		if lock != nil {
			l, err := bind(path)
			if err != nil {
				_ = lock.Close()
				return nil, err
			}
			return &listening{ln: l, path: path, ownsPath: true, lock: lock}, nil
		}
		if c, err := net.Dial("unix", path); err == nil {
			_ = c.Close()
			return nil, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another machine spirit holds %s but does not answer",
				filepath.Join(runtimeDir, lockName))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// bind listens at path; holding the lock, any socket file there is stale.
func bind(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	// release removes the path itself, while it still holds the lock.
	ln.SetUnlinkOnClose(false)
	// As logis.socket's SocketMode; the runtime directory is private anyway.
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

// lockSocket takes the exclusive lock on <runtimeDir>/logis.lock, or returns
// nil if another process holds it. The descriptor is close-on-exec, so
// launched programs never inherit the lock.
func lockSocket(runtimeDir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(runtimeDir, lockName), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		_ = f.Close()
		return nil, nil
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// activated is the socket systemd passed (logis.socket), if any: with
// LISTEN_PID naming this process, its first socket is descriptor 3.
func activated() (*net.UnixListener, error) {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) {
		return nil, nil
	}
	if n, err := strconv.Atoi(os.Getenv("LISTEN_FDS")); err != nil || n < 1 {
		return nil, nil
	}
	f := os.NewFile(listenFDsStart, "logis.socket")
	// FileListener keeps a close-on-exec duplicate, so launched programs do
	// not inherit the socket.
	ln, err := net.FileListener(f)
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("the socket systemd passed: %w", err)
	}
	unix, ok := ln.(*net.UnixListener)
	if !ok {
		_ = ln.Close()
		return nil, errors.New("the socket systemd passed is not a Unix stream socket")
	}
	return unix, nil
}
