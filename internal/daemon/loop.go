package daemon

import (
	"bufio"
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/unixsock"
)

// writeTimeout: a client that stops reading must not stall the daemon.
const writeTimeout = 2 * time.Second

// event is something the daemon's goroutine does with its state.
type event func(*loop)

// stop is why the daemon stops: its exit status, and what goes to its log.
type stop struct {
	status  int
	message string
}

// loop is the daemon's event loop. Its goroutine alone touches core.
// Socket goroutines hand it events through a channel (so that a fast client
// waits for the daemon); the Host's go to an unbounded queue, so that a
// resolver may answer from within a call the loop made.
type loop struct {
	core    *core
	stopped *stop
	// live is the config in force; it decides the idle timeout.
	live *config.Live
	// themes is the theme in force (with live's config).
	themes *theme.Live
	// logf writes one line to the daemon's log.
	logf func(message string)

	sockets chan event
	done    chan struct{}

	mu     sync.Mutex
	queue  []event
	wake   chan struct{}
	closed bool
}

func newLoop() *loop {
	return &loop{
		sockets: make(chan event, 64),
		done:    make(chan struct{}),
		wake:    make(chan struct{}, 1),
	}
}

// fromSocket hands the loop an event; false once the daemon has stopped.
func (l *loop) fromSocket(e event) bool {
	select {
	case l.sockets <- e:
		return true
	case <-l.done:
		return false
	}
}

// post queues an event without waiting; dropped once the daemon has stopped.
func (l *loop) post(e event) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.queue = append(l.queue, e)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (l *loop) drain() []event {
	l.mu.Lock()
	defer l.mu.Unlock()
	q := l.queue
	l.queue = nil
	return q
}

// shut stops accepting events; senders return at once from then on.
func (l *loop) shut() {
	l.mu.Lock()
	l.closed = true
	l.queue = nil
	l.mu.Unlock()
	close(l.done)
}

// serve runs events until the daemon must stop: a Host's Exit, the idle
// timeout in force (when positive) with no client connected, or ctx's end.
// Between events it sleeps; only the idle timeout sets a timer.
func (l *loop) serve(ctx context.Context) stop {
	idleSince := time.Now()
	for l.stopped == nil {
		wasIdle := l.core.idle()
		if s, done := l.next(ctx, l.live.IdleTimeout(), idleSince); done {
			return s
		}
		// Idleness is measured from the moment the last client left.
		if !wasIdle || !l.core.idle() {
			idleSince = time.Now()
		}
	}
	return *l.stopped
}

// next waits for the next event and runs it; done when the daemon is
// dismissed or the idle timeout ran out first.
func (l *loop) next(ctx context.Context, idleTimeout time.Duration, idleSince time.Time) (stop, bool) {
	var expired <-chan time.Time
	if idleTimeout > 0 && l.core.idle() {
		timer := time.NewTimer(time.Until(idleSince.Add(idleTimeout)))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-ctx.Done():
		return stop{0, "the machine spirit is dismissed and returns to rest"}, true
	case <-expired:
		return stop{0, "no summons came for " + idleTimeout.String() +
			"; the machine spirit returns to rest"}, true
	case e := <-l.sockets:
		e(l)
	case <-l.wake:
		for _, e := range l.drain() {
			if l.stopped == nil {
				e(l)
			}
		}
	}
	return stop{}, false
}

// accept turns each connection into events, with a reader goroutine each.
func (l *loop) accept(ln *unixsock.Listener) {
	var next SessionID
	for {
		c, err := ln.Accept()
		if errors.Is(err, os.ErrClosed) {
			l.fromSocket(func(l *loop) {
				l.stopped = &stop{2, "the machine spirit can no longer hear its socket"}
			})
			return
		}
		if err != nil {
			// Out of descriptors or similar: do not spin.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		next++
		id, p := next, &streamPeer{c}
		if !l.fromSocket(func(l *loop) { l.core.connect(id, p) }) {
			_ = c.Close()
			return
		}
		go l.read(id, c)
	}
}

// read hands each request of connection id to the loop, then its hang-up.
// The core alone closes the connection (its peer), so that a reply to the
// last request still goes out.
func (l *loop) read(id SessionID, c *os.File) {
	r := bufio.NewReader(c)
	for {
		var request protocol.Request
		err := protocol.ReadRequest(r, &request)
		var tooLarge *protocol.TooLargeError
		var malformed *protocol.MalformedError
		if err != nil && !errors.As(err, &tooLarge) && !errors.As(err, &malformed) {
			break // end of stream, or the connection broke
		}
		if !l.fromSocket(func(l *loop) { l.core.receive(id, request, err) }) || err != nil {
			break
		}
	}
	l.fromSocket(func(l *loop) { l.core.disconnect(id) })
}

// streamPeer answers a client over its connection.
type streamPeer struct{ c *os.File }

func (p *streamPeer) send(reply protocol.Reply) {
	// A failed write surfaces as the reader's hang-up.
	_ = p.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = protocol.WriteFrame(p.c, reply)
}

// close ends the connection; its reader then sees it closed and stops.
func (p *streamPeer) close() { _ = p.c.Close() }

// loopHost is the Host of a running daemon.
type loopHost struct{ l *loop }

func (h loopHost) Answer(id SessionID, answer Answer) {
	h.l.post(func(l *loop) { l.core.answered(id, answer) })
}

func (h loopHost) Fail(id SessionID, message string) {
	h.l.post(func(l *loop) { l.core.failed(id, message) })
}

func (h loopHost) Exit(status int, message string) {
	h.l.post(func(l *loop) { l.stopped = &stop{status, message} })
}
