package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// The daemon's exit with real provider processes: a provider that may be
// ended (it was never sent an answer) gets its graces before the daemon
// is gone, so it never outlives the daemon; one that was sent an answer
// may be carrying out an action and is left alone.

// script writes a provider script to dir and returns its launch.
func script(t *testing.T, dir, body string) *protocol.Launch {
	t.Helper()
	path := filepath.Join(dir, "provider.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return &protocol.Launch{Argv: [][]byte{[]byte(path)}, Cwd: []byte(dir),
		Env: [][]byte{[]byte("PATH=/usr/bin:/bin"), []byte("DIR=" + dir)}}
}

func pidFile(t *testing.T, dir, name string) int {
	t.Helper()
	var pid int
	waitUntil(t, name+" to be written", func() bool {
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !strings.HasSuffix(string(text), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(text)))
		return err == nil
	})
	return pid
}

// running: process pid runs (a zombie counts as gone).
func runs(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(stat)
	return !strings.HasPrefix(strings.TrimSpace(s[strings.LastIndexByte(s, ')')+1:]), "Z")
}

func (r *rig) openProvider(l *protocol.Launch) *raw {
	r.t.Helper()
	c := r.connect()
	c.greet()
	open := protocol.Open(protocol.ModeProvider, nil)
	open.Provider = l
	c.send(open)
	if got := c.recvType(); got != protocol.TypeVisible {
		r.t.Fatalf("the provider session is %s", got)
	}
	return c
}

func TestAtItsExitTheDaemonEndsTheProvidersThatNeverActed(t *testing.T) {
	r := newRig(t)
	d := r.start(0)
	dir := t.TempDir()
	// Never answered (no marked item); ignores cancel, end of input and
	// SIGTERM.
	r.openProvider(script(t, dir, `echo $$ > "$DIR/provider"
read -r hello
echo '{"type":"hello","protocol":1}'
echo '{"type":"stage","id":"s","items":[{"id":"a","columns":["unmarked"]}]}'
trap '' TERM
while :; do sleep 0.1; done
`))
	provider := pidFile(t, dir, "provider")
	defer func() { _ = syscall.Kill(provider, syscall.SIGKILL) }()
	d.cancel()
	if status := d.exit(t); status != 0 {
		t.Fatalf("exit %d", status)
	}
	if runs(provider) {
		t.Fatal("a provider that never acted outlived the daemon")
	}
}

func TestAtItsExitTheDaemonLeavesAnActingProviderAndItsAction(t *testing.T) {
	r := newRig(t)
	d := r.start(0)
	dir := t.TempDir()
	// Its marked item is chosen at once; then it starts an action and
	// idles on, deaf.
	r.openProvider(script(t, dir, `read -r hello
echo '{"type":"hello","protocol":1}'
echo '{"type":"stage","id":"s","items":[{"id":"a","columns":[">act"]}]}'
read -r activate
sleep 30 </dev/null >/dev/null 2>&1 &
echo $! > "$DIR/action"
echo $$ > "$DIR/provider"
exec sleep 30 </dev/null >/dev/null
`))
	action, provider := pidFile(t, dir, "action"), pidFile(t, dir, "provider")
	defer func() {
		_ = syscall.Kill(action, syscall.SIGKILL)
		_ = syscall.Kill(provider, syscall.SIGKILL)
	}()
	d.cancel()
	d.exit(t)
	time.Sleep(2500 * time.Millisecond) // past both graces
	if !runs(action) || !runs(provider) {
		t.Fatalf("action runs %v, provider runs %v", runs(action), runs(provider))
	}
}
