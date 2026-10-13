package svg

import "math"

// shader colours device pixels: premultiplied RGBA, 0 to 1.
type shader interface {
	at(x, y float64) [4]float32
}

type solid [4]float32

func (s solid) at(float64, float64) [4]float32 { return s }

func premultiplied(c color, opacity float64) [4]float32 {
	a := c.a * opacity
	return [4]float32{float32(c.r * a), float32(c.g * a), float32(c.b * a), float32(a)}
}

type stop struct {
	offset float64
	color  color
}

// gradient is a resolved linear or radial gradient. Device points map
// into gradient space through inv.
type gradient struct {
	radial                bool
	x1, y1, x2, y2        float64 // linear vector
	cx, cy, r, fx, fy, fr float64 // radial circles
	spread                string
	stops                 []stop
	inv                   Matrix
	opacity               float64
}

func (g *gradient) at(x, y float64) [4]float32 {
	px, py := g.inv.Apply(x, y)
	var t float64
	if g.radial {
		var ok bool
		if t, ok = g.radialT(px, py); !ok {
			return [4]float32{}
		}
	} else {
		dx, dy := g.x2-g.x1, g.y2-g.y1
		t = ((px-g.x1)*dx + (py-g.y1)*dy) / (dx*dx + dy*dy)
	}
	switch g.spread {
	case "reflect":
		t = math.Abs(math.Mod(t, 2))
		if t > 1 {
			t = 2 - t
		}
	case "repeat":
		t -= math.Floor(t)
	default:
		t = min(max(t, 0), 1)
	}
	return premultiplied(g.colorAt(t), g.opacity)
}

// radialT solves for the largest t whose circle, between the focal and
// the end circle, passes through the point with a non-negative radius.
func (g *gradient) radialT(px, py float64) (float64, bool) {
	cdx, cdy, dr := g.cx-g.fx, g.cy-g.fy, g.r-g.fr
	pdx, pdy := px-g.fx, py-g.fy
	a := cdx*cdx + cdy*cdy - dr*dr
	b := pdx*cdx + pdy*cdy + g.fr*dr
	c := pdx*pdx + pdy*pdy - g.fr*g.fr
	if math.Abs(a) < 1e-12 {
		if b == 0 {
			return 0, false
		}
		t := c / (2 * b)
		return t, g.fr+t*dr >= 0
	}
	disc := b*b - a*c
	if disc < 0 {
		return 0, false
	}
	sq := math.Sqrt(disc)
	for _, t := range [2]float64{(b + sq) / a, (b - sq) / a} {
		if g.fr+t*dr >= 0 {
			return t, true
		}
	}
	return 0, false
}

func (g *gradient) colorAt(t float64) color {
	s := g.stops
	if t <= s[0].offset {
		return s[0].color
	}
	for i := 1; i < len(s); i++ {
		if t <= s[i].offset {
			a, b := s[i-1], s[i]
			if b.offset == a.offset {
				return b.color
			}
			u := (t - a.offset) / (b.offset - a.offset)
			mix := func(p, q float64) float64 { return p + (q-p)*u }
			return color{mix(a.color.r, b.color.r), mix(a.color.g, b.color.g), mix(a.color.b, b.color.b), mix(a.color.a, b.color.a)}
		}
	}
	return s[len(s)-1].color
}

// gradientAttr follows a gradient's href chain: the first element in it
// that has attr defines it.
func (r *renderer) gradientAttr(n *node, attr string) string {
	seen := map[*node]bool{}
	for at := n; at != nil && !seen[at]; at = r.doc.ids[hrefID(at)] {
		seen[at] = true
		if at.name != "linearGradient" && at.name != "radialGradient" {
			return ""
		}
		if v, ok := at.attrs[attr]; ok {
			return v
		}
	}
	return ""
}

// gradientStops are the stops of the first gradient in the href chain
// that has any.
func (r *renderer) gradientStops(n *node) []stop {
	seen := map[*node]bool{}
	for at := n; at != nil && !seen[at]; at = r.doc.ids[hrefID(at)] {
		seen[at] = true
		if at.name != "linearGradient" && at.name != "radialGradient" {
			return nil
		}
		var stops []stop
		for _, c := range at.children {
			if c.name != "stop" {
				continue
			}
			st := computeStyle(r.styleOf(at), c)
			off, _ := parseOpacity(c.attr("offset"))
			if len(stops) > 0 {
				off = max(off, stops[len(stops)-1].offset)
			}
			col := st.stopColor.color
			if st.stopColor.kind == paintCurrent {
				col = st.color
			}
			col.a *= st.stopOpacity
			stops = append(stops, stop{off, col})
		}
		if len(stops) > 0 {
			return stops
		}
	}
	return nil
}

// gradientShader resolves gradient n for an element with bounding box
// bbox (user space) drawn through ctm. nil means paint nothing.
func (r *renderer) gradientShader(n *node, bbox rect, ctm Matrix, opacity float64) shader {
	stops := r.gradientStops(n)
	if len(stops) == 0 {
		return nil
	}
	if len(stops) == 1 {
		return solid(premultiplied(stops[0].color, opacity))
	}
	g := &gradient{radial: n.name == "radialGradient", stops: stops, opacity: opacity}
	g.spread = r.gradientAttr(n, "spreadMethod")
	userSpace := r.gradientAttr(n, "gradientUnits") == "userSpaceOnUse"
	m := ctm
	vp := r.viewport
	if !userSpace {
		if !bbox.ok || bbox.x1-bbox.x0 <= 0 || bbox.y1-bbox.y0 <= 0 {
			return nil
		}
		m = m.Mul(bboxUnits(bbox))
		vp = viewport{1, 1}
	}
	m = m.Mul(parseTransform(r.gradientAttr(n, "gradientTransform")))
	inv, ok := m.Invert()
	if !ok {
		return nil
	}
	g.inv = inv
	num := func(attr string, def string, ref float64) float64 {
		s := r.gradientAttr(n, attr)
		if s == "" {
			s = def
		}
		v, ok := parseLength(s, ref, defaultFontSize)
		if !ok {
			v, _ = parseLength(def, ref, defaultFontSize)
		}
		return v
	}
	if !g.radial {
		g.x1, g.y1 = num("x1", "0%", vp.w), num("y1", "0%", vp.h)
		g.x2, g.y2 = num("x2", "100%", vp.w), num("y2", "0%", vp.h)
		if g.x1 == g.x2 && g.y1 == g.y2 {
			return solid(premultiplied(stops[len(stops)-1].color, opacity))
		}
		return g
	}
	g.cx, g.cy, g.r = num("cx", "50%", vp.w), num("cy", "50%", vp.h), num("r", "50%", vp.diagonal())
	g.fx, g.fy = g.cx, g.cy
	if r.gradientAttr(n, "fx") != "" {
		g.fx = num("fx", "50%", vp.w)
	}
	if r.gradientAttr(n, "fy") != "" {
		g.fy = num("fy", "50%", vp.h)
	}
	g.fr = num("fr", "0%", vp.diagonal())
	if !(g.r > 0) {
		return solid(premultiplied(stops[len(stops)-1].color, opacity))
	}
	// SVG 1.1: a focal point outside the end circle moves onto it.
	if d := math.Hypot(g.fx-g.cx, g.fy-g.cy); d > g.r*0.999 {
		k := g.r * 0.999 / d
		g.fx, g.fy = g.cx+(g.fx-g.cx)*k, g.cy+(g.fy-g.cy)*k
	}
	return g
}
