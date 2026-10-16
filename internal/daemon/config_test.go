package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// The daemon's config: read before the socket is taken, its idle-timeout
// in force from the start, sound edits applied at once (to the resolver,
// that is the window, too), invalid ones logged and toasted while the last
// sound config stands.

const heeds = "heeds the amended config"

func writeConfig(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Written whole and closed, as an editor's save in place.
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// configured is a resolver (Scripted underneath) that records what the
// daemon hands the window: each config, and whether a session was visible
// when it came; each toast.
type configured struct {
	*Scripted
	mu      sync.Mutex
	visible bool
	configs []config.Config
	shownAt []bool
	toasts  []Toast
}

func (c *configured) Show(s Session) {
	c.mu.Lock()
	c.visible = true
	c.mu.Unlock()
	c.Scripted.Show(s)
}

func (c *configured) Hide(id SessionID) {
	c.mu.Lock()
	c.visible = false
	c.mu.Unlock()
	c.Scripted.Hide(id)
}

func (c *configured) Configure(cfg config.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs = append(c.configs, cfg)
	c.shownAt = append(c.shownAt, c.visible)
}

func (c *configured) Toast(t Toast) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toasts = append(c.toasts, t)
}

func (c *configured) seen() ([]config.Config, []bool, []Toast) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]config.Config(nil), c.configs...), append([]bool(nil), c.shownAt...),
		append([]Toast(nil), c.toasts...)
}

// startConfigured runs a daemon reading the config source, and waits until
// it listens.
func (r *rig) startConfigured(source config.Source, o Options) (*running, *configured) {
	r.t.Helper()
	o.Config = &source
	resolver := make(chan *configured, 1)
	o.Resolver = func(h Host) (Resolver, error) {
		c := &configured{Scripted: NewScripted(h)}
		resolver <- c
		return c, nil
	}
	d := r.run(o)
	waitUntil(r.t, "the daemon to listen", func() bool {
		return strings.Contains(d.stdout.String(), "listens at "+r.socket()) || d.exited()
	})
	select {
	case c := <-resolver:
		return d, c
	case <-time.After(patience):
		r.t.Fatalf("no resolver; log %q", d.stderr.String())
		return nil, nil
	}
}

func (r *rig) configFile() string { return filepath.Join(r.dir, "config", "logis", "logis.jsonc") }

// named is the config file as --config names it.
func (r *rig) named() config.Source { return config.NewSource(r.configFile()) }

// byDefault is the config file found through a scratch XDG_CONFIG_HOME.
func (r *rig) byDefault() config.Source {
	r.t.Setenv("XDG_CONFIG_HOME", filepath.Join(r.dir, "config"))
	source := config.NewSource("")
	if source.Path() != r.configFile() {
		r.t.Fatalf("the default is %s", source.Path())
	}
	return source
}

func TestAnInvalidConfigKeepsTheDaemonFromStarting(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"idle-timeout": "soon"}`)
	source := config.NewSource(r.configFile())
	d := r.run(Options{Config: &source})
	if status := d.exit(t); status != 2 {
		t.Errorf("exit %d", status)
	}
	want := "logis: the config is heretical: " + r.configFile() + ":1:"
	if !strings.HasPrefix(d.stderr.String(), want) || !strings.Contains(d.stderr.String(), "`idle-timeout`") {
		t.Errorf("log %q", d.stderr.String())
	}
	if !gone(r.socket()) {
		t.Error("a socket was taken anyway")
	}
}

func gone(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func TestTheConfigsIdleTimeoutAppliesFromTheStart(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"idle-timeout": 0.2}`)
	d, c := r.startConfigured(r.named(), Options{})
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d; log %q", status, d.stderr.String())
	}
	configs, _, _ := c.seen()
	if len(configs) != 1 || configs[0].IdleTimeout != 200*time.Millisecond {
		t.Errorf("the resolver was configured with %+v", configs)
	}
}

func TestAMissingDefaultConfigMeansTheDefaults(t *testing.T) {
	r := newRig(t)
	_, c := r.startConfigured(r.byDefault(), Options{})
	if configs, _, _ := c.seen(); len(configs) != 1 || !configs[0].Equal(config.Default()) {
		t.Errorf("%+v", configs)
	}
}

func TestASoundEditIsAppliedAtOnceAlsoToAVisibleSession(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), "{}")
	d, c := r.startConfigured(r.named(), Options{})
	session, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeVisible {
		t.Fatalf("the session is %s", state)
	}
	writeConfig(t, r.configFile(), `{"focus-loss": "keep"} // stay`)
	waitUntil(t, "the reload", func() bool { return strings.Contains(d.stderr.String(), heeds) })
	configs, shown, _ := c.seen()
	if len(configs) != 2 || configs[1].FocusLoss != config.FocusKeep || !shown[1] {
		t.Errorf("configs %+v while visible %v", configs, shown)
	}
	session.send(protocol.Cancel())
}

func TestAnEditedIdleTimeoutAppliesWhileTheDaemonIsIdle(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), "{}")
	d, _ := r.startConfigured(r.named(), Options{})
	tmp := r.configFile() + ".tmp"
	writeConfig(t, tmp, `{"idle-timeout": 0.2}`)
	if err := os.Rename(tmp, r.configFile()); err != nil {
		t.Fatal(err)
	}
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d", status)
	}
	if !strings.Contains(d.stderr.String(), heeds) {
		t.Errorf("log %q", d.stderr.String())
	}
}

func TestAConfigWrittenAfterTheDaemonStartedIsApplied(t *testing.T) {
	r := newRig(t)
	d, _ := r.startConfigured(r.byDefault(), Options{})
	writeConfig(t, r.configFile(), `{"idle-timeout": 0.2}`)
	if status := d.exit(t); status != 0 || !strings.Contains(d.stderr.String(), heeds) {
		t.Errorf("exit %d; log %q", status, d.stderr.String())
	}
}

func TestAnInvalidEditIsLoggedAndToastedAndTheLastSoundConfigStands(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"idle-timeout": 0.3}`)
	d, c := r.startConfigured(r.named(), Options{})
	// A connected client keeps the daemon from idling out meanwhile.
	client := r.connect()
	client.greet()

	writeConfig(t, r.configFile(), "{\n  \"idle-timeout\": \"soon\"\n}")
	place := r.configFile() + ":2:"
	waitUntil(t, "the heresy", func() bool { return strings.Contains(d.stderr.String(), place) })
	line := lineWith(d.stderr.String(), place)
	if !strings.HasPrefix(line, "logis: the config is heretical: ") ||
		!strings.HasSuffix(line, "; the last sound config stands") || !strings.Contains(line, "`idle-timeout`") {
		t.Errorf("%q", line)
	}
	_, _, toasts := c.seen()
	if len(toasts) != 1 || toasts[0].Kind != ToastConfigRejected || !strings.Contains(toasts[0].Error, place) {
		t.Errorf("toasts %+v", toasts)
	}

	writeConfig(t, r.configFile(), `{"idle-timout": 0}`)
	waitUntil(t, "the second heresy", func() bool {
		return strings.Contains(d.stderr.String(), "unknown field `idle-timout`")
	})
	// Had an invalid edit replaced the config, the daemon would stay.
	_ = client.conn.Close()
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d", status)
	}
	if configs, _, _ := c.seen(); len(configs) != 1 || strings.Contains(d.stderr.String(), heeds) {
		t.Errorf("an invalid edit was applied: %+v; log %q", configs, d.stderr.String())
	}
}

func lineWith(log, part string) string {
	for line := range strings.Lines(log) {
		if strings.Contains(line, part) {
			return strings.TrimSuffix(line, "\n")
		}
	}
	return ""
}

func TestTheSameConfigAfterAHeresyIsRestoredAndANewOneApplied(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), "{}")
	d, c := r.startConfigured(r.named(), Options{})
	writeConfig(t, r.configFile(), `{"focus-loss": "linger"}`)
	waitUntil(t, "the heresy", func() bool { return strings.Contains(d.stderr.String(), "`focus-loss`") })
	writeConfig(t, r.configFile(), "{} // as it was")
	waitUntil(t, "the restoration", func() bool {
		return strings.Contains(d.stderr.String(), "logis: the config is sound once more; nothing has changed\n")
	})
	writeConfig(t, r.configFile(), `{"focus-loss": "keep", "idle-timeout": 0.2}`)
	if status := d.exit(t); status != 0 || !strings.Contains(d.stderr.String(), heeds) {
		t.Errorf("exit %d; log %q", status, d.stderr.String())
	}
	if configs, _, _ := c.seen(); len(configs) != 2 || configs[1].FocusLoss != config.FocusKeep {
		t.Errorf("%+v", configs)
	}
}

func TestTheIdleTimeoutFlagOutranksTheConfig(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"idle-timeout": 0.1}`)
	d, _ := r.startConfigured(r.named(), Options{IdleTimeout: 0, IdleTimeoutSet: true})
	writeConfig(t, r.configFile(), `{"idle-timeout": 0.2}`)
	waitUntil(t, "the reload", func() bool { return strings.Contains(d.stderr.String(), heeds) })
	// Elapsed time is the behaviour under test: neither timeout may end it.
	time.Sleep(500 * time.Millisecond)
	if d.exited() {
		t.Errorf("it left; log %q", d.stderr.String())
	}
}

func TestANonZeroIdleTimeoutCountsAsTheFlag(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"idle-timeout": 0}`)
	d, _ := r.startConfigured(r.named(), Options{IdleTimeout: 100 * time.Millisecond})
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d", status)
	}
}
