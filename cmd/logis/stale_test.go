package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// A daemon left running across an upgrade (feat/logis-go 750bed66): the
// built binary runs as the daemon from a copy this test owns, which it then
// replaces as an upgrade does (a new file renamed over it).

// installCopy copies the built logis to dir/logis, as an install would.
func installCopy(t *testing.T, bin, dir string) string {
	t.Helper()
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "logis")
	if err := os.WriteFile(path+".new", b, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestADaemonWhoseBinaryWasReplacedRemindsEveryCallerAndStillServes(t *testing.T) {
	r := newRig(t)
	installed := filepath.Join(r.dir, "usr-bin")
	if err := os.MkdirAll(installed, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := installCopy(t, r.bin, installed)
	daemonCmd := exec.Command(exe, "daemon")
	daemonCmd.Env = r.env()
	d := r.startCmd(daemonCmd)

	r.dmenuRun([]byte("rite\n>oil\n")).printed(t, ">oil\n")
	if got := r.dmenuRun([]byte(">oil\n")); got.stderr != "" {
		t.Fatalf("a reminder before the upgrade: %q", got.stderr)
	}

	installCopy(t, r.bin, installed) // the upgrade
	want := "logis: logis updated — restart the daemon: kill " +
		strconv.Itoa(d.cmd.Process.Pid) + "\n"
	for range 2 {
		got := r.dmenuRun([]byte("rite\n>oil\n"))
		got.printed(t, ">oil\n")
		if got.stderr != want {
			t.Fatalf("stderr %q, want %q", got.stderr, want)
		}
	}

	// The log says it once.
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	log, _ := io.ReadAll(d.stderr)
	if n := strings.Count(string(log), strings.TrimPrefix(want, "logis: ")); n != 1 {
		t.Fatalf("the log has the reminder %d times:\n%s", n, log)
	}
}

// Under systemd's socket activation (contrib/systemd logis.socket) the
// daemon serves on the socket it was passed, and its reminder names the
// unit to restart.
func TestASocketActivatedDaemonWhoseBinaryWasReplacedNamesItsUnit(t *testing.T) {
	r := newRig(t)
	installed := filepath.Join(r.dir, "usr-bin")
	if err := os.MkdirAll(installed, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := installCopy(t, r.bin, installed)
	ln, err := net.Listen("unix", r.socket())
	if err != nil {
		t.Fatal(err)
	}
	unix := ln.(*net.UnixListener)
	unix.SetUnlinkOnClose(false) // the socket stays, as systemd's does
	passed, err := unix.File()
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close() // passed holds it
	// LISTEN_PID names the daemon's own process: the shell's, which it
	// becomes by exec.
	daemonCmd := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0" daemon`, exe)
	daemonCmd.Env = r.env()
	daemonCmd.ExtraFiles = []*os.File{passed} // descriptor 3
	r.startCmd(daemonCmd)
	_ = passed.Close()

	r.dmenuRun([]byte(">oil\n")).printed(t, ">oil\n")
	installCopy(t, r.bin, installed)
	got := r.dmenuRun([]byte(">oil\n"))
	got.printed(t, ">oil\n")
	if want := "logis: logis updated — systemctl --user restart logis\n"; got.stderr != want {
		t.Fatalf("stderr %q, want %q", got.stderr, want)
	}
}
