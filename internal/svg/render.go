package svg

import (
	"image"
	"slices"
	"strings"
)

// nonRendering elements are drawn only when referenced, or never.
var nonRendering = map[string]bool{
	"defs": true, "title": true, "desc": true, "metadata": true, "style": true,
	"symbol": true, "clipPath": true, "mask": true, "linearGradient": true,
	"radialGradient": true, "stop": true, "pattern": true, "filter": true,
	"marker": true, "script": true, "font": true, "font-face": true, "cursor": true,
	"view": true, "animate": true, "animateTransform": true, "animateMotion": true,
	"animateColor": true, "set": true, "glyph": true, "missing-glyph": true,
	"color-profile": true, "mpath": true, "hkern": true, "vkern": true,
}

var shapes = map[string]bool{
	"path": true, "rect": true, "circle": true, "ellipse": true, "line": true,
	"polyline": true, "polygon": true,
}

type renderer struct {
	doc      *Doc
	report   map[string]bool
	viewport viewport
	bounds   image.Rectangle
	// active guards references (use, clip paths, masks) against cycles.
	active map[*node]bool
	styles map[*node]style
}

// Render draws the document over dst (premultiplied RGBA8), its user
// space mapped onto dst through m after the view box. It returns what the
// document holds that logis does not draw, sorted; those parts are left
// out or drawn plainer, never fatal.
func (d *Doc) Render(dst *image.RGBA, m Matrix) []string {
	r := &renderer{
		doc: d, report: map[string]bool{}, bounds: dst.Rect,
		viewport: viewport{d.width, d.height},
		active:   map[*node]bool{}, styles: map[*node]style{},
	}
	for k := range d.unsupported {
		r.report[k] = true
	}
	if vb, ok := parseViewBox(d.root.attr("viewBox")); ok {
		r.viewport = viewport{vb[2], vb[3]}
	}
	c := newCanvas(dst.Rect)
	root := r.styleOf(d.root)
	r.effects(d.root, root, m.Mul(d.view), c, func(c *canvas) { r.children(d.root, m.Mul(d.view), root, c) })
	c.drawOver(dst)
	out := make([]string, 0, len(r.report))
	for k := range r.report {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// styleOf is n's style inherited along its own ancestors (for elements
// drawn by reference: gradient stops, clip paths, masks).
func (r *renderer) styleOf(n *node) style {
	if s, ok := r.styles[n]; ok {
		return s
	}
	parent := rootStyle()
	if n.parent != nil {
		parent = r.styleOf(n.parent)
	}
	s := computeStyle(parent, n)
	r.styles[n] = s
	return s
}

func (r *renderer) children(n *node, ctm Matrix, s style, c *canvas) {
	for _, child := range n.children {
		r.element(child, ctm, s, c)
		if n.name == "switch" && displayed(child) {
			return // a switch draws its first child only
		}
	}
}

func (r *renderer) element(n *node, ctm Matrix, parent style, c *canvas) {
	if !displayed(n) || nonRendering[n.name] {
		return
	}
	s := computeStyle(parent, n)
	switch {
	case shapes[n.name]:
		r.shape(n, ctm, s, c)
	case n.name == "g" || n.name == "a" || n.name == "switch":
		m := ctm.Mul(parseTransform(n.attr("transform")))
		r.effects(n, s, m, c, func(c *canvas) { r.children(n, m, s, c) })
	case n.name == "svg":
		m := ctm.Mul(r.viewportTransform(n, n, 0, 0))
		r.effects(n, s, m, c, func(c *canvas) { r.children(n, m, s, c) })
	case n.name == "use":
		r.use(n, ctm, s, c)
	default:
		r.report["<"+n.name+">"] = true
	}
}

// viewportTransform places a nested svg or a used symbol (vb) at x, y in
// the size of sized (the use or the svg itself).
func (r *renderer) viewportTransform(vb, sized *node, x, y float64) Matrix {
	length := func(attr string, ref float64, def string) float64 {
		s := sized.attr(attr)
		if s == "" {
			s = def
		}
		v, _ := parseLength(s, ref, defaultFontSize)
		return v
	}
	m := Translate(x+length("x", r.viewport.w, "0"), y+length("y", r.viewport.h, "0"))
	if box, ok := parseViewBox(vb.attr("viewBox")); ok {
		w, h := length("width", r.viewport.w, "100%"), length("height", r.viewport.h, "100%")
		m = m.Mul(viewBoxTransform(box, vb.attr("preserveAspectRatio"), w, h))
	}
	return m
}

func (r *renderer) use(n *node, ctm Matrix, s style, c *canvas) {
	target := r.doc.ids[hrefID(n)]
	if target == nil || r.active[target] || r.active[n] || !displayed(target) {
		return
	}
	r.active[n] = true
	defer delete(r.active, n)
	m := ctm.Mul(parseTransform(n.attr("transform")))
	r.effects(n, s, m, c, func(c *canvas) {
		switch target.name {
		case "symbol", "svg":
			inner := m.Mul(r.viewportTransform(target, n, 0, 0))
			ts := computeStyle(s, target)
			r.effects(target, ts, inner, c, func(c *canvas) { r.children(target, inner, ts, c) })
		default:
			x, _ := parseLength(n.attr("x"), r.viewport.w, s.fontSize)
			y, _ := parseLength(n.attr("y"), r.viewport.h, s.fontSize)
			r.element(target, m.Mul(Translate(x, y)), s, c)
		}
	})
}

func (r *renderer) shape(n *node, ctm Matrix, s style, c *canvas) {
	m := ctm.Mul(parseTransform(n.attr("transform")))
	p, ok := shapePath(n, r.viewport, s.fontSize)
	if !ok || !s.visible {
		return
	}
	if s.markers {
		r.report["marker"] = true
	}
	if s.vfx == "non-scaling-stroke" {
		r.report["vector-effect"] = true
	}
	r.effects(n, s, m, c, func(dst *canvas) {
		var bbox rect
		if s.fill.kind == paintURL || s.stroke.kind == paintURL {
			bbox = p.bounds(Identity)
		}
		fill := func() {
			if sh := r.shader(s.fill, s, s.fillOpacity, bbox, m); sh != nil {
				dst.paint(fillCoverage(p, m, s.evenOdd, dst.r), sh)
			}
		}
		stroke := func() {
			sh := r.shader(s.stroke, s, s.strokeOpacity, bbox, m)
			if sh == nil {
				return
			}
			if polys := r.strokeOutline(p, s, m); polys != nil {
				dst.paint(polygonCoverage(polys, dst.r), sh)
			}
		}
		if s.strokeFirst {
			stroke()
			fill()
		} else {
			fill()
			stroke()
		}
	})
}

// strokeOutline is the stroke of p as device-space polygons.
func (r *renderer) strokeOutline(p Path, s style, m Matrix) [][][2]float64 {
	width, ok := parseLength(s.strokeWidth, r.viewport.diagonal(), s.fontSize)
	if !ok || !(width > 0) {
		return nil
	}
	tol := fillTolerance / m.expansion()
	lines := p.flatten(Identity, tol)
	if pattern, ok := r.dashPattern(s); ok {
		offset, _ := parseLength(s.dashOffset, r.viewport.diagonal(), s.fontSize)
		lines = dash(lines, pattern, offset)
	}
	st := &stroker{hw: width / 2, cap: s.lineCap, join: s.lineJoin, limit: s.miterLimit, tol: tol}
	for _, l := range lines {
		st.stroke(l)
	}
	for _, poly := range st.out {
		for i, q := range poly {
			x, y := m.Apply(q[0], q[1])
			poly[i] = [2]float64{x, y}
		}
	}
	return st.out
}

// dashPattern is the dash array; !ok for none, an invalid one, or one
// whose lengths add up to nothing (all of which draw solid).
func (r *renderer) dashPattern(s style) ([]float64, bool) {
	if s.dashArray == "" || s.dashArray == "none" {
		return nil, false
	}
	var out []float64
	sum := 0.0
	for _, f := range strings.FieldsFunc(s.dashArray, func(r rune) bool { return r == ',' || isSpace(byte(r)) }) {
		v, ok := parseLength(f, r.viewport.diagonal(), s.fontSize)
		if !ok || v < 0 {
			return nil, false
		}
		out = append(out, v)
		sum += v
	}
	if !(sum > 0) {
		return nil, false
	}
	if len(out)%2 == 1 {
		out = append(out, out...)
	}
	return out, true
}

// shader is how p paints: nil for nothing.
func (r *renderer) shader(p paint, s style, opacity float64, bbox rect, m Matrix) shader {
	switch p.kind {
	case paintColor:
		return solid(premultiplied(p.color, opacity))
	case paintCurrent:
		return solid(premultiplied(s.color, opacity))
	case paintURL:
		target := r.doc.ids[p.url]
		if target != nil && (target.name == "linearGradient" || target.name == "radialGradient") {
			return r.gradientShader(target, bbox, m, opacity)
		}
		if target != nil && target.name == "pattern" {
			r.report["pattern"] = true
		}
		if p.fallback != nil {
			return r.shader(*p.fallback, s, opacity, bbox, m)
		}
	}
	return nil
}
