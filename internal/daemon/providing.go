package daemon

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Provider sessions in the daemon (feat/logis §4.8; Rust
// src/daemon/providing.rs): starting a provider when its session is shown,
// turning its lines into window steps, and its timers:
//
//   - its first stage is due within startPatience, or the session fails;
//   - a toast shows for its span (3 s unless the provider says otherwise),
//     and after the provider's close the window lingers (without the
//     keyboard) until it is over;
//   - a provider retired without ever having been sent an activate or
//     submit gets grace to exit after cancel and end of input, then
//     SIGTERM, then after another grace SIGKILL. One that was sent one is
//     never signalled (an action may be under way; dismissal never kills
//     it).

const (
	// startPatience is how long a provider may take to show its first stage.
	startPatience = 5 * time.Second
	// grace is how long a provider that may be ended gets after each word.
	grace = time.Second
)

// Spawn starts a session's provider process; the session's id names what
// it hears.
type Spawn func(id SessionID, launch protocol.Launch) (provider.Running, error)

func noSpawn(SessionID, protocol.Launch) (provider.Running, error) {
	return nil, errors.New("this machine spirit starts no providers")
}

// live is a started provider in its session.
type live struct {
	external *provider.External
	// shown: the window shows its stage.
	shown bool
	// due is when its first stage is due (zero once shown).
	due time.Time
	// toast is until when its toast shows (zero for none).
	toast time.Time
}

// retiring is a retired provider that may still be ended. Once killed it
// stays until its exit is heard, without further timers.
type retiring struct {
	id      SessionID
	running provider.Running
	since   time.Time
	termed  bool
	killed  bool
}

// next is when its next signal is due (zero once killed).
func (r retiring) next() time.Time {
	switch {
	case r.killed:
		return time.Time{}
	case r.termed:
		return r.since.Add(2 * grace)
	}
	return r.since.Add(grace)
}

// providers are the daemon's providers beyond their sessions.
type providers struct {
	spawn    Spawn
	retiring []retiring
	// lingering is a session over whose toast still shows, until
	// lingerUntil (zero: none).
	lingering   SessionID
	lingerUntil time.Time
}

// retire: the session of l is over; its provider retires. Whether the
// window should linger on its toast (after the provider's close).
func (p *providers) retire(id SessionID, l *live, closed bool, now time.Time) bool {
	linger := closed && l.toast.After(now)
	if linger {
		p.lingering, p.lingerUntil = id, l.toast
	}
	if running, ok := l.external.Retire(); ok {
		p.retiring = append(p.retiring, retiring{id: id, running: running, since: now})
	}
	return linger
}

// start starts the provider of shown session id.
func (c *core) start(id SessionID, cn *conn) {
	launch := *cn.launch
	cn.launch = nil
	running, err := c.providers.spawn(id, launch)
	var external *provider.External
	if err == nil {
		if external, err = provider.NewExternal(running); err != nil {
			running.CloseInput()
		}
	}
	if err != nil {
		c.fail(id, "the provider would not wake: "+err.Error())
		return
	}
	external.Log = c.logf
	cn.live = &live{external: external, due: c.now().Add(startPatience)}
}

// heard: the provider of session id spoke or exited.
func (c *core) heard(id SessionID, h provider.Heard) {
	cn, ok := c.conns[id]
	if !ok || cn.live == nil {
		// A retired provider: it is gone now, or its words come too late.
		if h.Exited {
			c.providers.forget(id)
		}
		return
	}
	var said provider.Said
	var err error
	if h.Exited {
		cn.live.external.Exited()
		err = fmt.Errorf("it departed before its rite was done (%s)", h.Status)
	} else {
		said, err = cn.live.external.Heard(h.Message, h.Err)
	}
	switch {
	case err != nil:
		c.fail(id, "the provider spoke heresy: "+err.Error())
	case said.Close:
		c.finish(id, protocol.Outcome{Kind: protocol.Closed})
	default:
		for _, step := range said.Steps {
			c.step(id, step)
		}
	}
}

// step is one step of a provider for its window: the first stage shows it.
func (c *core) step(id SessionID, step provider.Step) {
	cn, ok := c.conns[id]
	if !ok || cn.live == nil {
		return
	}
	l := cn.live
	switch {
	case step.Kind == provider.StepStage && !l.shown:
		l.shown, l.due = true, time.Time{}
		c.resolver.Show(Session{ID: id, Mode: cn.mode, Options: step.Options, Entries: step.Entries})
	case step.Kind == provider.StepToast && !l.shown:
		c.logf("a provider's toast before its window: " + step.Notice.Text)
	case !l.shown:
	case step.Kind == provider.StepToast:
		l.toast = c.now().Add(step.Span)
		c.resolver.Step(id, step)
	default:
		c.resolver.Step(id, step)
	}
}

func (p *providers) forget(id SessionID) {
	for i, r := range p.retiring {
		if r.id == id {
			p.retiring = append(p.retiring[:i], p.retiring[i+1:]...)
			return
		}
	}
}

// deadline is when the next timer is due, if any.
func (c *core) deadline() (time.Time, bool) {
	var next time.Time
	consider := func(t time.Time) {
		if !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	for _, cn := range c.conns {
		if cn.live != nil {
			consider(cn.live.due)
			consider(cn.live.toast)
		}
	}
	consider(c.providers.lingerUntil)
	c.appsDeadline(consider)
	for _, r := range c.providers.retiring {
		consider(r.next())
	}
	return next, !next.IsZero()
}

// tick runs the timers due at now.
func (c *core) tick(now time.Time) {
	c.appsTick(now)
	var late, spent []SessionID
	for id, cn := range c.conns {
		l := cn.live
		if l == nil {
			continue
		}
		if !l.due.IsZero() && !now.Before(l.due) {
			late = append(late, id)
		}
		if !l.toast.IsZero() && !now.Before(l.toast) {
			l.toast = time.Time{}
			spent = append(spent, id)
		}
	}
	for _, id := range spent {
		if lingerer, ok := c.resolver.(Lingerer); ok {
			lingerer.Untoast(id)
		}
	}
	for _, id := range late {
		c.fail(id, fmt.Sprintf("the provider showed nothing within %d s", int(startPatience/time.Second)))
	}
	if p := &c.providers; !p.lingerUntil.IsZero() && !now.Before(p.lingerUntil) {
		p.lingerUntil = time.Time{}
		c.resolver.Hide(p.lingering)
	}
	for i := range c.providers.retiring {
		r := &c.providers.retiring[i]
		switch {
		case r.killed:
		case !now.Before(r.since.Add(2 * grace)):
			r.running.Signal(syscall.SIGKILL)
			r.killed = true
		case !now.Before(r.since.Add(grace)) && !r.termed:
			r.running.Signal(syscall.SIGTERM)
			r.termed = true
		}
	}
}
