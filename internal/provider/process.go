package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// A provider process (Rust src/provider/process.rs), spawned in its own
// process group with the caller's working directory and environment,
// spoken to through its stdin and heard on its stdout, one JSON line at a
// time.
//
//   - A listener goroutine turns stdout lines into callbacks, reads on to
//     the end (so the provider never blocks on a full pipe), and reports
//     the exit: it waits on the output and a pidfd together, so the exit is
//     seen even while a child the provider started still holds its stdout.
//   - Writes never block the daemon: stdin is non-blocking, and a provider
//     that does not read its input is an error.
//   - Signals go through the process's pidfd (os.Process), so they never
//     reach another process that reused the pid, and only to the provider
//     itself: never to its process group or the children it started.

// Launch is what to run, where, and with what environment (the caller's).
type Launch struct {
	Argv []string
	Cwd  string
	// Env is NAME=VALUE each.
	Env []string
}

// LaunchOf is a provider as a client asked for it: its bytes as strings,
// and only the environment's NAME=VALUE pairs.
func LaunchOf(l protocol.Launch) Launch {
	out := Launch{Cwd: string(l.Cwd)}
	for _, a := range l.Argv {
		out.Argv = append(out.Argv, string(a))
	}
	for _, kv := range l.Env {
		if bytes.IndexByte(kv, '=') >= 0 {
			out.Env = append(out.Env, string(kv))
		}
	}
	return out
}

// Heard is what the listener reports: one line (its Message, or Err why it
// could not be read), or the provider's exit (Exited, how in Status, as
// Rust's ExitStatus says it: "exit status: 0", "signal: 15 (SIGTERM)").
type Heard struct {
	Message Message
	Err     error
	Exited  bool
	Status  string
}

// Process is a running provider. Its methods are for one goroutine (the
// daemon's); the listener has its own descriptors.
type Process struct {
	proc  *os.Process
	stdin int // -1 once closed
}

// Spawn starts the provider. heard gets each line until it returns false,
// then the exit; it runs on the listener goroutine.
func Spawn(l Launch, heard func(Heard) bool) (*Process, error) {
	if len(l.Argv) == 0 {
		return nil, errors.New("a provider needs a program to run")
	}
	cwd := l.Cwd
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		cwd = "/"
	}
	path, err := lookPath(l.Argv[0], l.Env, cwd)
	if err != nil {
		return nil, fmt.Errorf("'%s' cannot be started: %v", l.Argv[0], err)
	}
	var in, out [2]int
	if err := syscall.Pipe2(in[:], syscall.O_CLOEXEC); err != nil {
		return nil, fmt.Errorf("the provider cannot be piped: %v", err)
	}
	if err := syscall.Pipe2(out[:], syscall.O_CLOEXEC); err != nil {
		closeFds(in[0], in[1])
		return nil, fmt.Errorf("the provider cannot be piped: %v", err)
	}
	childIn, childOut := os.NewFile(uintptr(in[0]), "stdin"), os.NewFile(uintptr(out[1]), "stdout")
	pidfd := -1
	proc, err := os.StartProcess(path, l.Argv, &os.ProcAttr{
		Dir:   cwd,
		Env:   l.Env,
		Files: []*os.File{childIn, childOut, os.Stderr},
		// Its own process group: signals for the daemon's group (a
		// terminal's Ctrl+C) never reach it or the actions it starts.
		Sys: &syscall.SysProcAttr{Setpgid: true, PidFD: &pidfd},
	})
	_, _ = childIn.Close(), childOut.Close()
	if err != nil {
		closeFds(in[1], out[0])
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, fmt.Errorf("'%s' cannot be started: %v", l.Argv[0], err)
	}
	if pidfd < 0 {
		_ = proc.Kill()
		_, _ = proc.Wait()
		closeFds(in[1], out[0])
		return nil, errors.New("the provider cannot be watched: no pidfd")
	}
	_ = syscall.SetNonblock(in[1], true)
	go listen(out[0], pidfd, proc, heard)
	return &Process{proc: proc, stdin: in[1]}, nil
}

// Send sends one line. It fails when the provider closed its input, or
// does not read it (the pipe is full).
func (p *Process) Send(m ToProvider) error {
	if p.stdin < 0 {
		return errors.New("the provider's input is closed")
	}
	for line := Encode(m); len(line) > 0; {
		n, err := syscall.Write(p.stdin, line)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("the provider does not take its input: %v", err)
		}
		line = line[n:]
	}
	return nil
}

// CloseInput closes the provider's stdin: it reads end of file.
func (p *Process) CloseInput() {
	if p.stdin >= 0 {
		_ = syscall.Close(p.stdin)
		p.stdin = -1
	}
}

// Signal sends sig to the provider alone. Harmless once it is gone.
func (p *Process) Signal(sig syscall.Signal) { _ = p.proc.Signal(sig) }

// lookPath finds name as the caller would: a name with a slash as it is
// (relative to cwd), any other on the caller's PATH (the daemon's when
// the caller has none).
func lookPath(name string, env []string, cwd string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	search, found := "", false
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			search, found = v, true
		}
	}
	if !found {
		search = os.Getenv("PATH")
	}
	for _, dir := range filepath.SplitList(search) {
		if dir == "" {
			dir = "."
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", errors.New("no such program on the caller's PATH")
}

func closeFds(fds ...int) {
	for _, fd := range fds {
		_ = syscall.Close(fd)
	}
}
