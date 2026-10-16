package launch

// Starting an application's process so that it outlives logis: in a
// session of its own (setsid: no controlling terminal, not in the daemon's
// process group, so nothing aimed at the daemon reaches it), with stdin
// from and stdout/stderr to /dev/null, and reaped by a goroutine so it
// never lingers as a zombie while the daemon runs (after the daemon, the
// subreaper or init inherits it).
//
// To go into a scope from its first instruction, a process is held: it
// starts as /bin/sh waiting for a line on its stdin, is moved into the
// scope by pid, then released and replaced by the application (exec, same
// pid). os/exec returns only after the exec, so holding inside the child
// before it would block the daemon; the shell is the holder.

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// hold waits on a line, then becomes the application ("$@").
const hold = `read -r _; exec "$@" </dev/null`

// A started process: held, or already the application.
type started struct {
	cmd  *exec.Cmd
	hold io.WriteCloser
}

// start starts argv detached, in env and cwd; program is argv[0] found on
// the session's PATH. held keeps it waiting for release.
func start(program string, argv, env []string, cwd string, held bool) (*started, error) {
	if len(argv) == 0 {
		return nil, errors.New("no program")
	}
	cmd := &exec.Cmd{Path: program, Args: argv}
	if held {
		cmd = &exec.Cmd{Path: "/bin/sh", Args: append([]string{"/bin/sh", "-c", hold, "logis-launch"}, argv...)}
	}
	// Never the daemon's own environment: an empty Env would mean it.
	cmd.Env = append([]string{}, env...)
	cmd.Dir = cwd
	// Stdin, Stdout and Stderr left nil are /dev/null.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	s := &started{cmd: cmd}
	if held {
		pipe, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		s.hold = pipe
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return s, nil
}

// pid is the process's id (the application's, once released).
func (s *started) pid() int { return s.cmd.Process.Pid }

// release lets a held process become the application, and leaves it to be
// reaped when it exits.
func (s *started) release() {
	if s.hold != nil {
		_, _ = io.WriteString(s.hold, "\n")
		_ = s.hold.Close()
	}
	go func() { _ = s.cmd.Wait() }()
}

// inUnit waits (at most patience) until process pid is in unit's cgroup.
func inUnit(pid int, unit string, patience time.Duration) bool {
	deadline := time.Now().Add(patience)
	suffix := "/" + unit
	for {
		cgroup, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
		for _, line := range strings.Split(string(cgroup), "\n") {
			if strings.HasSuffix(strings.TrimRight(line, " \t\r"), suffix) {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}
