package xkb

import (
	"errors"
	"strconv"
	"strings"
)

// ParseKeymap compiles a keymap in the text v1 format, as a compositor
// sends it in wl_keyboard.keymap (a trailing NUL is allowed). Only the
// client's needs are compiled: keycodes, types, the compat interprets
// (for virtual modifiers and repeat) and symbols. A malformed keymap, or
// one beyond the package's limits, is an *Error.
func ParseKeymap(src string) (*Keymap, error) {
	c, err := compile(src)
	if err != nil {
		return nil, err
	}
	return c.km, nil
}

func compile(src string) (*compiler, error) {
	if len(src) > MaxKeymapSize {
		return nil, errorf(0, "%d bytes, more than the %d accepted", len(src), MaxKeymapSize)
	}
	p := newParser(src)
	top, err := p.statements()
	if p.lex.err != nil {
		return nil, p.lex.err
	}
	if err != nil {
		return nil, err
	}
	if !p.is("}") && p.peek().kind != tEOF {
		return nil, errorf(p.peek().line, "unexpected %q", p.peek().text)
	}
	if p.is("}") {
		return nil, errorf(p.peek().line, "unbalanced %q", "}")
	}
	var sections []*stmt
	for _, s := range top {
		if len(s.head) > 0 && s.head[0].text == "xkb_keymap" {
			sections = append(sections, s.body...)
		}
	}
	if len(sections) == 0 {
		return nil, errorf(0, "no xkb_keymap block")
	}
	c := newCompiler()
	// Keycodes, types, compat, symbols, whatever order they come in.
	for _, kind := range []string{"xkb_keycodes", "xkb_types", "xkb_compatibility", "xkb_compat", "xkb_symbols"} {
		for _, s := range sections {
			if len(s.head) > 0 && s.head[0].text == kind {
				if err := c.section(kind, s.body); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := c.finish(); err != nil {
		return nil, err
	}
	return c, nil
}

type compiler struct {
	km         *Keymap
	names      map[string]uint32 // key name -> keycode
	aliases    map[string]string
	typeByName map[string]*keyType
	interps    []interp
	defRepeat  bool
	defLevel1  bool
	symKeys    []*symKey
	symKeyOf   map[*key]*symKey // the first statement for each key
	modmaps    []modmapEntry
	minSet     bool
	maxSet     bool
	work       int // interprets tried, against maxWork
}

func newCompiler() *compiler {
	c := &compiler{
		km:         &Keymap{keys: map[uint32]*key{}},
		names:      map[string]uint32{},
		aliases:    map[string]string{},
		typeByName: map[string]*keyType{},
		symKeyOf:   map[*key]*symKey{},
	}
	for _, n := range realModNames {
		c.km.mods = append(c.km.mods, mod{name: n, mapping: Mask(1) << len(c.km.mods)})
	}
	return c
}

func (c *compiler) section(kind string, body []*stmt) error {
	for _, s := range body {
		if len(s.head) == 0 {
			continue
		}
		var err error
		switch kind {
		case "xkb_keycodes":
			err = c.keycode(s)
		case "xkb_types":
			err = c.typeStmt(s)
		case "xkb_compatibility", "xkb_compat":
			err = c.compatStmt(s)
		case "xkb_symbols":
			err = c.symbolsStmt(s)
		}
		if err != nil {
			var e *Error
			if errors.As(err, &e) && e.Line == 0 {
				e.Line = s.line
			}
			return err
		}
	}
	return nil
}

// number parses a numeric literal (decimal or 0x hex) up to 32 bits.
func number(e *expr) (uint32, error) {
	if e == nil || e.kind != "num" {
		return 0, errorf(0, "expected a number")
	}
	v, err := strconv.ParseUint(e.text, 0, 32)
	if err != nil {
		return 0, errorf(0, "bad number %q", e.text)
	}
	return uint32(v), nil
}

func (c *compiler) keycode(s *stmt) error {
	h := s.head[0]
	switch {
	case h.kind == tKeyName && s.value != nil:
		v, err := number(s.value)
		if err != nil {
			return err
		}
		if old, ok := c.names[h.text]; ok {
			delete(c.km.keys, old)
		}
		name := strings.Clone(h.text) // not a slice of the whole source
		c.names[name] = v
		c.km.keys[v] = &key{code: v, name: name}
	case h.text == "alias" && len(s.head) == 2 && s.value != nil:
		c.aliases[s.head[1].text] = s.value.text
	case h.text == "minimum" && s.value != nil:
		v, err := number(s.value)
		if err != nil {
			return err
		}
		c.km.minKey, c.minSet = v, true
	case h.text == "maximum" && s.value != nil:
		v, err := number(s.value)
		if err != nil {
			return err
		}
		c.km.maxKey, c.maxSet = v, true
	}
	return nil
}

func (c *compiler) keyByName(name string) (*key, bool) {
	if real, ok := c.aliases[name]; ok {
		name = real
	}
	code, ok := c.names[name]
	if !ok {
		return nil, false
	}
	return c.km.keys[code], true
}

func (c *compiler) modIndex(name string) int {
	for i, m := range c.km.mods {
		if strings.EqualFold(m.name, name) {
			return i
		}
	}
	return -1
}

func (c *compiler) declareVmods(items []*expr) error {
	for _, it := range items {
		name, val := it, (*expr)(nil)
		if it.kind == "assign" {
			name, val = it.left, it.right
		}
		i := c.modIndex(name.text)
		if i < 0 {
			if len(c.km.mods) >= maxMods {
				return errorf(0, "more than %d modifiers", maxMods)
			}
			c.km.mods = append(c.km.mods, mod{name: strings.Clone(name.text)})
			i = len(c.km.mods) - 1
		}
		if val != nil && i >= 8 {
			v, err := c.mask(val)
			if err != nil {
				return err
			}
			c.km.mods[i].mapping = v
			c.km.mods[i].explicit = true
		}
	}
	return nil
}

// mask evaluates a modifier mask expression (Shift+Lock, all, none,
// NumLock, 0x4000) to declared modifier bits.
func (c *compiler) mask(e *expr) (Mask, error) {
	if e == nil {
		return 0, errorf(0, "missing modifier mask")
	}
	switch e.kind {
	case "num":
		v, err := number(e)
		return Mask(v), err
	case "ident":
		switch strings.ToLower(e.text) {
		case "all":
			return 0xffffffff, nil
		case "none":
			return 0, nil
		}
		i := c.modIndex(e.text)
		if i < 0 {
			return 0, errorf(0, "unknown modifier %q", e.text)
		}
		return Mask(1) << i, nil
	case "bin":
		l, err := c.mask(e.left)
		if err != nil {
			return 0, err
		}
		r, err := c.mask(e.right)
		if err != nil {
			return 0, err
		}
		switch e.text {
		case "+":
			return l | r, nil
		case "-":
			return l &^ r, nil
		}
	}
	return 0, errorf(0, "bad modifier mask")
}

// levelNum parses "2", "Level2" or "level2" into a 0-based level.
func levelNum(e *expr) (int, error) {
	if e == nil {
		return 0, errorf(0, "missing level")
	}
	s := strings.TrimPrefix(strings.ToLower(e.text), "level")
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 || v > maxLevel {
		return 0, errorf(0, "bad level %q", e.text)
	}
	return v - 1, nil
}

func groupNum(e *expr) (int, error) {
	if e == nil {
		return 0, errorf(0, "missing group")
	}
	s := strings.TrimPrefix(strings.ToLower(e.text), "group")
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 || v > maxGroups {
		return 0, errorf(0, "bad group %q", e.text)
	}
	return v - 1, nil
}

func boolValue(e *expr) bool {
	switch strings.ToLower(e.text) {
	case "true", "yes", "on":
		return true
	}
	return false
}
