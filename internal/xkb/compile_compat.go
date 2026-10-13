package xkb

import (
	"strconv"
	"strings"
)

// How an interpret's modifiers must match a key's modifier map.
const (
	matchNone = iota
	matchAnyOrNone
	matchAny
	matchAll
	matchExactly
)

// interp is a compat interpret: what a keysym on a key does (here: the
// virtual modifier it binds and whether the key repeats).
type interp struct {
	sym          Keysym
	anySym       bool
	match        int
	mods         Mask
	vmod         int // -1: none
	levelOneOnly bool
	repeat       bool
}

func (c *compiler) compatStmt(s *stmt) error {
	h := s.head[0].text
	switch {
	case h == "virtual_modifiers":
		return c.declareVmods(s.items)
	case strings.EqualFold(h, "interpret.repeat") && s.value != nil:
		c.defRepeat = boolValue(s.value)
	case strings.EqualFold(h, "interpret.useModMapMods") && s.value != nil:
		c.defLevel1 = isLevelOne(s.value.text)
	case h == "interpret" && len(s.head) >= 2:
		return c.interpret(s)
	}
	return nil
}

func (c *compiler) interpret(s *stmt) error {
	if len(c.interps) >= maxInterprets {
		return errorf(0, "more than %d interprets", maxInterprets)
	}
	in := interp{vmod: -1, match: matchAnyOrNone, mods: realMask, repeat: c.defRepeat, levelOneOnly: c.defLevel1}
	symTok := s.head[1]
	if symTok.text == "Any" || symTok.text == "any" {
		in.anySym = true
	} else {
		ks := keysymToken(symTok)
		if ks == NoSymbol {
			return nil // an unknown keysym: libxkbcommon drops it too
		}
		in.sym = ks
	}
	if s.index != nil {
		switch strings.ToLower(s.index.text) {
		case "noneof":
			in.match = matchNone
		case "anyofornone":
			in.match = matchAnyOrNone
		case "anyof":
			in.match = matchAny
		case "allof":
			in.match = matchAll
		case "exactly":
			in.match = matchExactly
		}
		if len(s.index.args) == 1 {
			m, err := c.mask(s.index.args[0])
			if err != nil {
				return err
			}
			in.mods = m & realMask
		}
	}
	for _, f := range s.body {
		if len(f.head) == 0 || f.value == nil {
			continue
		}
		switch strings.ToLower(f.head[0].text) {
		case "virtualmodifier", "virtualmod":
			in.vmod = c.modIndex(f.value.text)
		case "repeat":
			in.repeat = boolValue(f.value)
		case "usemodmapmods", "usemodmap":
			in.levelOneOnly = isLevelOne(f.value.text)
		}
	}
	c.interps = append(c.interps, in)
	return nil
}

func isLevelOne(s string) bool {
	switch strings.ToLower(s) {
	case "level1", "levelone":
		return true
	}
	return false
}

// keysymToken reads a keysym from a token: a name, a hex number, or a
// decimal digit (which means the digit's keysym).
func keysymToken(t token) Keysym {
	return keysymText(t.kind == tNum, t.text)
}

func keysymText(isNum bool, text string) Keysym {
	if isNum {
		v, err := strconv.ParseUint(text, 0, 32)
		if err != nil {
			return NoSymbol
		}
		if !strings.HasPrefix(text, "0x") && v <= 9 {
			return Keysym('0' + v)
		}
		return Keysym(v)
	}
	if text == "NoSymbol" {
		return NoSymbol
	}
	return KeysymFromName(text)
}
