package daemon

import (
	"time"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/launch"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// App sessions in the daemon (feat/logis stories 5–7, §4.7): the index the
// daemon keeps resident (rescanned whenever the applications directories
// change, indexer.go), frecency, and the launch of the chosen application.
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
	// terminal is the config's terminal; nil: one is sought
	// (launch.FindTerminal).
	terminal []string
	// config is the config file the daemon reads ("": none), which a
	// missing terminal's error names.
	config string
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
	// toast is when its error toast goes (zero: none shows).
	toast time.Time
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
// logis's tabbed launcher on the tab last used when tabbed, its rites
// silent when it has none to run.
func (c *core) showApps(id SessionID, f *appsFeed, tabbed, silent bool) {
	var step provider.Step
	f.stages, step = c.firstStage()
	s := Session{ID: id, Mode: protocol.ModeApps, Options: step.Options, Entries: step.Entries, Tabbed: tabbed}
	if tabbed {
		s.Tab, s.RitesSilent = c.apps.tab, silent
	}
	c.reveal(s)
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
	said := f.stages.Answer(answer, c.apps.index, c.terminalFor(f.env))
	if said.Failure != "" {
		c.logf(said.Failure)
	}
	c.appsSteps(id, f, said.Steps)
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
		c.appsSteps(id, f, provider.AppsFailed(message))
		return
	}
	for _, w := range launched.Warnings {
		c.logf(a.plan.AppID + ": " + w)
	}
	c.note(a.id)
	c.finish(id, protocol.Outcome{Kind: protocol.Closed})
}

// appsSteps hands app session id's steps to the window, timing a toast:
// it goes after its span, or once its words are read.
func (c *core) appsSteps(id SessionID, f *appsFeed, steps []provider.Step) {
	for _, step := range steps {
		if step.Kind == provider.StepToast {
			f.toast = c.now().Add(step.Notice.ReadFor(step.Span))
		}
		c.resolver.Step(id, step)
	}
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

// terminalFor finds the terminal for a launch asked by a client with
// environment env: the config's, else one sought on the client's
// environment, afresh at each launch.
func (c *core) terminalFor(env []string) apps.Terminal {
	configured, file := c.apps.terminal, c.apps.config
	return func() ([]string, error) {
		if argv, ok := launch.FindTerminal(configured, env); ok {
			return argv, nil
		}
		return nil, &apps.NoTerminalError{Config: file}
	}
}

// appsDeadline is when the next launch stops waiting for its token.
func (c *core) appsDeadline(consider func(time.Time)) {
	for _, cn := range c.conns {
		if cn.apps != nil && cn.apps.awaiting != nil {
			consider(cn.apps.awaiting.due)
		}
		if cn.apps != nil {
			consider(cn.apps.toast)
		}
	}
}

// appsTick launches without a token what waited too long for one, and
// takes away the toasts that have had their time.
func (c *core) appsTick(now time.Time) {
	var late, spent []SessionID
	for id, cn := range c.conns {
		if cn.apps != nil && cn.apps.awaiting != nil && !now.Before(cn.apps.awaiting.due) {
			late = append(late, id)
		}
		if cn.apps != nil && !cn.apps.toast.IsZero() && !now.Before(cn.apps.toast) {
			cn.apps.toast = time.Time{}
			spent = append(spent, id)
		}
	}
	if lingerer, ok := c.resolver.(Lingerer); ok {
		for _, id := range spent {
			lingerer.Untoast(id)
		}
	}
	for _, id := range late {
		c.logf("no activation token came; the app is launched without one")
		c.activated(id, "")
	}
}
