package daemon

// SessionID is a session's identity within one daemon run (its
// connection's number).
type SessionID uint64

// effectKind is what the daemon must do after a policy decision.
type effectKind int

const (
	// show the session; it is now the visible one.
	show effectKind = iota
	// queue: the session waits behind the visible one.
	queue
	// hide: stop showing the session.
	hide
	// replaced: the session lost its place to an interactive one and is over.
	replaced
)

type effect struct {
	kind effectKind
	id   SessionID
}

// sessions is the concurrency policy (feat/logis §4.1): at most one session
// is visible. While one is, a new dmenu session queues (first in, first
// shown) and a new interactive session replaces it. Pure bookkeeping; the
// core carries out the effects, in order.
type sessions struct {
	shown   SessionID
	showing bool
	queued  []SessionID
}

// open: a new session arrives.
func (s *sessions) open(id SessionID, interactive bool) []effect {
	switch {
	case !s.showing:
		s.shown, s.showing = id, true
		return []effect{{show, id}}
	case !interactive:
		s.queued = append(s.queued, id)
		return []effect{{queue, id}}
	default:
		old := s.shown
		s.shown = id
		return []effect{{hide, old}, {replaced, old}, {show, id}}
	}
}

// close: a session ends (result, cancellation or disconnect) and leaves the
// policy.
func (s *sessions) close(id SessionID) []effect {
	if !s.showing || s.shown != id {
		for i, queued := range s.queued {
			if queued == id {
				s.queued = append(s.queued[:i], s.queued[i+1:]...)
				break
			}
		}
		return nil
	}
	effects := []effect{{hide, id}}
	if len(s.queued) == 0 {
		s.showing = false
		return effects
	}
	s.shown, s.queued = s.queued[0], s.queued[1:]
	return append(effects, effect{show, s.shown})
}

// visible is the session currently shown, if any.
func (s *sessions) visible() (SessionID, bool) { return s.shown, s.showing }
