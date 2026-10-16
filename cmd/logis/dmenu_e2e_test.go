package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/wltest"
)

// The dmenu runes at the process boundary: real `logis --dmenu` clients and
// a real daemon over its socket, with the scripted resolver standing in for
// the window (it picks the first item whose shown text starts with '>', or
// dismisses). No compositor is needed, so these run everywhere;
// dmenu_window_e2e_test.go covers the window itself.

// ran is how a client ended.
type ran struct {
	code           int
	stdout, stderr string
}

// stopsItsDaemon stops, at the test's end, the daemon a client of this rig
// started (detached, so not this test's child); found by its welcome.
func (r *rig) stopsItsDaemon() {
	r.t.Cleanup(func() {
		if gone(r.socket()) {
			return
		}
		c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: "/nonexistent", Patience: time.Second})
		if err != nil {
			return
		}
		_ = c.Close()
		if !r.own[c.Daemon] {
			stopDaemon(c.Daemon)
		}
	})
}

// dmenuRun runs `logis --dmenu args…` with this stdin to completion.
func (r *rig) dmenuRun(stdin []byte, args ...string) ran {
	r.t.Helper()
	return r.runClient(r.logis(append([]string{"--dmenu"}, args...)...), stdin)
}

func (r *rig) runClient(cmd *exec.Cmd, stdin []byte) ran {
	r.t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(stdin), &stdout, &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * patience):
		_ = cmd.Process.Kill()
		<-done
		r.t.Fatalf("logis %v never ended; stderr %q", cmd.Args[1:], stderr.String())
	}
	return ran{cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()}
}

func (got ran) printed(t *testing.T, want string) {
	t.Helper()
	if got.code != 0 || got.stdout != want {
		t.Fatalf("exit %d, stdout %q, want 0 and %q; stderr %q", got.code, got.stdout, want, got.stderr)
	}
}

func dmenuRig(t *testing.T) *rig {
	r := newRig(t)
	r.stopsItsDaemon()
	return r
}

func TestE2EDmenu0ReadsNulSeparatedItemsWithNewlinesInside(t *testing.T) {
	r := dmenuRig(t)
	r.runClient(r.logis("--dmenu0"), []byte("x\x00>two\nlines\x00y\x00")).printed(t, ">two\nlines\n")
}

func TestE2EIndexPrintsThePositionOfTheChoice(t *testing.T) {
	r := dmenuRig(t)
	r.dmenuRun([]byte("a\nb\n>c\n"), "--index").printed(t, "2\n")
}

func TestE2EWithNthShowsAColumnAndTheWholeLineComesBack(t *testing.T) {
	r := dmenuRig(t)
	r.dmenuRun([]byte("id-1\tplain\nid-2\t>marked\n"), "--with-nth=2").printed(t, "id-2\t>marked\n")
}

func TestE2EAcceptNthPrintsColumnsSplitAtTheNthDelimiter(t *testing.T) {
	r := dmenuRig(t)
	got := r.dmenuRun([]byte("a,b,c\nkey,>shown,value\n"), "--with-nth={2}", "--accept-nth={3}:{1}", "--nth-delimiter=,")
	got.printed(t, "value:key\n")
}

func TestE2ETheOriginalBytesSurviveColumnsAndInvalidUTF8(t *testing.T) {
	r := dmenuRig(t)
	r.dmenuRun([]byte("\xff\xfe\t>pick\r\n"), "--with-nth=2").printed(t, "\xff\xfe\t>pick\r\n")
}

func TestE2ENoRunIfEmptyExits1AtOnceWithoutADaemon(t *testing.T) {
	r := dmenuRig(t)
	got := r.dmenuRun(nil, "--no-run-if-empty")
	if got.code != 1 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
	if !gone(r.socket()) {
		t.Fatal("a daemon was summoned")
	}
	// With input it runs as usual.
	r.dmenuRun([]byte(">x\n"), "-R").printed(t, ">x\n")
}

func TestE2EAnUnmarkedListIsDismissedWithExit1AndEmptyStdout(t *testing.T) {
	r := dmenuRig(t)
	got := r.dmenuRun([]byte("a\nb\n"))
	if got.code != 1 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
}

func TestE2EPromptOnlyNeverWaitsForStdin(t *testing.T) {
	r := dmenuRig(t)
	cmd := r.logis("--dmenu", "--prompt-only", "pin: ")
	stdin, err := cmd.StdinPipe() // stays open
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * patience):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("the client waited for stdin")
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestE2EListRunesWithoutDmenuAreAnError(t *testing.T) {
	r := newRig(t)
	got := r.runClient(r.logis("--index"), nil)
	if got.code != 2 || !strings.Contains(got.stderr, "--index") {
		t.Fatalf("%+v", got)
	}
}

func TestE2EADaemonThatCannotStartIsReportedWithExit2(t *testing.T) {
	r := newRig(t)
	cmd := r.logis("--dmenu")
	cmd.Env = append(cmd.Env, "LOGIS_RESOLVER=heresy")
	got := r.runClient(cmd, []byte("a\n"))
	if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, "LOGIS_RESOLVER names no known resolver") {
		t.Fatalf("%+v", got)
	}
}

// The caller contract: a `logis --dmenu` caller learns that logis cannot
// serve it from exit 2 and an empty stdout, with the plain reason on
// stderr, whatever stands in the way. The daemon it starts opens the
// window, and relays why it could not.
func TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy(t *testing.T) {
	r := newRig(t)
	if _, err := wltest.Serve(r.dir, "wayland-test", withoutLayerShell); err != nil {
		t.Fatal(err)
	}
	windowed := func(display string, runtimeDir bool) *exec.Cmd {
		cmd := r.logis("--dmenu")
		var env []string
		for _, kv := range cmd.Env {
			name, _, _ := strings.Cut(kv, "=")
			if name != "LOGIS_RESOLVER" && (runtimeDir || name != "XDG_RUNTIME_DIR") {
				env = append(env, kv)
			}
		}
		if display != "" {
			env = append(env, "WAYLAND_DISPLAY="+display)
		}
		cmd.Env = env
		return cmd
	}
	for _, c := range []struct {
		cmd    *exec.Cmd
		reason string
	}{
		{windowed("", true), "WAYLAND_DISPLAY is not set"},
		{windowed("wayland-test", true), "zwlr_layer_shell_v1"},
		{windowed("wayland-test", false), "XDG_RUNTIME_DIR is not set"},
	} {
		got := r.runClient(c.cmd, nil)
		if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, c.reason) {
			t.Fatalf("%s: %+v", c.reason, got)
		}
		if !gone(r.socket()) {
			t.Fatalf("%s: a daemon stayed", c.reason)
		}
	}
}
