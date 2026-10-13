package e2e

import (
	"fmt"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// VerifyPeer refuses the connected Unix socket fd unless its server is
// process expected: the isolation guard. The kernel's peer credentials
// (SO_PEERCRED) name the process that listens on the socket, so a
// connection to the live session, or to anything but the compositor the
// harness spawned, is refused before a single request is sent.
func VerifyPeer(fd, expected int) error {
	cred, err := syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return fmt.Errorf("cannot read the socket's peer credentials: %w", err)
	}
	if int(cred.Pid) != expected {
		return fmt.Errorf("refusing to inject: the socket is served by process %d, "+
			"not by the test compositor %d the harness started", cred.Pid, expected)
	}
	return nil
}

// ConnectVerified connects to the Wayland socket at path only if
// VerifyPeer proves its server is process expected.
func ConnectVerified(path string, expected int) (*wlproto.Display, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("cannot reach the test compositor at %s: %w", path, err)
	}
	if err := VerifyPeer(fd, expected); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	d, err := wlproto.ConnectFd(fd)
	if err != nil {
		return nil, fmt.Errorf("libwayland-client cannot connect: %w", err)
	}
	return d, nil
}
