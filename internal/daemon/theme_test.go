package daemon

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The theme the config names (Rust src/daemon/mod.rs, lifecycle.rs
// reconfigure): worn from the start (the bundled default when it cannot be
// worn, said in the log), its file watched in themes/ beside the config, a
// sound edit worn at once, a broken one logged and toasted while the last
// sound theme stands; reduce-motion stills it.

const dons = "the cogitator dons the theme"

// themed is a configured resolver that also records each theme it is
// asked to wear.
type themed struct {
	*configured
	mu     sync.Mutex
	themes []*theme.Theme
}

func (th *themed) Retheme(t *theme.Theme) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.themes = append(th.themes, t)
}

func (th *themed) worn() []*theme.Theme {
	th.mu.Lock()
	defer th.mu.Unlock()
	return append([]*theme.Theme(nil), th.themes...)
}

func (r *rig) startThemed(source config.Source) (*running, *themed) {
	r.t.Helper()
	got := make(chan *themed, 1)
	d := r.run(Options{Config: &source, Resolver: func(h Host) (Resolver, error) {
		th := &themed{configured: &configured{Scripted: NewScripted(h)}}
		got <- th
		return th, nil
	}})
	waitUntil(r.t, "the daemon to listen", func() bool {
		return strings.Contains(d.stdout.String(), "listens at "+r.socket()) || d.exited()
	})
	select {
	case th := <-got:
		return d, th
	case <-time.After(patience):
		r.t.Fatalf("no resolver; log %q", d.stderr.String())
		return nil, nil
	}
}

func (r *rig) themeFile(name string) string {
	return filepath.Join(filepath.Dir(r.configFile()), "themes", name+".jsonc")
}

func TestTheThemeTheConfigNamesIsWornFromTheStart(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"theme": "mine"}`)
	writeConfig(t, r.themeFile("mine"), `{"extends": "cogitator-phosphor"}`)
	_, th := r.startThemed(r.named())
	worn := th.worn()
	if len(worn) != 1 || worn[0].Name != "mine" || worn[0].Dir != filepath.Dir(r.themeFile("mine")) ||
		worn[0].Effects.Curvature == 0 {
		t.Fatalf("%+v", worn)
	}
}

func TestABrokenThemeAtTheStartLeavesTheDefaultAndSaysSo(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"theme": "mine"}`)
	writeConfig(t, r.themeFile("mine"), `{"glow": 1}`)
	d, th := r.startThemed(r.named())
	if worn := th.worn(); len(worn) != 1 || worn[0].Name != theme.DefaultName {
		t.Fatalf("%+v", worn)
	}
	want := "logis: the theme is heretical: " + r.themeFile("mine") +
		":1:10: invalid type: integer `1`, expected struct GlowDoc; the bundled forge-crimson-brass stands\n"
	waitUntil(t, "the heresy", func() bool { return strings.Contains(d.stderr.String(), want) })
}

func TestAnEditedThemeIsWornAtOnceAndABrokenOneIsToasted(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{"theme": "mine"}`)
	writeConfig(t, r.themeFile("mine"), `{}`)
	d, th := r.startThemed(r.named())
	session, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeVisible {
		t.Fatalf("the session is %s", state)
	}

	writeConfig(t, r.themeFile("mine"), `{"window": {"rows": 4}}`)
	waitUntil(t, "the edited theme", func() bool { return strings.Contains(d.stderr.String(), dons+" 'mine'\n") })
	if worn := th.worn(); len(worn) != 2 || worn[1].Window.Rows != 4 {
		t.Fatalf("%+v", worn)
	}

	writeConfig(t, r.themeFile("mine"), "{\n  \"glow\": {\"radius\": \"wide\"}\n}")
	place := r.themeFile("mine") + ":2:"
	waitUntil(t, "the heresy", func() bool { return strings.Contains(d.stderr.String(), place) })
	if line := lineWith(d.stderr.String(), place); !strings.HasPrefix(line, "logis: the theme is heretical: ") ||
		!strings.HasSuffix(line, "; the last sound theme stands") {
		t.Errorf("%q", line)
	}
	_, _, toasts := th.seen()
	if len(toasts) != 1 || toasts[0].Kind != ToastThemeRejected || !strings.HasPrefix(toasts[0].Error, place) {
		t.Errorf("toasts %+v", toasts)
	}

	writeConfig(t, r.themeFile("mine"), `{"window": {"rows": 4}} // as it was`)
	waitUntil(t, "the restoration", func() bool {
		return strings.Contains(d.stderr.String(), "logis: the theme is sound once more; nothing has changed\n")
	})
	if worn := th.worn(); len(worn) != 2 {
		t.Errorf("a refused or restored theme was worn: %d", len(worn))
	}
	session.send(protocol.Cancel())
}

func TestReducingMotionStillsTheWornTheme(t *testing.T) {
	r := newRig(t)
	writeConfig(t, r.configFile(), `{}`)
	d, th := r.startThemed(r.named())
	if worn := th.worn(); len(worn) != 1 || !worn[0].IsContinuous() {
		t.Fatalf("%+v", worn)
	}
	writeConfig(t, r.configFile(), `{"reduce-motion": true}`)
	waitUntil(t, "the stilled theme", func() bool {
		return strings.Contains(d.stderr.String(), dons+" 'forge-crimson-brass'\n")
	})
	if worn := th.worn(); len(worn) != 2 || worn[1].IsContinuous() || !strings.Contains(d.stderr.String(), heeds) {
		t.Fatalf("%+v; log %q", worn, d.stderr.String())
	}
}
