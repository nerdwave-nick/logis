package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// startDaemon starts `logis daemon --detach` and waits until it listens, at
// most until deadline. The daemon runs in a process group of its own (Ctrl+C
// in the client's terminal must not reach it), detached from the client's
// stdio. Its stdout is a pipe: the readiness line arrives there, and end of
// stream without it means the daemon gave up, its reason on stderr.
func startDaemon(o Options, deadline time.Time) error {
	exe := o.Exe
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot find its own binary to rouse the machine spirit: %w", err)
		}
		exe = self
	}
	ready, theirs, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("cannot rouse the machine spirit: %w", err)
	}
	defer func() { _ = ready.Close() }()
	cmd := exec.Command(exe, append([]string{"daemon", "--detach"}, o.DaemonArgs...)...)
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+o.RuntimeDir)
	cmd.Stdout = theirs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	if err == nil {
		err = cmd.Start()
	}
	_ = theirs.Close()
	if err != nil {
		return fmt.Errorf("cannot rouse the machine spirit: %w", err)
	}
	wait := time.Until(deadline)
	_ = ready.SetReadDeadline(later(deadline))
	var b [1]byte
	n, err := ready.Read(b[:])
	switch {
	case n == 1:
		_ = stderr.Close()
		// Reaped when it exits, should this client outlive it.
		go func() { _ = cmd.Wait() }()
		return nil
	case errors.Is(err, io.EOF):
		text, _ := io.ReadAll(stderr)
		status := cmd.Wait()
		reason := strings.TrimPrefix(strings.TrimSpace(string(text)), "logis: ")
		if reason == "" {
			return fmt.Errorf("the machine spirit could not be roused (%v)", status)
		}
		return fmt.Errorf("the machine spirit could not be roused: %s", reason)
	default:
		_ = cmd.Process.Kill()
		_ = stderr.Close()
		_ = cmd.Wait()
		return fmt.Errorf("the machine spirit did not wake within %.1f s (%v)", wait.Seconds(), err)
	}
}
