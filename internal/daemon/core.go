package daemon

import (
	"fmt"
	"time"

	"github.com/nerdwave-nick/logis/internal/launch"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// peer is the sending half of one client connection.
type peer interface {
	// send sends one reply; a failure shows later as the reader's hang-up.
	send(reply protocol.Reply)
	// close ends the connection; the client sees end of stream.
	close()
}

type phase int

const (
	// greeting: waiting for hello.
	greeting phase = iota
	// ready: greeted; waiting for open.
	ready
	// opened: a session is open.
	opened
)

// conn is one client connection and the session it may carry, with what
// feeds that session (Rust Feed): the built-in dmenu provider (dmenu, and
// the list of servitor sessions until its feature), the app launcher, or
// an external provider: its launch until the session is shown, then the
// started provider.
type conn struct {
	peer  peer
	phase phase
	mode  protocol.Mode
	dmenu *provider.Dmenu
	apps  *appsFeed
	// ended: the dmenu client sent all its items.
	ended  bool
	launch *protocol.Launch
	live   *live
	// tabbed: plain logis's tabbed launcher, its rites run by launch
	// (tabs.go).
	tabbed bool
}

// core is the daemon's protocol state machine: connections, their sessions,
// their providers, the session policy and the resolver. It does no I/O of
// its own and runs on the daemon's goroutine only; it answers through each
// connection's peer, and its timers (deadline, tick) run on the loop's
// clock.
type core struct {
	resolver  Resolver
	sessions  sessions
	conns     map[SessionID]*conn
	pid       int
	providers providers
	apps      appsState
	now       func() time.Time
	// logf writes one line to the daemon's log.
	logf func(message string)
}

func newCore(resolver Resolver, pid int) *core {
	return &core{resolver: resolver, conns: map[SessionID]*conn{}, pid: pid,
		providers: providers{spawn: noSpawn}, apps: newAppsState(), now: time.Now, logf: func(string) {}}
}

// idle: no client is connected and no provider is being ended.
func (c *core) idle() bool { return len(c.conns) == 0 && len(c.providers.retiring) == 0 }

func (c *core) connect(id SessionID, p peer) {
	c.conns[id] = &conn{peer: p}
}

// receive handles one request of connection id, or the reason it could not
// be decoded.
func (c *core) receive(id SessionID, request protocol.Request, undecodable error) {
	cn, ok := c.conns[id]
	if !ok {
		return
	}
	if undecodable != nil {
		c.fail(id, undecodable.Error())
		return
	}
	switch {
	case cn.phase == greeting && request.Type == protocol.TypeHello:
		if request.Version != protocol.Version {
			c.fail(id, fmt.Sprintf("the client speaks protocol %d, this machine spirit protocol %d; "+
				"restart the daemon so both speak the same", request.Version, protocol.Version))
			return
		}
		cn.phase = ready
		cn.peer.send(protocol.Welcome(c.pid))
	case cn.phase == ready && request.Type == protocol.TypeOpen:
		c.open(id, cn, request)
	case cn.phase == opened && request.Type == protocol.TypeItems:
		c.items(id, cn, request.Items)
	case cn.phase == opened && request.Type == protocol.TypeEnd && cn.mode == protocol.ModeDmenu && !cn.ended:
		cn.ended = true
		if visible, ok := c.sessions.visible(); ok && visible == id {
			c.resolver.End(id)
		}
	case cn.phase == opened && request.Type == protocol.TypeCancel:
		c.finish(id, protocol.Outcome{Kind: protocol.Cancelled})
	default:
		c.outOfPlace(id, cn, request.Type)
	}
}

func (c *core) open(id SessionID, cn *conn, request protocol.Request) {
	switch {
	case request.Mode == protocol.ModeProvider && request.Provider == nil:
		c.fail(id, "a provider session must name its provider")
		return
	case request.Mode != protocol.ModeProvider && request.Provider != nil:
		c.fail(id, "only a provider session names a provider")
		return
	case request.Mode != protocol.ModeApps && request.Rites != nil:
		c.fail(id, "only plain logis's app session names its rites")
		return
	}
	if err := c.resolver.Accepts(request.Mode); err != nil {
		c.fail(id, err.Error())
		return
	}
	switch request.Mode {
	case protocol.ModeProvider:
		cn.launch = request.Provider
	case protocol.ModeApps:
		cn.apps = &appsFeed{env: launch.EnvFromBytes(request.Env)}
		cn.launch, cn.tabbed = request.Rites, request.Rites != nil
	default:
		o, err := protocol.DecodeOptions(request.Options)
		if err != nil {
			c.fail(id, err.Error())
			return
		}
		cn.dmenu = provider.NewDmenu(o)
	}
	cn.phase, cn.mode = opened, request.Mode
	c.apply(c.sessions.open(id, request.Mode.Interactive()))
}

func (c *core) items(id SessionID, cn *conn, items []protocol.Item) {
	switch {
	case cn.mode != protocol.ModeDmenu:
		c.fail(id, "only dmenu sessions take items")
	case cn.ended:
		c.fail(id, "items arrived after end")
	default:
		if step, shown := cn.dmenu.Push(items); shown {
			c.resolver.Step(id, step)
		}
	}
}

func (c *core) outOfPlace(id SessionID, cn *conn, name string) {
	expected := "the session is already open"
	switch cn.phase {
	case greeting:
		expected = "the first word must be 'hello'"
	case ready:
		expected = "a session begins with 'open'"
	}
	c.fail(id, fmt.Sprintf("'%s' is out of place: %s", name, expected))
}

func (c *core) disconnect(id SessionID) {
	if cn, ok := c.conns[id]; ok {
		delete(c.conns, id)
		cn.peer.close()
		c.closeSession(id, cn, false)
	}
}

// answered: the user answered the visible session.
func (c *core) answered(id SessionID, answer Answer) {
	cn, ok := c.conns[id]
	if visible, shown := c.sessions.visible(); !ok || !shown || visible != id {
		return
	}
	if cn.tabbed {
		c.rememberTab(answer.Tab)
	}
	switch {
	case cn.mode == protocol.ModeDmenu:
		if outcome := cn.dmenu.Outcome(answer); outcome.Kind != protocol.Dismissed || answer.Kind == AnswerDismissed {
			c.finish(id, outcome)
		}
	case answer.Kind == AnswerDismissed:
		c.finish(id, protocol.Outcome{Kind: protocol.Dismissed})
	case cn.tabbed && answer.Tab == provider.TabRites, cn.live != nil && !cn.tabbed:
		c.answeredProvider(id, cn.live, answer)
	case cn.apps != nil:
		c.answeredApps(id, cn.apps, answer)
	}
	// What the user chose in a servitor session is for that feature's
	// ticket to carry out.
}

// failed: the resolver could not show the session; its client gets message
// (and exits 2), and the next session is shown.
func (c *core) failed(id SessionID, message string) {
	if visible, ok := c.sessions.visible(); ok && visible == id {
		c.fail(id, message)
	}
}

// finish ends a session with its outcome and closes its connection.
func (c *core) finish(id SessionID, outcome protocol.Outcome) {
	c.end(id, protocol.Result(outcome), outcome.Kind == protocol.Closed)
}

// fail ends a connection with an error, cancelling any session it had.
func (c *core) fail(id SessionID, message string) {
	c.end(id, protocol.Error(message), false)
}

func (c *core) end(id SessionID, last protocol.Reply, closed bool) {
	cn, ok := c.conns[id]
	if !ok {
		return
	}
	delete(c.conns, id)
	cn.peer.send(last)
	cn.peer.close()
	c.closeSession(id, cn, closed)
}

// closeSession: the session of cn is over. Its provider retires, and the
// window hides, or lingers on a toast after the provider's own close.
func (c *core) closeSession(id SessionID, cn *conn, closed bool) {
	linger := cn.live != nil && c.providers.retire(id, cn.live, closed, c.now())
	effects := c.sessions.close(id)
	if lingerer, ok := c.resolver.(Lingerer); ok && linger {
		for i, e := range effects {
			if e == (effect{hide, id}) {
				effects = append(effects[:i:i], effects[i+1:]...)
				lingerer.Linger(id)
				break
			}
		}
	}
	c.apply(effects)
}

// closeAll ends every connection without a word, as the daemon's exit does;
// their providers hear cancel and end of input.
func (c *core) closeAll() {
	for id, cn := range c.conns {
		delete(c.conns, id)
		cn.peer.close()
		if cn.live != nil {
			c.providers.retire(id, cn.live, false, c.now())
		}
	}
}

func (c *core) apply(effects []effect) {
	for _, e := range effects {
		switch e.kind {
		case show:
			c.show(e.id)
		case queue:
			if cn, ok := c.conns[e.id]; ok {
				cn.peer.send(protocol.Queued())
			}
		case hide:
			c.resolver.Hide(e.id)
		case replaced:
			c.finish(e.id, protocol.Outcome{Kind: protocol.Replaced})
		}
	}
}

func (c *core) show(id SessionID) {
	cn, ok := c.conns[id]
	if !ok || cn.phase != opened {
		return
	}
	cn.peer.send(protocol.Visible())
	switch {
	case cn.dmenu != nil:
		stage := cn.dmenu.Show()
		c.resolver.Show(Session{ID: id, Mode: cn.mode, Options: stage.Options, Entries: stage.Entries})
		if cn.ended {
			c.resolver.End(id)
		}
	case cn.apps != nil:
		c.showApps(id, cn.apps, cn.tabbed)
		if cn.launch != nil {
			c.start(id, cn) // the rites' tab
		}
	case cn.launch != nil:
		c.start(id, cn)
	}
}
