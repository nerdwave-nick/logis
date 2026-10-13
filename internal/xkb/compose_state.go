package xkb

// ComposeStatus is the state of a sequence.
type ComposeStatus int

// The Compose statuses, as in libxkbcommon.
const (
	ComposeNothing ComposeStatus = iota
	ComposeComposing
	ComposeComposed
	ComposeCancelled
)

// ComposeState is a sequence in progress.
type ComposeState struct {
	t          *ComposeTable
	prev, node *composeNode
}

// NewState starts with no sequence.
func (t *ComposeTable) NewState() *ComposeState { return &ComposeState{t: t} }

// Reset forgets a sequence in progress.
func (s *ComposeState) Reset() { s.prev, s.node = nil, nil }

// Feed feeds a keysym. Modifier keysyms are ignored (false), so a Shift
// pressed inside a sequence never cancels it.
func (s *ComposeState) Feed(ks Keysym) bool {
	if ks.IsModifier() {
		return false
	}
	from := s.node
	if from == nil || from.leaf {
		from = s.t.root
	}
	s.prev = s.node
	s.node = from.next[ks]
	return true
}

// Status is where the sequence stands.
func (s *ComposeState) Status() ComposeStatus {
	switch {
	case s.node == nil && s.prev != nil && !s.prev.leaf:
		return ComposeCancelled
	case s.node == nil:
		return ComposeNothing
	case !s.node.leaf:
		return ComposeComposing
	}
	return ComposeComposed
}

// UTF8 is the composed text (the keysym's text when the sequence has
// only a keysym).
func (s *ComposeState) UTF8() string {
	if s.node == nil || !s.node.leaf {
		return ""
	}
	if !s.node.hasText && s.node.keysym != NoSymbol {
		if u := s.node.keysym.UTF32(); u > 0 {
			return string(u)
		}
	}
	return s.node.text
}

// Keysym is the composed keysym, if the sequence names one.
func (s *ComposeState) Keysym() Keysym {
	if s.node == nil || !s.node.leaf {
		return NoSymbol
	}
	return s.node.keysym
}
