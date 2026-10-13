package xkb

import "sort"

// finish derives what the serialised text leaves implicit: key types
// (FindAutomaticType), modifier maps, the interprets' virtual modifiers
// and repeat flags, the virtual modifiers' real mappings and the types'
// effective masks.
func (c *compiler) finish() error {
	km := c.km
	if len(km.types) == 0 {
		km.types = append(km.types, &keyType{name: "ONE_LEVEL", levels: 1})
	}
	for _, sk := range c.symKeys {
		c.resolveGroups(sk)
	}
	c.applyModmaps()
	byCode := c.keysByCode()
	interps := sortInterps(c.interps)
	for _, k := range byCode {
		err := interps.apply(k)
		c.work = interps.work
		if err != nil {
			return err
		}
	}
	for _, k := range byCode {
		for i := 8; i < len(km.mods); i++ {
			if k.vmodmap&(Mask(1)<<i) != 0 {
				km.mods[i].mapping |= k.modmap
			}
		}
	}
	km.canonical = realMask
	for i := 8; i < len(km.mods); i++ {
		km.canonical |= km.mods[i].mapping
	}
	for _, t := range km.types {
		t.mask = km.effective(t.mods)
		for i := range t.entries {
			e := &t.entries[i]
			if km.hasUnboundVmods(e.mods) {
				e.mask = 0
				continue
			}
			e.mask = km.effective(e.mods)
			e.presMask = km.effective(e.preserve)
		}
	}
	for _, k := range byCode {
		km.numGroups = max(km.numGroups, len(k.groups))
	}
	if len(byCode) > 0 {
		if !c.minSet {
			km.minKey = byCode[0].code
		}
		if !c.maxSet {
			km.maxKey = byCode[len(byCode)-1].code
		}
	}
	return nil
}

// resolveGroups gives each group of a key statement its type, explicit
// or automatic, and keeps at most the type's levels.
func (c *compiler) resolveGroups(sk *symKey) {
	k := sk.k
	k.groups = nil
	last := -1
	for i, g := range sk.groups {
		if len(g.levels) > 0 || g.typ != "" || sk.defType != "" {
			last = i
		}
	}
	for i := 0; i <= last; i++ {
		g := sk.groups[i]
		name := g.typ
		if name == "" {
			name = sk.defType
		}
		if name == "" {
			name = automaticType(g.levels)
		}
		t, ok := c.typeByName[name]
		if !ok {
			t = c.km.types[0]
		}
		levels := g.levels
		if len(levels) > t.levels {
			levels = levels[:t.levels]
		}
		k.groups = append(k.groups, group{typ: t, levels: levels, explicitActions: g.actions})
	}
}

func (c *compiler) keysByCode() []*key {
	out := make([]*key, 0, len(c.km.keys))
	for _, k := range c.km.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].code < out[j].code })
	return out
}

// applyModmaps sets each key's real modifier map. An entry by keysym
// names the key that has the keysym in the lowest group, then the lowest
// level, then the lowest keycode (libxkbcommon's FindKeyForSymbol).
func (c *compiler) applyModmaps() {
	var bySym map[Keysym]*key
	for _, m := range c.modmaps {
		if !m.bySym {
			if k, ok := c.keyByName(m.key); ok {
				k.modmap |= Mask(1) << m.real
			}
			continue
		}
		if bySym == nil {
			bySym = c.firstKeyForSyms()
		}
		if k := bySym[m.sym]; k != nil {
			k.modmap |= Mask(1) << m.real
		}
	}
}

func (c *compiler) firstKeyForSyms() map[Keysym]*key {
	type place struct{ group, level int }
	best := map[Keysym]*key{}
	at := map[Keysym]place{}
	for _, k := range c.keysByCode() {
		for gi, g := range k.groups {
			for li, syms := range g.levels {
				if len(syms) != 1 {
					continue
				}
				p, s := place{gi, li}, syms[0]
				if old, seen := at[s]; seen && (old.group < gi || old.group == gi && old.level <= li) {
					continue
				}
				best[s], at[s] = k, p
			}
		}
	}
	return best
}
