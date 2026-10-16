package xkb

// Mask is a modifier mask: bits 0-7 are the real modifiers (Shift, Lock,
// Control, Mod1-Mod5), higher bits the keymap's virtual modifiers.
type Mask uint32

const realMask Mask = 0xff

// Real modifier indices, fixed in every keymap.
const (
	ModShift   = 0
	ModLock    = 1
	ModControl = 2
	ModMod1    = 3
	ModMod2    = 4
	ModMod4    = 6
)

var realModNames = []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}

// Group overflow handling of a key (out_of_range_group_action).
const (
	rangeWrap = iota
	rangeSaturate
	rangeRedirect
)

type mod struct {
	name     string
	mapping  Mask
	explicit bool
}

type typeEntry struct {
	mods, mask         Mask // declared and effective
	preserve, presMask Mask
	level              int
}

func (e *typeEntry) active() bool { return e.mods == 0 || e.mask != 0 }

type keyType struct {
	name    string
	mods    Mask // declared
	mask    Mask // effective
	levels  int
	entries []typeEntry
}

type group struct {
	typ *keyType
	// levels holds the declared levels only, at most typ.levels of them;
	// the levels after it are empty.
	levels          [][]Keysym
	explicitActions bool
}

// level is the keysyms at level l (nil when the key declares none).
func (g *group) level(l int) []Keysym {
	if l < 0 || l >= len(g.levels) {
		return nil
	}
	return g.levels[l]
}

type key struct {
	code            uint32
	name            string
	groups          []group
	repeats         bool
	explicitRepeat  bool
	vmodmap         Mask
	explicitVmodmap bool
	modmap          Mask
	oorAction       int
	oorNumber       int
}

// Keymap is a compiled keymap. It is immutable once parsed, so a State
// can be made from it at any time.
type Keymap struct {
	mods        []mod
	types       []*keyType
	keys        map[uint32]*key
	minKey      uint32
	maxKey      uint32
	numGroups   int
	canonical   Mask
	layoutNames []string
}

func (km *Keymap) effective(m Mask) Mask {
	out := m & realMask
	for i := 8; i < len(km.mods); i++ {
		if m&(Mask(1)<<i) != 0 {
			out |= km.mods[i].mapping
		}
	}
	return out
}

func (km *Keymap) hasUnboundVmods(m Mask) bool {
	for i := 8; i < len(km.mods); i++ {
		if m&(Mask(1)<<i) != 0 && km.mods[i].mapping == 0 {
			return true
		}
	}
	return false
}

// ModIndex is the index of the named modifier, or -1.
func (km *Keymap) ModIndex(name string) int {
	for i, m := range km.mods {
		if m.name == name {
			return i
		}
	}
	return -1
}

// NumMods is the number of modifiers (real and virtual).
func (km *Keymap) NumMods() int { return len(km.mods) }

// NumLayouts is the number of layouts (groups).
func (km *Keymap) NumLayouts() int { return km.numGroups }

// LayoutName is the name of layout i, if the keymap names it.
func (km *Keymap) LayoutName(i int) string {
	if i >= 0 && i < len(km.layoutNames) {
		return km.layoutNames[i]
	}
	return ""
}

// Repeats says whether key kc (an xkb keycode) repeats.
func (km *Keymap) Repeats(kc uint32) bool {
	k := km.keys[kc]
	return k != nil && k.repeats
}

// SymsByLevel are the keysyms of key kc in layout and level, as the
// keymap defines them (no state applied).
func (km *Keymap) SymsByLevel(kc uint32, layout, level int) []Keysym {
	k := km.keys[kc]
	if k == nil {
		return nil
	}
	layout = wrapGroup(layout, len(k.groups), k.oorAction, k.oorNumber)
	if layout < 0 {
		return nil
	}
	return k.groups[layout].level(level)
}

// KeyRange is the smallest and largest keycode the keymap declares.
func (km *Keymap) KeyRange() (uint32, uint32) { return km.minKey, km.maxKey }

func wrapGroup(g, n, action, number int) int {
	if n == 0 {
		return -1
	}
	if g >= 0 && g < n {
		return g
	}
	switch action {
	case rangeRedirect:
		if number >= n {
			return 0
		}
		return number
	case rangeSaturate:
		if g < 0 {
			return 0
		}
		return n - 1
	}
	if g < 0 {
		r := g % n
		if r == 0 {
			return 0
		}
		return r + n
	}
	return g % n
}
