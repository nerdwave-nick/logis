package svg

import (
	"image"
	"math"
)

// maxTile bounds a pattern tile's raster, in pixels per side.
const maxTile = 2048

// tileShader paints a pattern: its tile rendered once at device
// resolution, repeated, sampled bilinearly.
type tileShader struct {
	tile    *canvas
	inv     Matrix // device → tile raster
	opacity float32
}

func (s *tileShader) at(x, y float64) [4]float32 {
	tx, ty := s.inv.Apply(x, y)
	w, h := s.tile.r.Dx(), s.tile.r.Dy()
	tx, ty = tx-0.5, ty-0.5
	x0, y0 := math.Floor(tx), math.Floor(ty)
	fx, fy := float32(tx-x0), float32(ty-y0)
	wrap := func(v, n int) int { return ((v % n) + n) % n }
	var out [4]float32
	for _, c := range [4]struct {
		dx, dy int
		k      float32
	}{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
		px := s.tile.pix[(wrap(int(y0)+c.dy, h)*w+wrap(int(x0)+c.dx, w))*4:]
		for i := range 4 {
			out[i] += px[i] * c.k * s.opacity
		}
	}
	return out
}

// patternAttr follows a pattern's href chain for attr.
func (r *renderer) patternAttr(n *node, attr string) string {
	seen := map[*node]bool{}
	for at := n; at != nil && at.name == "pattern" && !seen[at]; at = r.doc.ids[hrefID(at)] {
		seen[at] = true
		if v, ok := at.attrs[attr]; ok {
			return v
		}
	}
	return ""
}

// patternContent is the first pattern in the href chain with children.
func (r *renderer) patternContent(n *node) *node {
	seen := map[*node]bool{}
	for at := n; at != nil && at.name == "pattern" && !seen[at]; at = r.doc.ids[hrefID(at)] {
		seen[at] = true
		if len(at.children) > 0 {
			return at
		}
	}
	return nil
}

// patternShader resolves pattern n for an element with bounding box bbox
// drawn through ctm; nil paints nothing.
func (r *renderer) patternShader(n *node, bbox rect, ctm Matrix, opacity float64) shader {
	content := r.patternContent(n)
	if content == nil || r.active[n] || r.active[content] {
		return nil
	}
	boxUnits := r.patternAttr(n, "patternUnits") != "userSpaceOnUse"
	boxContent := r.patternAttr(n, "patternContentUnits") == "objectBoundingBox"
	bw, bh := bbox.x1-bbox.x0, bbox.y1-bbox.y0
	if (boxUnits || boxContent) && (!bbox.ok || bw <= 0 || bh <= 0) {
		return nil
	}
	num := func(attr string, ref float64) float64 {
		v, _ := parseLength(r.patternAttr(n, attr), ref, defaultFontSize)
		return v
	}
	var x, y, w, h float64
	if boxUnits {
		vp := viewport{1, 1}
		x, y = bbox.x0+num("x", vp.w)*bw, bbox.y0+num("y", vp.h)*bh
		w, h = num("width", vp.w)*bw, num("height", vp.h)*bh
	} else {
		x, y = num("x", r.viewport.w), num("y", r.viewport.h)
		w, h = num("width", r.viewport.w), num("height", r.viewport.h)
	}
	if !(w > 0 && h > 0) {
		return nil
	}
	// Pattern space to device, and tile coordinates (origin at x, y) to
	// the tile's raster at about device resolution.
	m := ctm.Mul(parseTransform(r.patternAttr(n, "patternTransform"))).Mul(Translate(x, y))
	sx, sy := math.Hypot(m.A, m.B), math.Hypot(m.C, m.D)
	cols := min(max(int(math.Ceil(w*sx)), 1), maxTile)
	rows := min(max(int(math.Ceil(h*sy)), 1), maxTile)
	toRaster := Scale(float64(cols)/w, float64(rows)/h)
	inner := Identity
	if vb, ok := parseViewBox(r.patternAttr(n, "viewBox")); ok {
		inner = viewBoxTransform(vb, r.patternAttr(n, "preserveAspectRatio"), w, h)
	} else if boxContent {
		inner = Scale(bw, bh)
	}
	mi, ok := m.Invert()
	if !ok {
		return nil
	}
	inv := toRaster.Mul(mi)
	tile := newCanvas(image.Rect(0, 0, cols, rows))
	saved := r.bounds
	r.bounds = tile.r
	r.active[n], r.active[content] = true, true
	r.children(content, toRaster.Mul(inner), r.styleOf(content), tile)
	delete(r.active, n)
	delete(r.active, content)
	r.bounds = saved
	return &tileShader{tile: tile, inv: inv, opacity: float32(opacity)}
}
