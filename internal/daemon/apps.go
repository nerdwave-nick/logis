package daemon

import (
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/launch"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// App sessions in the daemon (feat/logis stories 5–7, §4.7; Rust
// src/daemon/apps.rs): the index the daemon keeps resident (rescanned
// whenever the applications directories change, indexer.go), frecency,
// and the launch of the chosen application.
//
// A launch first asks the window for an xdg-activation token (the
// compositor issues it for the focused window and the key or button that
// chose), and launches once it comes, or after tokenPatience without one;
// a resolver that is no Activator gets the launch at once, without one.
// The session then closes (the client exits 0) and the launch is noted for
// frecency; a launch that fails is shown as an error toast instead and the
// stage takes Return again.

// tokenPatience is how long a launch waits for the compositor's token.
const tokenPatience = 500 * time.Millisecond

// Activator is a Resolver that asks the compositor for an xdg-activation
// token to launch appID from the visible session, and hands it over
// through its Host's Activated (also when none comes). Called on the
// daemon's goroutine; it must not block.
type Activator interface {
	Activation(id SessionID, appID string)
}

// appsState is the daemon's apps: the index, frecency and how to launch.
type appsState struct {
	index *apps.Index
	// indexed: the index has been read at all.
	indexed  bool
	frecency *apps.Frecency
	// frecencyPath is where frecency is kept ("": not kept).
	frecencyPath string
	// terminal is the config's terminal; nil: $TERMINAL -e.
	terminal []string
	// bus is the session bus's address.
	bus string
	// keep saves a frecency snapshot (the loop: off its goroutine).
	keep func(f *apps.Frecency, path string)
	// tab is plain logis's tab last used, kept in tabPath ("": not kept).
	tab     provider.Tab
	tabPath string
}

// appsFeed is one app session.
type appsFeed struct {
	// env is the client's environment.
	env []string
	// stages are its stages, once shown with an index.
	stages *provider.Apps
	// awaiting is a launch waiting for its token.
	awaiting *awaiting
}

type awaiting struct {
	plan launch.Plan
	id   string
	due  time.Time
}

func newAppsState() appsState {
	return appsState{index: &apps.Index{}, frecency: apps.NewFrecency()}
}

// configure applies what of the config the daemon itself uses.
func (c *core) configure(cfg config.Config) { c.apps.terminal = cfg.Terminal }

// firstStage is the first stage of an app session, or a placeholder while
// the index is first read.
func (c *core) firstStage() (*provider.Apps, provider.Step) {
	if !c.apps.indexed {
		o := provider.AppsOptions()
		o.Placeholder = "the census of applications is taken…"
		return nil, provider.Step{Kind: provider.StepStage, Options: o}
	}
	return provider.OpenApps(c.apps.index, c.apps.frecency, apps.Now())
}

// showApps: app session id became visible; its first stage, as plain
// logis's tabbed launcher on the tab last used when tabbed.
func (c *core) showApps(id SessionID, f *appsFeed, tabbed bool) {
	var step provider.Step
	f.stages, step = c.firstStage()
	s := Session{ID: id, Mode: protocol.ModeApps, Options: step.Options, Entries: step.Entries, Tabbed: tabbed}
	if tabbed {
		s.Tab = c.apps.tab
	}
	c.resolver.Show(s)
}

// indexed: a new index, kept, and shown by the visible session still
// waiting for it.
func (c *core) indexed(ix *apps.Index) {
	c.apps.index, c.apps.indexed = ix, true
	id, ok := c.sessions.visible()
	if cn := c.conns[id]; ok && cn != nil && cn.apps != nil && cn.apps.stages == nil {
		var step provider.Step
		cn.apps.stages, step = c.firstStage()
		c.resolver.Step(id, step)
	}
}

// answeredApps: the user answered app session id (not a dismissal).
func (c *core) answeredApps(id SessionID, f *appsFeed, answer Answer) {
	if f.awaiting != nil || f.stages == nil {
		return
	}
	said := f.stages.Answer(answer, c.apps.index, terminal(c.apps.terminal, f.env))
	for _, step := range said.Steps {
		c.resolver.Step(id, step)
	}
	if said.Launch == nil {
		return
	}
	f.awaiting = &awaiting{plan: *said.Launch, id: said.ID, due: c.now().Add(tokenPatience)}
	if a, ok := c.resolver.(Activator); ok {
		a.Activation(id, said.Launch.AppID)
	} else {
		c.activated(id, "")
	}
}

// activated: the compositor's token for session id's launch ("" for none).
func (c *core) activated(id SessionID, token string) {
	cn, ok := c.conns[id]
	if !ok || cn.apps == nil || cn.apps.awaiting == nil {
		return
	}
	f := cn.apps
	a := f.awaiting
	f.awaiting = nil
	launched, err := launch.Launch(a.plan, token, launch.Session{Bus: c.apps.bus, Env: f.env})
	if err != nil {
		message := a.plan.Name + " cannot be summoned: " + err.Error()
		c.logf(message)
		for _, step := range provider.AppsFailed(message) {
			c.resolver.Step(id, step)
		}
		return
	}
	for _, w := range launched.Warnings {
		c.logf(a.plan.AppID + ": " + w)
	}
	c.note(a.id)
	c.finish(id, protocol.Outcome{Kind: protocol.Closed})
}

// note notes a launch for frecency and keeps it.
func (c *core) note(id string) {
	c.apps.frecency.Record(id, apps.Now())
	if c.apps.frecencyPath == "" {
		return
	}
	snapshot := c.apps.frecency.Clone()
	if c.apps.keep != nil {
		c.apps.keep(snapshot, c.apps.frecencyPath)
	} else if err := snapshot.Save(c.apps.frecencyPath); err != nil {
		c.logf("frecency is not kept in " + c.apps.frecencyPath + ": " + err.Error())
	}
}

// terminal is the terminal prefix: the config's, or $TERMINAL -e from the
// caller's environment.
func terminal(configured, env []string) []string {
	if configured != nil {
		return configured
	}
	for _, kv := range env {
		if name, value, _ := strings.Cut(kv, "="); name == "TERMINAL" {
			if value == "" {
				break
			}
			return []string{value, "-e"}
		}
	}
	return nil
}

// appsDeadline is when the next launch stops waiting for its token.
func (c *core) appsDeadline(consider func(time.Time)) {
	for _, cn := range c.conns {
		if cn.apps != nil && cn.apps.awaiting != nil {
			consider(cn.apps.awaiting.due)
		}
	}
}

// appsTick launches without a token what waited too long for one.
func (c *core) appsTick(now time.Time) {
	var late []SessionID
	for id, cn := range c.conns {
		if cn.apps != nil && cn.apps.awaiting != nil && !now.Before(cn.apps.awaiting.due) {
			late = append(late, id)
		}
	}
	for _, id := range late {
		c.logf("no activation token came; the app is launched without one")
		c.activated(id, "")
	}
}
