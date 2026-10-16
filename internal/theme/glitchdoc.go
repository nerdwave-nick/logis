package theme

import (
	"strings"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The theme's glitch (33cfcf44): effects.glitch is a
// group of the glitch pass's controls (effects.GlitchControls) in their
// parts, its strength first, then the bursts (effects.BurstControls and
// the events that fire them). A bare number is the strength, as for the
// CRT pass's groups.

// glitchDoc is effects.glitch: the strength, a section per part, bursts.
var glitchDoc = func() *section {
	s := sec("GlitchDoc")
	for _, name := range effects.GlitchParts() {
		if name == "" {
			s.keys = append(s.keys, flt("strength"))
			continue
		}
		sub := sec(docName(name) + "Doc")
		for _, k := range effects.GlitchControls {
			if k.Part == name {
				sub.keys = append(sub.keys, flt(k.Key))
			}
		}
		s.keys = append(s.keys, part(name, sub))
	}
	bursts := sec("BurstsDoc")
	for _, k := range effects.BurstControls {
		bursts.keys = append(bursts.keys, flt(k.Key))
	}
	bursts.keys = append(bursts.keys, key{name: "on", kind: kEvents})
	s.keys = append(s.keys, part("bursts", bursts))
	return s
}()

// docName is a key as a struct's name: noise-band is NoiseBand.
func docName(k string) string {
	var b strings.Builder
	for _, w := range strings.Split(k, "-") {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

// events reads bursts.on: an array of the events' names.
func (r reader) events(v *jsonc.Value) (effects.Events, error) {
	if v.Kind != jsonc.Array {
		return 0, r.wrongType(v, "a sequence")
	}
	var on effects.Events
	for _, e := range v.Elems {
		if e.Kind != jsonc.String {
			return 0, r.wrongType(e, "a string")
		}
		i, err := r.word(e, effects.EventWords)
		if err != nil {
			return 0, err
		}
		on |= 1 << i
	}
	return on, nil
}

func (r *resolver) glitch(e node) effects.Glitch {
	var g effects.Glitch
	n := need[node](r, e, "glitch", "effects.glitch")
	for _, k := range effects.GlitchControls {
		in, at := n, "effects.glitch."+k.Key
		if k.Part != "" {
			in = need[node](r, n, k.Part, "effects.glitch."+k.Part)
			at = "effects.glitch." + k.Part + "." + k.Key
		}
		k.Set(&g, r.control(in, k.Key, at, k.Min, k.Max))
	}
	b := need[node](r, n, "bursts", "effects.glitch.bursts")
	for _, k := range effects.BurstControls {
		k.Set(&g.Bursts, r.control(b, k.Key, "effects.glitch.bursts."+k.Key, k.Min, k.Max))
	}
	g.Bursts.On = need[effects.Events](r, b, "on", "effects.glitch.bursts.on")
	return g
}
