package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/nerdwave-nick/logis/internal/protocol"
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

// conn is one client connection and the session it may carry.
type conn struct {
	peer    peer
	phase   phase
	mode    protocol.Mode
	options json.RawMessage
	// items are a dmenu session's items so far, in arrival order.
	items []protocol.Item
	// ended: the dmenu client sent all its items.
	ended bool
}

// core is the daemon's protocol state machine: connections, their sessions,
// the session policy and the resolver. It does no I/O of its own and runs on
// the daemon's goroutine only; it answers through each connection's peer.
type core struct {
	resolver Resolver
	sessions sessions
	conns    map[SessionID]*conn
	pid      int
}

func newCore(resolver Resolver, pid int) *core {
	return &core{resolver: resolver, conns: map[SessionID]*conn{}, pid: pid}
}

// idle: no client is connected.
func (c *core) idle() bool { return len(c.conns) == 0 }

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
	}
	if err := c.resolver.Accepts(request.Mode); err != nil {
		c.fail(id, err.Error())
		return
	}
	cn.phase, cn.mode, cn.options = opened, request.Mode, request.Options
	c.apply(c.sessions.open(id, request.Mode.Interactive()))
}

func (c *core) items(id SessionID, cn *conn, items []protocol.Item) {
	switch {
	case cn.mode != protocol.ModeDmenu:
		c.fail(id, "only dmenu sessions take items")
	case cn.ended:
		c.fail(id, "items arrived after end")
	default:
		cn.items = append(cn.items, items...)
		if visible, ok := c.sessions.visible(); ok && visible == id && len(items) > 0 {
			c.resolver.Append(id, items)
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
		c.apply(c.sessions.close(id))
	}
}

// answered: the user answered the visible session.
func (c *core) answered(id SessionID, answer Answer) {
	cn, ok := c.conns[id]
	if visible, shown := c.sessions.visible(); !ok || !shown || visible != id {
		return
	}
	switch {
	case answer.Kind == AnswerDismissed:
		c.finish(id, protocol.Outcome{Kind: protocol.Dismissed})
	case cn.mode != protocol.ModeDmenu:
		// What the user chose in an apps, servitor or provider session is
		// for those features' tickets to carry out.
	case answer.Kind == AnswerTyped:
		c.finish(id, protocol.Outcome{Kind: protocol.Typed, Text: answer.Text})
	case answer.Index >= 0 && answer.Index < len(cn.items):
		c.finish(id, protocol.Outcome{Kind: protocol.Selected, Index: uint64(answer.Index),
			Item: cn.items[answer.Index]})
	}
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
	c.end(id, protocol.Result(outcome))
}

// fail ends a connection with an error, cancelling any session it had.
func (c *core) fail(id SessionID, message string) {
	c.end(id, protocol.Error(message))
}

func (c *core) end(id SessionID, last protocol.Reply) {
	cn, ok := c.conns[id]
	if !ok {
		return
	}
	delete(c.conns, id)
	cn.peer.send(last)
	cn.peer.close()
	c.apply(c.sessions.close(id))
}

// closeAll ends every connection without a word, as the daemon's exit does.
func (c *core) closeAll() {
	for id, cn := range c.conns {
		delete(c.conns, id)
		cn.peer.close()
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
	c.resolver.Show(Session{ID: id, Mode: cn.mode, Options: cn.options, Items: cn.items})
	if cn.ended {
		c.resolver.End(id)
	}
}
