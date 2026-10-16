package svg

import "strings"

// effects draws n (style s, user space through m) with draw: straight
// onto c, or through a layer when n has opacity, a clip path or a mask.
// A clip path or mask that names nothing usable hides the element, as in
// resvg. Filters and blend modes are reported and left out.
func (r *renderer) effects(n *node, s style, m Matrix, c *canvas, draw func(*canvas)) {
	if s.filter != "" && s.filter != "none" {
		r.report["filter"] = true
	}
	if s.blend != "" && s.blend != "normal" {
		r.report["mix-blend-mode"] = true
	}
	clip, okClip := r.reference(s.clipPath, "clipPath")
	mask, okMask := r.reference(s.mask, "mask")
	if !okClip || !okMask || s.opacity <= 0 {
		return
	}
	if s.opacity >= 1 && clip == nil && mask == nil {
		draw(c)
		return
	}
	layer := newCanvas(c.r)
	draw(layer)
	var weights []float32
	bbox := func() rect { return r.contentBBox(n, Identity) }
	if clip != nil {
		weights = r.clipMask(clip, m, bbox)
		if weights == nil {
			return
		}
	}
	if mask != nil {
		mw := r.maskWeights(mask, m, bbox)
		if mw == nil {
			return
		}
		if weights == nil {
			weights = mw
		} else {
			for i := range weights {
				weights[i] *= mw[i]
			}
		}
	}
	c.composite(layer, float32(s.opacity), weights)
}

// reference resolves a url(#id) property to an element named want: nil
// for none; !ok when it names nothing of that kind.
func (r *renderer) reference(v, want string) (*node, bool) {
	if v == "" || v == "none" {
		return nil, true
	}
	p, ok := parsePaint(v)
	if !ok || p.kind != paintURL {
		return nil, true
	}
	n := r.doc.ids[p.url]
	if n == nil || n.name != want || r.active[n] {
		return nil, false
	}
	return n, true
}

// clipMask is the coverage of clip path cp for an element in user space
// m: the union of its shapes, each by its clip-rule.
func (r *renderer) clipMask(cp *node, m Matrix, bbox func() rect) []float32 {
	m = m.Mul(parseTransform(cp.attr("transform")))
	if cp.attr("clipPathUnits") == "objectBoundingBox" {
		b := bbox()
		if !b.ok || b.x1 <= b.x0 || b.y1 <= b.y0 {
			return nil
		}
		m = m.Mul(bboxUnits(b))
	}
	r.active[cp] = true
	defer delete(r.active, cp)
	cs := r.styleOf(cp)
	layer := newCanvas(r.bounds)
	white := solid{1, 1, 1, 1}
	var add func(n *node, m Matrix, s style)
	add = func(n *node, m Matrix, s style) {
		if !displayed(n) {
			return
		}
		s = computeStyle(s, n)
		switch {
		case shapes[n.name]:
			p, ok := shapePath(n, r.viewport, s.fontSize)
			if ok && s.visible {
				layer.paint(fillCoverage(p, m.Mul(parseTransform(n.attr("transform"))), s.clipEvenOdd, layer.r), white)
			}
		case n.name == "use":
			if t := r.doc.ids[hrefID(n)]; t != nil && shapes[t.name] {
				x, _ := parseLength(n.attr("x"), r.viewport.w, s.fontSize)
				y, _ := parseLength(n.attr("y"), r.viewport.h, s.fontSize)
				add(t, m.Mul(parseTransform(n.attr("transform"))).Mul(Translate(x, y)), s)
			}
		case n.name == "text":
			r.report["<text>"] = true
		}
	}
	for _, child := range cp.children {
		add(child, m, cs)
	}
	weights := layer.alphaMask()
	if outer, ok := r.reference(cs.clipPath, "clipPath"); !ok {
		return nil
	} else if outer != nil {
		ow := r.clipMask(outer, m, bbox)
		if ow == nil {
			return nil
		}
		for i := range weights {
			weights[i] *= ow[i]
		}
	}
	return weights
}

// maskWeights is mask mk's luminance for an element in user space m,
// limited to the mask's region.
func (r *renderer) maskWeights(mk *node, m Matrix, bbox func() rect) []float32 {
	unitsBox := mk.attr("maskUnits") != "userSpaceOnUse"
	contentBox := mk.attr("maskContentUnits") == "objectBoundingBox"
	var b rect
	if unitsBox || contentBox {
		if b = bbox(); !b.ok || b.x1 <= b.x0 || b.y1 <= b.y0 {
			return nil
		}
	}
	r.active[mk] = true
	defer delete(r.active, mk)
	region := m
	vp := r.viewport
	if unitsBox {
		region, vp = m.Mul(bboxUnits(b)), viewport{1, 1}
	}
	num := func(attr, def string, ref float64) float64 {
		v, ok := parseLength(mk.attr(attr), ref, defaultFontSize)
		if !ok {
			v, _ = parseLength(def, ref, defaultFontSize)
		}
		return v
	}
	var area Path
	area.roundedRect(num("x", "-10%", vp.w), num("y", "-10%", vp.h), num("width", "120%", vp.w), num("height", "120%", vp.h), 0, 0)
	inside := fillCoverage(area, region, false, r.bounds)
	if inside == nil {
		return nil
	}
	content := m
	if contentBox {
		content = m.Mul(bboxUnits(b))
	}
	layer := newCanvas(r.bounds)
	ms := r.styleOf(mk)
	r.children(mk, content, ms, layer)
	weights := layer.luminanceMask()
	for i := range weights {
		x, y := r.bounds.Min.X+i%r.bounds.Dx(), r.bounds.Min.Y+i/r.bounds.Dx()
		weights[i] *= inside.at(x, y)
	}
	if ms.mask != "" && ms.mask != "none" && !strings.HasPrefix(ms.mask, "url(#)") {
		r.report["mask on mask"] = true
	}
	return weights
}

// bbox is n's bounding box through m, its transform included: its shape,
// or the union of what it draws (fill geometry, no strokes).
func (r *renderer) bbox(n *node, m Matrix) rect {
	return r.contentBBox(n, m.Mul(parseTransform(n.attr("transform"))))
}

// contentBBox is n's bounding box in its own user space (inside its
// transform) through m.
func (r *renderer) contentBBox(n *node, m Matrix) rect {
	var b rect
	if !displayed(n) || r.active[n] {
		return b
	}
	switch {
	case shapes[n.name]:
		if p, ok := shapePath(n, r.viewport, defaultFontSize); ok {
			b = p.bounds(m)
		}
	case n.name == "use":
		if t := r.doc.ids[hrefID(n)]; t != nil {
			x, _ := parseLength(n.attr("x"), r.viewport.w, defaultFontSize)
			y, _ := parseLength(n.attr("y"), r.viewport.h, defaultFontSize)
			r.active[n] = true
			if t.name == "symbol" {
				inner := m.Mul(r.viewportTransform(t, n, 0, 0))
				for _, c := range t.children {
					b.union(r.bbox(c, inner))
				}
			} else {
				b = r.bbox(t, m.Mul(Translate(x, y)))
			}
			delete(r.active, n)
		}
	case n.name == "g" || n.name == "a" || n.name == "switch" || n.name == "svg":
		for _, c := range n.children {
			if !nonRendering[c.name] {
				b.union(r.bbox(c, m))
			}
		}
	}
	return b
}
