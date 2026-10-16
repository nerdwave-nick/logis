package xkb

import "unicode/utf8"

// State mirrors the compositor's keyboard state as wl_keyboard.modifiers
// reports it (xkb_state_update_mask); a client never feeds key events.
type State struct {
	km    *Keymap
	mods  Mask
	group int
}

// NewState is a state with no modifier and the first layout active.
func NewState(km *Keymap) *State { return &State{km: km} }

// Keymap is the keymap the state is of.
func (s *State) Keymap() *Keymap { return s.km }

func (s *State) canonical(m Mask) Mask {
	return m&s.km.canonical | s.km.effective(m&^s.km.canonical)
}

// UpdateMask applies the masks and layouts of wl_keyboard.modifiers (a
// compositor sends the effective layout as locked, base and latched 0).
func (s *State) UpdateMask(base, latched, locked Mask, baseGroup, latchedGroup, lockedGroup uint32) {
	s.mods = s.canonical(base) | s.canonical(latched) | s.canonical(locked)
	n := s.km.numGroups
	lockedG := wrapGroup(int(int32(lockedGroup)), n, rangeWrap, 0)
	if lockedG < 0 {
		lockedG = 0
	}
	s.group = wrapGroup(int(int32(baseGroup))+int(int32(latchedGroup))+lockedG, n, rangeWrap, 0)
	if s.group < 0 {
		s.group = 0
	}
}

// Mods is the effective modifier mask (xkb_state_serialize_mods EFFECTIVE).
func (s *State) Mods() Mask { return s.mods }

// ModIsActive is xkb_state_mod_index_is_active for the effective mods.
func (s *State) ModIsActive(idx int) bool {
	if idx < 0 || idx >= len(s.km.mods) {
		return false
	}
	m := s.km.mods[idx].mapping
	return m != 0 && s.mods&m == m
}

// Layout is the layout key kc uses now, or -1.
func (s *State) Layout(kc uint32) int {
	k := s.km.keys[kc]
	if k == nil {
		return -1
	}
	return wrapGroup(s.group, len(k.groups), k.oorAction, k.oorNumber)
}

func (s *State) entry(k *key, layout int) *typeEntry {
	t := k.groups[layout].typ
	active := s.mods & t.mask
	for i := range t.entries {
		e := &t.entries[i]
		if e.active() && e.mask == active {
			return e
		}
	}
	return nil
}

// Level is the level key kc is at in layout, or -1.
func (s *State) Level(kc uint32, layout int) int {
	k := s.km.keys[kc]
	if k == nil || layout < 0 || layout >= len(k.groups) {
		return -1
	}
	if e := s.entry(k, layout); e != nil {
		return e.level
	}
	return 0
}

func (s *State) rawSyms(kc uint32) []Keysym {
	layout := s.Layout(kc)
	if layout < 0 {
		return nil
	}
	return s.km.SymsByLevel(kc, layout, s.Level(kc, layout))
}

// Syms is xkb_state_key_get_syms: the key's keysyms now, with the Caps
// Lock transformation applied.
func (s *State) Syms(kc uint32) []Keysym {
	syms := s.rawSyms(kc)
	if len(syms) == 0 || !s.capsTransform(kc) {
		return syms
	}
	up := make([]Keysym, len(syms))
	for i, ks := range syms {
		up[i] = ks.ToUpper()
	}
	return up
}

// OneSym is xkb_state_key_get_one_sym.
func (s *State) OneSym(kc uint32) Keysym {
	syms := s.Syms(kc)
	if len(syms) != 1 {
		return NoSymbol
	}
	return syms[0]
}

func (s *State) capsTransform(kc uint32) bool {
	return s.ModIsActive(ModLock) && !s.ModIsConsumed(kc, ModLock, ConsumedXKB)
}

func (s *State) ctrlTransform(kc uint32) bool {
	return s.ModIsActive(ModControl) && !s.ModIsConsumed(kc, ModControl, ConsumedXKB)
}

// oneSymForString is libxkbcommon's get_one_sym_for_string.
func (s *State) oneSymForString(kc uint32) Keysym {
	layout := s.Layout(kc)
	k := s.km.keys[kc]
	if layout < 0 || k == nil {
		return NoSymbol
	}
	syms := s.km.SymsByLevel(kc, layout, s.Level(kc, layout))
	if len(syms) != 1 {
		return NoSymbol
	}
	sym := syms[0]
	if s.ctrlTransform(kc) && sym > 127 {
		for i := range k.groups {
			alt := s.km.SymsByLevel(kc, i, s.Level(kc, i))
			if len(alt) == 1 && alt[0] <= 127 {
				sym = alt[0]
				break
			}
		}
	}
	if s.capsTransform(kc) {
		sym = sym.ToUpper()
	}
	return sym
}

// UTF8 is xkb_state_key_get_utf8: the text key kc types now, with the
// Caps Lock and Control transformations (Ctrl+G is "\a").
func (s *State) UTF8(kc uint32) string {
	var syms []Keysym
	if sym := s.oneSymForString(kc); sym != NoSymbol {
		syms = []Keysym{sym}
	} else {
		syms = s.Syms(kc)
	}
	var b []byte
	for _, ks := range syms {
		u := ks.UTF32()
		if u <= 0 {
			return ""
		}
		b = utf8.AppendRune(b, u)
	}
	if !utf8.Valid(b) {
		return ""
	}
	if len(b) == 1 && b[0] <= 127 && s.ctrlTransform(kc) {
		b[0] = toControl(b[0])
	}
	return string(b)
}

// toControl is XkbToControl from libX11.
func toControl(c byte) byte {
	switch {
	case c >= '@' && c < 0x7f || c == ' ':
		return c & 0x1f
	case c == '2':
		return 0
	case c >= '3' && c <= '7':
		return c - ('3' - 0x1b)
	case c == '8':
		return 0x7f
	case c == '/':
		return '_' & 0x1f
	}
	return c
}
