package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/config"
)

// The config at the binary's boundary: --check-config, --config handed from
// a client to the daemon it starts, and live reload in the daemon. Every
// test uses the rig's scratch XDG_CONFIG_HOME and XDG_RUNTIME_DIR. The
// scripted resolver stands in for the window, so what the daemon applies is
// observed through its log and through the setting that already acts:
// idle-timeout.

const heeds = "heeds the amended config"

func (r *rig) configFile() string { return filepath.Join(r.dir, "config", "logis", "logis.jsonc") }

// logLines hands out the daemon's log lines as they are written.
type logLines struct {
	t     *testing.T
	lines chan string
	seen  []string
}

func (d *daemonProc) log(t *testing.T) *logLines {
	l := &logLines{t: t, lines: make(chan string, 256)}
	go func() {
		s := bufio.NewScanner(d.stderr)
		for s.Scan() {
			l.lines <- s.Text()
		}
		close(l.lines)
	}()
	return l
}

// line waits for a line containing every one of parts.
func (l *logLines) line(parts ...string) string {
	l.t.Helper()
	deadline := time.After(patience)
	for {
		select {
		case line, ok := <-l.lines:
			if !ok {
				l.t.Fatalf("the log ended without %q; it said %q", parts, l.seen)
			}
			l.seen = append(l.seen, line)
			if containsAll(line, parts) {
				return line
			}
		case <-deadline:
			l.t.Fatalf("no log line with %q; the log said %q", parts, l.seen)
		}
	}
}

// rest is everything logged so far, once the daemon has exited.
func (l *logLines) rest() []string {
	for line := range l.lines {
		l.seen = append(l.seen, line)
	}
	return l.seen
}

// connectFailing connects where the daemon should refuse to start; one
// that starts anyway is stopped at the end, not left behind.
func (r *rig) connectFailing(o client.Options) (*client.Conn, error) {
	c, err := client.Connect(o)
	if err == nil {
		r.t.Cleanup(func() {
			_ = c.Close()
			stopDaemon(c.Daemon)
		})
	}
	return c, err
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestE2ECheckConfigJudgesTheConfigAndExits0Or2(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), "{\n  // brass and fire\n  \"theme\": \"cogitator-phosphor\",\n}\n")
	out, err := r.logis("--check-config").Output()
	if err != nil || string(out) != "logis: the config at "+r.configFile()+
		" is sound; the theme 'cogitator-phosphor' is sound\n" {
		t.Errorf("%v: %q", err, out)
	}
	writeFile(t, r.configFile(), "{\n  \"thme\": \"x\"\n}\n")
	cmd := r.logis("--check-config")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || len(out) != 0 ||
		!strings.HasPrefix(stderr.String(), "logis: the config is heretical: "+r.configFile()+":2:8: unknown field `thme`") {
		t.Errorf("%v: stdout %q stderr %q", err, out, stderr.String())
	}
}

func TestE2EADaemonRefusesToStartOnAnInvalidConfigAndItsClientFails(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), `{"idle-timeout": "soon"}`)
	_, err := r.connectFailing(client.Options{RuntimeDir: r.dir, Exe: r.bin})
	if err == nil || !strings.Contains(err.Error(), "heretical") ||
		!strings.Contains(err.Error(), r.configFile()+":1:") {
		t.Errorf("%v", err)
	}
	if !gone(r.socket()) {
		t.Error("a daemon is listening anyway")
	}
}

func TestE2EAClientHandsItsConfigToTheDaemonItStarts(t *testing.T) {
	r := newRig(t)
	path := filepath.Join(t.TempDir(), "named.jsonc")
	writeFile(t, path, `{"heresy": true}`)
	_, err := r.connectFailing(client.Options{RuntimeDir: r.dir, Exe: r.bin,
		DaemonArgs: config.NewSource(path).DaemonArgs()})
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("%v", err)
	}
}

func TestE2EARunningDaemonKeepsItsConfigWhateverACallerNames(t *testing.T) {
	r := newRig(t)
	r.start()
	path := filepath.Join(t.TempDir(), "named.jsonc")
	writeFile(t, path, `{"heresy": true}`)
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: r.bin,
		DaemonArgs: config.NewSource(path).DaemonArgs()})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if got := r.dmenu(">x"); got.Item.Text != ">x" {
		t.Errorf("%+v", got)
	}
}

func TestE2ETheConfigsIdleTimeoutAppliesFromTheStart(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.2}`)
	d := r.start()
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
}

func TestE2EAnEditInPlaceIsAppliedAtOnce(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), "{}")
	d := r.start()
	log := d.log(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.2} // rest soon`)
	log.line(heeds)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d: %q", code, log.rest())
	}
}

func TestE2EAnAtomicallyReplacedConfigIsApplied(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), "{}")
	d := r.start()
	log := d.log(t)
	tmp := r.configFile() + ".tmp"
	writeFile(t, tmp, `{"idle-timeout": 0.2}`)
	if err := os.Rename(tmp, r.configFile()); err != nil {
		t.Fatal(err)
	}
	log.line(heeds)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d: %q", code, log.rest())
	}
}

func TestE2EAConfigWrittenAfterTheDaemonStartedIsApplied(t *testing.T) {
	r := newRig(t)
	if !gone(filepath.Dir(r.configFile())) {
		t.Fatal("the config home exists already")
	}
	d := r.start()
	log := d.log(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.2}`)
	log.line(heeds)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d: %q", code, log.rest())
	}
}

func TestE2EAnInvalidEditIsReportedAndTheLastSoundConfigStays(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.3}`)
	d := r.start()
	log := d.log(t)
	// A connected client keeps the daemon from idling out meanwhile.
	c := r.connect()

	writeFile(t, r.configFile(), "{\n  \"idle-timeout\": \"soon\"\n}")
	line := log.line("heretical", r.configFile()+":2:", "`idle-timeout`")
	if !strings.Contains(line, "the last sound config stands") {
		t.Errorf("%q", line)
	}
	writeFile(t, r.configFile(), `{"idle-timout": 0}`)
	log.line("heretical", r.configFile()+":1:", "unknown field `idle-timout`")

	// Had an invalid edit replaced the config, the daemon would stay.
	_ = c.Close()
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	for _, l := range log.rest() {
		if strings.Contains(l, heeds) {
			t.Errorf("an invalid edit was applied: %q", l)
		}
	}
}

func TestE2EASoundEditAfterAnInvalidOneIsApplied(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), "{}")
	d := r.start()
	log := d.log(t)
	writeFile(t, r.configFile(), `{"focus-loss": "linger"}`)
	log.line("heretical", "`focus-loss`")
	writeFile(t, r.configFile(), `{"focus-loss": "keep", "idle-timeout": 0.2}`)
	log.line(heeds)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d: %q", code, log.rest())
	}
}

func TestE2ETheIdleTimeoutFlagOutranksTheConfig(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.1}`)
	d := r.start("--idle-timeout", "0")
	log := d.log(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.2}`)
	log.line(heeds)
	// Elapsed time is the behaviour under test: neither timeout may end it.
	time.Sleep(500 * time.Millisecond)
	if !d.running() {
		t.Error("it left")
	}
}

func TestE2EANamedConfigIsTheOneWatched(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), `{"idle-timeout": 0.1}`)
	named := filepath.Join(t.TempDir(), "named.jsonc")
	writeFile(t, named, "{}")
	d := r.start("--config", named)
	log := d.log(t)
	time.Sleep(300 * time.Millisecond) // the default file's timeout is not the daemon's
	if !d.running() {
		t.Fatal("it read the default file")
	}
	writeFile(t, named, `{"idle-timeout": 0.2}`)
	log.line(heeds)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
}

func TestE2EADaemonStartedByAClientReloadsAfterTheClientIsGone(t *testing.T) {
	r := newRig(t)
	writeFile(t, r.configFile(), "{}")
	// The client starts a detached daemon and leaves: the daemon's starter
	// stderr is closed from here on.
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: r.bin})
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Daemon
	_ = c.Close()
	t.Cleanup(func() { stopDaemon(pid) })
	alive := func() bool { return syscall.Kill(pid, 0) == nil }

	logFile := filepath.Join(r.dir, "logis.log")
	logged := func(needle string) bool {
		text, err := os.ReadFile(logFile)
		return err == nil && strings.Contains(string(text), needle)
	}
	writeFile(t, r.configFile(), `{"theme": "../escape"}`)
	waitUntil(t, "the heresy in logis.log", func() bool { return logged("heretical") })
	if !alive() {
		t.Fatal("the daemon died reporting a heresy")
	}
	writeFile(t, r.configFile(), `{"idle-timeout": 0.2}`)
	waitUntil(t, "the reload in logis.log", func() bool { return logged(heeds) })
	waitUntil(t, "the daemon to idle out", func() bool { return !alive() })
}
