package xkb

import "strings"

func (c *compiler) typeStmt(s *stmt) error {
	switch s.head[0].text {
	case "virtual_modifiers":
		return c.declareVmods(s.items)
	case "type":
		if len(s.head) < 2 {
			return errorf(0, "a type without a name")
		}
		t := &keyType{name: s.head[1].text, levels: 1}
		for _, f := range s.body {
			if err := c.typeField(t, f); err != nil {
				return err
			}
		}
		for i := range t.entries {
			t.entries[i].mods &= t.mods
			t.entries[i].preserve &= t.entries[i].mods
		}
		if _, dup := c.typeByName[t.name]; !dup {
			if len(c.km.types) >= maxTypes {
				return errorf(0, "more than %d key types", maxTypes)
			}
			c.km.types = append(c.km.types, t)
		} else {
			for i, old := range c.km.types {
				if old.name == t.name {
					c.km.types[i] = t
				}
			}
		}
		c.typeByName[t.name] = t
	}
	return nil
}

func (c *compiler) typeField(t *keyType, f *stmt) error {
	if len(f.head) == 0 || f.value == nil {
		return nil
	}
	switch strings.ToLower(f.head[0].text) {
	case "modifiers":
		m, err := c.mask(f.value)
		if err != nil {
			return err
		}
		t.mods = m
	case "map":
		m, err := c.mask(f.index)
		if err != nil {
			return err
		}
		lvl, err := levelNum(f.value)
		if err != nil {
			return err
		}
		e, err := t.entry(m)
		if err != nil {
			return err
		}
		e.level = lvl
		t.levels = max(t.levels, lvl+1)
	case "preserve":
		m, err := c.mask(f.index)
		if err != nil {
			return err
		}
		p, err := c.mask(f.value)
		if err != nil {
			return err
		}
		e, err := t.entry(m)
		if err != nil {
			return err
		}
		e.preserve = p
	}
	return nil
}

func (t *keyType) entry(m Mask) (*typeEntry, error) {
	for i := range t.entries {
		if t.entries[i].mods == m {
			return &t.entries[i], nil
		}
	}
	if len(t.entries) >= maxTypeEntries {
		return nil, errorf(0, "type %q has more than %d entries", t.name, maxTypeEntries)
	}
	t.entries = append(t.entries, typeEntry{mods: m})
	return &t.entries[len(t.entries)-1], nil
}
