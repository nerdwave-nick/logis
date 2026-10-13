package xkb

import "strings"

// symKey is a key statement of xkb_symbols, before types are resolved.
type symKey struct {
	k       *key
	groups  []rawGroup
	defType string
}

type rawGroup struct {
	typ     string
	levels  [][]Keysym
	actions bool
	defined bool
}

type modmapEntry struct {
	real  int
	key   string
	sym   Keysym
	bySym bool
}

func (c *compiler) symbolsStmt(s *stmt) error {
	switch h := s.head[0].text; {
	case h == "virtual_modifiers":
		return c.declareVmods(s.items)
	case strings.EqualFold(h, "name") && s.index != nil && s.value != nil:
		g, err := groupNum(s.index)
		if err != nil {
			return err
		}
		for len(c.km.layoutNames) <= g {
			c.km.layoutNames = append(c.km.layoutNames, "")
		}
		c.km.layoutNames[g] = s.value.text
	case h == "key" && len(s.head) >= 2:
		k, ok := c.keyByName(s.head[1].text)
		if !ok {
			return nil // an unknown key: ignored, as libxkbcommon does
		}
		return c.keyStmt(k, s.items)
	case h == "modifier_map" && len(s.head) >= 2:
		real := c.modIndex(s.head[1].text)
		if real < 0 || real >= 8 {
			return errorf(0, "modifier_map on %q", s.head[1].text)
		}
		for _, it := range s.items {
			switch it.kind {
			case "key":
				c.modmaps = append(c.modmaps, modmapEntry{real: real, key: it.text})
			case "ident", "num":
				c.modmaps = append(c.modmaps, modmapEntry{real: real, sym: keysymText(it.kind == "num", it.text), bySym: true})
			}
		}
	}
	return nil
}

func (c *compiler) keyStmt(k *key, items []*expr) error {
	sk := &symKey{k: k}
	grp := func(i int) *rawGroup {
		for len(sk.groups) <= i {
			sk.groups = append(sk.groups, rawGroup{})
		}
		return &sk.groups[i]
	}
	implicit := 0
	for _, it := range items {
		switch it.kind {
		case "list":
			if implicit >= maxGroups {
				return errorf(0, "key <%s> has more than %d groups", k.name, maxGroups)
			}
			g := grp(implicit)
			g.levels = levelsOf(it)
			g.defined = true
			implicit++
		case "assign":
			if err := c.keyField(k, sk, grp, it); err != nil {
				return err
			}
		}
	}
	if into, ok := c.symKeyOf[k]; ok {
		into.merge(sk)
		return nil
	}
	c.symKeyOf[k] = sk
	c.symKeys = append(c.symKeys, sk)
	return nil
}

// merge folds a later statement for the same key into the first, as
// libxkbcommon's MergeKeys and MergeGroups do in override mode: an
// explicit type wins, a level with keysyms replaces the one before, and
// extra levels are added. (Repeat, vmods and group handling were already
// set on the key, the later statement winning.)
func (into *symKey) merge(from *symKey) {
	if from.defType != "" {
		into.defType = from.defType
	}
	for i, fg := range from.groups {
		for len(into.groups) <= i {
			into.groups = append(into.groups, rawGroup{})
		}
		ig := &into.groups[i]
		if fg.typ != "" {
			ig.typ = fg.typ
		}
		ig.actions = ig.actions || fg.actions
		switch {
		case !fg.defined:
		case !ig.defined:
			ig.levels, ig.defined = fg.levels, true
		default:
			for l, syms := range fg.levels {
				switch {
				case l >= len(ig.levels):
					ig.levels = append(ig.levels, syms)
				case len(syms) > 0:
					ig.levels[l] = syms
				}
			}
		}
	}
}

func (c *compiler) keyField(k *key, sk *symKey, grp func(int) *rawGroup, it *expr) error {
	field, idx := it.left, (*expr)(nil)
	if field.kind == "index" {
		field, idx = field.left, field.right
	}
	gi := 0
	if idx != nil {
		var err error
		if gi, err = groupNum(idx); err != nil {
			return err
		}
	}
	switch strings.ToLower(field.text) {
	case "type":
		if idx == nil {
			sk.defType = it.right.text
		} else {
			grp(gi).typ = it.right.text
		}
	case "symbols":
		g := grp(gi)
		g.levels = levelsOf(it.right)
		g.defined = true
	case "actions":
		g := grp(gi)
		g.actions = true
		if !g.defined && it.right.kind == "list" {
			// The level count follows the actions when no symbols are given.
			g.levels = make([][]Keysym, len(it.right.args))
			g.defined = true
		}
	case "repeat", "repeats", "autorepeat":
		k.repeats = boolValue(it.right)
		k.explicitRepeat = true
	case "virtualmods", "vmods", "virtualmodifiers":
		m, err := c.mask(it.right)
		if err != nil {
			return err
		}
		k.vmodmap = m
		k.explicitVmodmap = true
	case "groupswrap", "wrapgroups":
		k.oorAction = rangeWrap
	case "groupsclamp", "clampgroups":
		k.oorAction = rangeSaturate
	case "groupsredirect", "redirectgroups":
		g, err := groupNum(it.right)
		if err != nil {
			return err
		}
		k.oorAction, k.oorNumber = rangeRedirect, g
	}
	return nil
}

// levelsOf reads a symbols list: one entry per level, each a keysym or a
// {braced} list of them; NoSymbol entries leave a level empty.
func levelsOf(e *expr) [][]Keysym {
	out := make([][]Keysym, 0, len(e.args))
	for _, l := range e.args {
		var syms []Keysym
		one := func(x *expr) {
			if s := keysymText(x.kind == "num", x.text); s != NoSymbol {
				syms = append(syms, s)
			}
		}
		if l.kind == "brace" {
			for _, x := range l.args {
				one(x)
			}
		} else {
			one(l)
		}
		out = append(out, syms)
	}
	return out
}

// automaticType is libxkbcommon's FindAutomaticType: the type a group
// without an explicit one gets from its width and its first keysyms.
func automaticType(levels [][]Keysym) string {
	sym := func(i int) Keysym {
		if i >= len(levels) || len(levels[i]) == 0 {
			return NoSymbol
		}
		return levels[i][0]
	}
	w := len(levels)
	if w <= 1 {
		return "ONE_LEVEL"
	}
	s0, s1 := sym(0), sym(1)
	if w == 2 {
		if s0.IsLower() && s1.IsUpperOrTitle() {
			return "ALPHABETIC"
		}
		if s0.IsKeypad() || s1.IsKeypad() {
			return "KEYPAD"
		}
		return "TWO_LEVEL"
	}
	if w <= 4 {
		if s0.IsLower() && s1.IsUpperOrTitle() {
			s2, s3 := sym(2), NoSymbol
			if w == 4 {
				s3 = sym(3)
			}
			if s2.IsLower() && s3.IsUpperOrTitle() {
				return "FOUR_LEVEL_ALPHABETIC"
			}
			return "FOUR_LEVEL_SEMIALPHABETIC"
		}
		if s0.IsKeypad() || s1.IsKeypad() {
			return "FOUR_LEVEL_KEYPAD"
		}
		return "FOUR_LEVEL"
	}
	return ""
}
