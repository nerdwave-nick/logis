package servitor

import (
	"os/exec"
	"syscall"
)

// Launching `servitor invoke` so that it outlives logis (servitor caller
// interface S7, codex passage detached; owner decision ae44412a). As
// fuzzel launches applications: fork and exec, with
//
//   - stdin, stdout and stderr on /dev/null, never a pipe logis would
//     leave;
//   - a session of its own (setsid), so no hangup of logis's terminal or
//     signal to logis's process group reaches it;
//   - the errno of a failed exec reported back at once (os/exec's start
//     does this through a pipe closed on exec), so a servitor that cannot
//     start is known at once.
//
// It runs in the caller's environment and working directory (this
// provider's, which the daemon gave it from the client). logis never waits
// for it, never reads its exit status and never signals it; once the
// provider exits it is adopted by the session's subreaper or init. Known
// limits (S7): SIGKILL, and a cgroup killed whole (stopping logis.service
// or the caller's scope); a launch prefix such as `systemd-run --user
// --scope` would set it apart, which this slice does not add.

// Detached starts `program args...` detached and returns once it runs.
func Detached(program string, args []string) error {
	cmd := exec.Command(program, args...)
	// Nil Stdin, Stdout and Stderr are the null device.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return startError(err)
	}
	// Never waited for: releasing it neither waits nor signals.
	return cmd.Process.Release()
}
