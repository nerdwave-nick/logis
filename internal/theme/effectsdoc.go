package theme

import (
	"strconv"
	"strings"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The theme's effects as written (cd816303, diverging from Rust v1's seven
// strengths): a group per effect of the CRT pass with its controls
// (effects.Controls), where a bare number is the group's strength (the
// aberration's amount); v1's keys that moved into a group (bloom,
// curvature, vignette) still set their new places. Both are read into the
// group as it is written, so a theme over its base sets just what it says.

const groupWants = "a strength (0 to 1), or an object of the effect's settings"

// v1Moved are v1's effects keys that live in a group now.
var v1Moved = []struct{ name, group, key string }{
	{"vignette", "roundness", "vignette"},
	{"bloom", "glow", "strength"},
	{"curvature", "roundness", "curvature"},
}

// effectsDoc is the effects section: the groups in effects.Controls'
// order, the glitch (glitchdoc.go), then v1's moved keys. A group whose first control is no strength
// (roundness) takes no bare number.
var effectsDoc = func() *section {
	s := sec("EffectsDoc")
	for _, group := range effects.Groups() {
		sub := sec(strings.ToUpper(group[:1]) + group[1:] + "Doc")
		for _, k := range effects.Controls {
			switch {
			case k.Group != group:
			case k.Words != nil:
				sub.keys = append(sub.keys, key{name: k.Key, kind: kMask})
			default:
				sub.keys = append(sub.keys, flt(k.Key))
			}
		}
		kind := kSection
		if first := sub.keys[0].name; first == "strength" || first == "amount" {
			kind = kGroup
		}
		s.keys = append(s.keys, key{name: group, kind: kind, sub: sub})
	}
	s.keys = append(s.keys, key{name: "glitch", kind: kGroup, sub: glitchDoc})
	for _, m := range v1Moved {
		s.keys = append(s.keys, flt(m.name))
	}
	return s
}()

// spelled is a control's value written under another name (a bare number
// for a group's strength, or v1's key): a refusal of it names what was
// written.
type spelled struct {
	v float32
	// key is the path as written, effects.bloom say.
	key string
}

// group reads an effect's group: its object, or a bare number for its
// first control.
func (r reader) group(v *jsonc.Value, k key) (node, error) {
	switch v.Kind {
	case jsonc.Number:
		f, _ := strconv.ParseFloat(string(v.Number), 64)
		return node{k.sub.keys[0].name: spelled{float32(f), "effects." + k.name}}, nil
	case jsonc.Object, jsonc.Array:
		return r.section(v, k.sub)
	}
	return nil, r.wrongType(v, groupWants)
}

// effects reads the effects section and moves v1's keys into their
// groups; a value given under both names is refused at the later one.
func (r reader) effects(v *jsonc.Value, s *section) (node, error) {
	n, err := r.section(v, s)
	if err != nil {
		return nil, err
	}
	end := func(name string) int {
		for _, m := range v.Members {
			if m.Key == name {
				return m.KeyEnd
			}
		}
		return v.End
	}
	for _, m := range v1Moved {
		f, ok := n[m.name].(float32)
		if !ok {
			continue
		}
		delete(n, m.name)
		g, _ := n[m.group].(node)
		if g == nil {
			g = node{}
			n[m.group] = g
		}
		if _, given := g[m.key]; given {
			return nil, r.doc.Errorf(max(end(m.name), end(m.group)), "`%s` is v1's name of `%s.%s`: give it once", m.name, m.group, m.key)
		}
		g[m.key] = spelled{f, "effects." + m.name}
	}
	return n, nil
}

// control is a control's value in group n at key, held to lo..hi.
func (r *resolver) control(n node, k, key string, lo, hi float32) float32 {
	v, ok := n[k].(float32)
	if s, written := n[k].(spelled); written {
		v, key, ok = s.v, s.key, true
	}
	if !ok {
		r.fail("%s: missing, and no base theme gives it", key)
		return 0
	}
	if !(v >= lo && v <= hi) {
		r.fail("%s: %s is out of range (%s to %s)", key, show(v), show(lo), show(hi))
	}
	return v
}

func (r *resolver) effects(e node) Effects {
	var c effects.CRT
	for _, k := range effects.Controls {
		g := need[node](r, e, k.Group, "effects."+k.Group)
		at := "effects." + k.Group + "." + k.Key
		if k.Words != nil {
			k.Set(&c, float32(need[effects.MaskKind](r, g, k.Key, at)))
			continue
		}
		k.Set(&c, r.control(g, k.Key, at, k.Min, k.Max))
	}
	return c
}
