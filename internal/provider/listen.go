package provider

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// MaxLine is the longest line a provider may write.
const MaxLine = 16 << 20

// listen is the listener goroutine. It waits for output or the exit (the
// pidfd), so an exit is seen even while a child the provider started still
// holds its stdout. Lines go to heard while it wants them; the output is
// read on to its end in any case, never left to fill the pipe nor closed
// under a child that still writes to it.
func listen(stdout, pidfd int, proc *os.Process, heard func(Heard) bool) {
	defer closeFds(stdout, pidfd)
	lines := &lineSplitter{heard: heard}
	buf := make([]byte, 64<<10)
	exited := false
	for {
		if !exited {
			readable, err := waitReadable(stdout, pidfd)
			if err != nil {
				continue // interrupted
			}
			if !readable {
				// Gone, with nothing more to say for now: an unended last
				// line counts, and the rest is only drained.
				lines.finish()
				heard(Heard{Exited: true, Status: reap(proc)})
				exited = true
				continue
			}
		}
		n, err := syscall.Read(stdout, buf)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if n <= 0 || err != nil {
			break
		}
		lines.feed(buf[:n])
	}
	if !exited {
		lines.finish()
		heard(Heard{Exited: true, Status: reap(proc)})
	}
}

// waitReadable waits until stdout has something to read (or is closed), or
// the process is gone; true for stdout.
func waitReadable(stdout, pidfd int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(stdout), Events: unix.POLLIN}, {Fd: int32(pidfd), Events: unix.POLLIN}}
	if _, err := unix.Poll(fds, -1); err != nil {
		if errors.Is(err, unix.EINTR) {
			return false, err
		}
		return true, nil // let the read report it
	}
	return fds[0].Revents != 0, nil
}

// reap waits for the gone process and says how it ended, as Rust does.
func reap(proc *os.Process) string {
	state, err := proc.Wait()
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	return StatusOf(state)
}

// StatusOf says how a process ended as Rust's ExitStatus does: "exit
// status: 0", "signal: 15 (SIGTERM)".
func StatusOf(state *os.ProcessState) string {
	ws, ok := state.Sys().(syscall.WaitStatus)
	switch {
	case !ok:
		return state.String()
	case ws.Exited():
		return fmt.Sprintf("exit status: %d", ws.ExitStatus())
	case ws.Signaled():
		s := fmt.Sprintf("signal: %d (%s)", int(ws.Signal()), unix.SignalName(ws.Signal()))
		if ws.CoreDump() {
			s += " (core dumped)"
		}
		return s
	}
	return state.String()
}

// lineSplitter splits output into lines and hands them on while the
// listener wants them.
type lineSplitter struct {
	heard   func(Heard) bool
	partial []byte
	deaf    bool
}

func (l *lineSplitter) feed(b []byte) {
	for {
		end := bytes.IndexByte(b, '\n')
		if end < 0 {
			break
		}
		if len(l.partial) == 0 {
			l.deliver(b[:end])
		} else {
			l.partial = append(l.partial, b[:end]...)
			line := l.partial
			l.partial = nil
			l.deliver(line)
		}
		b = b[end+1:]
	}
	if l.deaf {
		return
	}
	l.partial = append(l.partial, b...)
	if len(l.partial) > MaxLine {
		l.partial = nil
		err := fmt.Errorf("a line longer than %d bytes", MaxLine)
		l.deaf = !l.heard(Heard{Err: err})
	}
}

// finish: the output ended (or the provider did); an unended line counts.
func (l *lineSplitter) finish() {
	line := l.partial
	l.partial = nil
	l.deliver(line)
}

func (l *lineSplitter) deliver(line []byte) {
	if l.deaf || len(bytes.TrimSpace(line)) == 0 {
		return
	}
	m, err := Parse(line)
	l.deaf = !l.heard(Heard{Message: m, Err: err})
}
