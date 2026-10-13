package xkb

// ConsumedMode selects how consumed modifiers are computed.
type ConsumedMode int

const (
	// ConsumedXKB is XKB_CONSUMED_MODE_XKB: every modifier of the key's
	// type is consumed, minus the matching entry's preserve.
	ConsumedXKB ConsumedMode = iota
	// ConsumedGTK is XKB_CONSUMED_MODE_GTK: only modifiers that change
	// the key's symbols, and of those only single-modifier entries or the
	// active one.
	ConsumedGTK
)

// Consumed is xkb_state_key_get_consumed_mods2.
func (s *State) Consumed(kc uint32, mode ConsumedMode) Mask {
	k := s.km.keys[kc]
	layout := s.Layout(kc)
	if k == nil || layout < 0 {
		return 0
	}
	var preserve, consumed Mask
	matching := s.entry(k, layout)
	if matching != nil {
		preserve = matching.presMask
	}
	g := &k.groups[layout]
	switch mode {
	case ConsumedXKB:
		consumed = g.typ.mask
	case ConsumedGTK:
		consumed = gtkConsumed(g, matching)
	}
	return consumed &^ preserve
}

func gtkConsumed(g *group, matching *typeEntry) Mask {
	t := g.typ
	noMods := 0
	for i := range t.entries {
		if e := &t.entries[i]; e.active() && e.mask == 0 {
			noMods = e.level
			break
		}
	}
	var consumed Mask
	for i := range t.entries {
		e := &t.entries[i]
		if !e.active() || sameSyms(g.level(e.level), g.level(noMods)) {
			continue
		}
		if e == matching || oneBit(e.mask) {
			consumed |= e.mask &^ e.presMask
		}
	}
	return consumed
}

func sameSyms(a, b []Keysym) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func oneBit(m Mask) bool { return m != 0 && m&(m-1) == 0 }

// ModIsConsumed is xkb_state_mod_index_is_consumed2.
func (s *State) ModIsConsumed(kc uint32, idx int, mode ConsumedMode) bool {
	if idx < 0 || idx >= len(s.km.mods) || s.km.keys[kc] == nil {
		return false
	}
	m := s.km.mods[idx].mapping
	return m != 0 && s.Consumed(kc, mode)&m == m
}
