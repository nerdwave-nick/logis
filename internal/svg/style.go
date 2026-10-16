package svg

import (
	"math"
	"strconv"
	"strings"
)

// color is a straight-alpha sRGB colour, each channel 0 to 1.
type color struct{ r, g, b, a float64 }

// paintKind is what a fill or stroke paints with.
type paintKind uint8

const (
	paintNone paintKind = iota
	paintColor
	paintCurrent // currentColor, resolved with the element's color
	paintURL     // a paint server; fallback when it cannot be used
	// paintContext is SVG 2's context-fill (url "fill") or context-stroke
	// (url "stroke"): the paint of the use element drawing this one.
	paintContext
)

type paint struct {
	kind     paintKind
	color    color
	url      string
	fallback *paint
}

// style is an element's computed style.
type style struct {
	fill, stroke                       paint
	fillOpacity, strokeOpacity         float64
	evenOdd, clipEvenOdd               bool
	strokeWidth                        string // resolved against the viewport when drawn
	lineCap, lineJoin                  string
	miterLimit                         float64
	dashArray, dashOffset              string
	color                              color
	visible                            bool
	fontSize                           float64
	strokeFirst                        bool
	opacity, stopOpacity               float64
	stopColor                          paint
	clipPath, mask, filter, blend, vfx string
	markers                            bool
}

const defaultFontSize = 16

func rootStyle() style {
	black := color{0, 0, 0, 1}
	return style{
		fill: paint{kind: paintColor, color: black}, fillOpacity: 1, strokeOpacity: 1,
		strokeWidth: "1", lineCap: "butt", lineJoin: "miter", miterLimit: 4,
		color: black, visible: true, fontSize: defaultFontSize, opacity: 1,
		stopColor: paint{kind: paintColor, color: black}, stopOpacity: 1,
	}
}

// computeStyle is n's style given its parent's: inherited properties carry
// over, the others start from their initial values; invalid values are
// ignored.
func computeStyle(parent style, n *node) style {
	s := parent
	init := rootStyle()
	s.opacity, s.stopColor, s.stopOpacity = 1, init.stopColor, 1
	s.clipPath, s.mask, s.filter, s.blend, s.vfx = "", "", "", "", ""
	props := n.props
	get := func(name string) (string, bool) {
		v, ok := props[name]
		return v, ok && v != "inherit" && v != ""
	}
	if v, ok := get("font-size"); ok {
		if f, ok := parseLength(v, parent.fontSize, parent.fontSize); ok && f > 0 {
			s.fontSize = f
		}
	}
	if v, ok := get("color"); ok {
		if c, ok := parseColor(v); ok {
			s.color = c
		}
	}
	for _, p := range [...]struct {
		name string
		dst  *paint
	}{{"fill", &s.fill}, {"stroke", &s.stroke}, {"stop-color", &s.stopColor}} {
		if v, ok := get(p.name); ok {
			if pt, ok := parsePaint(v); ok {
				*p.dst = pt
			}
		}
	}
	for _, p := range [...]struct {
		name string
		dst  *float64
	}{{"fill-opacity", &s.fillOpacity}, {"stroke-opacity", &s.strokeOpacity}, {"opacity", &s.opacity}, {"stop-opacity", &s.stopOpacity}} {
		if v, ok := get(p.name); ok {
			if f, ok := parseOpacity(v); ok {
				*p.dst = f
			}
		}
	}
	if v, ok := get("stroke-miterlimit"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 1 {
			s.miterLimit = f
		}
	}
	keyword := func(name string, dst *string, allowed ...string) {
		if v, ok := get(name); ok {
			for _, a := range allowed {
				if v == a {
					*dst = v
				}
			}
		}
	}
	keyword("stroke-linecap", &s.lineCap, "butt", "round", "square")
	keyword("stroke-linejoin", &s.lineJoin, "miter", "round", "bevel", "miter-clip", "arcs")
	text := func(name string, dst *string) {
		if v, ok := get(name); ok {
			*dst = v
		}
	}
	text("stroke-width", &s.strokeWidth)
	text("stroke-dasharray", &s.dashArray)
	text("stroke-dashoffset", &s.dashOffset)
	text("clip-path", &s.clipPath)
	text("mask", &s.mask)
	text("filter", &s.filter)
	text("mix-blend-mode", &s.blend)
	text("vector-effect", &s.vfx)
	if v, ok := get("fill-rule"); ok {
		s.evenOdd = v == "evenodd"
	}
	if v, ok := get("clip-rule"); ok {
		s.clipEvenOdd = v == "evenodd"
	}
	if v, ok := get("visibility"); ok {
		s.visible = v == "visible"
	}
	if v, ok := get("paint-order"); ok {
		f := strings.Fields(v)
		s.strokeFirst = len(f) > 0 && f[0] == "stroke" || len(f) > 1 && f[0] == "markers" && f[1] == "stroke"
	}
	for _, m := range [...]string{"marker", "marker-start", "marker-mid", "marker-end"} {
		if v, ok := get(m); ok {
			s.markers = v != "none"
		}
	}
	return s
}

// displayed reports whether n is rendered at all (display: none hides it
// and everything inside).
func displayed(n *node) bool { return n.props["display"] != "none" }

func parseOpacity(s string) (float64, bool) {
	pct := strings.HasSuffix(s, "%")
	f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	if pct {
		f /= 100
	}
	return min(max(f, 0), 1), true
}

// parseLength reads a length in px: ref is what 100% is, em the font
// size.
func parseLength(s string, ref, em float64) (float64, bool) {
	sc := scanner{s: strings.TrimSpace(s)}
	v, ok := sc.number()
	if !ok {
		return 0, false
	}
	unit := strings.TrimSpace(sc.s[sc.i:])
	factor, known := map[string]float64{
		"": 1, "px": 1, "pt": 4.0 / 3, "pc": 16, "mm": 96 / 25.4, "cm": 96 / 2.54,
		"in": 96, "em": em, "ex": em / 2, "%": ref / 100,
	}[unit]
	return v * factor, known
}

// parsePaint reads none, a colour, currentColor or url(#id) with an
// optional fallback.
func parsePaint(s string) (paint, bool) {
	switch s {
	case "none":
		return paint{kind: paintNone}, true
	case "currentColor", "currentcolor":
		return paint{kind: paintCurrent}, true
	case "context-fill":
		return paint{kind: paintContext, url: "fill"}, true
	case "context-stroke":
		return paint{kind: paintContext, url: "stroke"}, true
	}
	if rest, ok := strings.CutPrefix(s, "url("); ok {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return paint{}, false
		}
		ref := strings.Trim(strings.TrimSpace(rest[:end]), `'"`)
		p := paint{kind: paintURL, url: strings.TrimPrefix(ref, "#")}
		if fb := strings.TrimSpace(rest[end+1:]); fb != "" {
			if f, ok := parsePaint(fb); ok && f.kind != paintURL {
				p.fallback = &f
			}
		}
		return p, true
	}
	c, ok := parseColor(s)
	return paint{kind: paintColor, color: c}, ok
}
