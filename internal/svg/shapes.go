package svg

import (
	"math"
	"strings"
)

// kappa places cubic control points for a quarter ellipse.
const kappa = 0.5522847498307936

// viewport is the size percentages refer to.
type viewport struct{ w, h float64 }

func (v viewport) diagonal() float64 { return math.Sqrt((v.w*v.w + v.h*v.h) / 2) }

// shapePath is the outline of a basic shape or path element; !ok when it
// draws nothing (a zero or negative size, an empty path).
func shapePath(n *node, vp viewport, em float64) (Path, bool) {
	length := func(attr string, ref float64) float64 {
		v, _ := parseLength(n.attr(attr), ref, em)
		return v
	}
	var p Path
	switch n.name {
	case "path":
		p = parsePathData(n.attr("d"))
	case "rect":
		x, y := length("x", vp.w), length("y", vp.h)
		w, h := length("width", vp.w), length("height", vp.h)
		if !(w > 0 && h > 0) {
			return nil, false
		}
		rx, okx := parseLength(n.attr("rx"), vp.w, em)
		ry, oky := parseLength(n.attr("ry"), vp.h, em)
		okx, oky = okx && rx >= 0, oky && ry >= 0
		switch {
		case !okx && !oky:
			rx, ry = 0, 0
		case !okx:
			rx = ry
		case !oky:
			ry = rx
		}
		rx, ry = min(rx, w/2), min(ry, h/2)
		p.roundedRect(x, y, w, h, rx, ry)
	case "circle":
		r := length("r", vp.diagonal())
		if !(r > 0) {
			return nil, false
		}
		p.ellipse(length("cx", vp.w), length("cy", vp.h), r, r)
	case "ellipse":
		rx, ry := length("rx", vp.w), length("ry", vp.h)
		if !(rx > 0 && ry > 0) {
			return nil, false
		}
		p.ellipse(length("cx", vp.w), length("cy", vp.h), rx, ry)
	case "line":
		p.moveTo(length("x1", vp.w), length("y1", vp.h))
		p.lineTo(length("x2", vp.w), length("y2", vp.h))
	case "polyline", "polygon":
		v, _ := numbers(n.attr("points"))
		if len(v) < 4 {
			return nil, false
		}
		for i := 0; i+1 < len(v); i += 2 {
			if i == 0 {
				p.moveTo(v[0], v[1])
			} else {
				p.lineTo(v[i], v[i+1])
			}
		}
		if n.name == "polygon" {
			p.close()
		}
	default:
		return nil, false
	}
	return p, len(p) > 1
}

func (p *Path) roundedRect(x, y, w, h, rx, ry float64) {
	if rx <= 0 || ry <= 0 {
		p.moveTo(x, y)
		p.lineTo(x+w, y)
		p.lineTo(x+w, y+h)
		p.lineTo(x, y+h)
		p.close()
		return
	}
	kx, ky := rx*kappa, ry*kappa
	r, b := x+w, y+h
	p.moveTo(x+rx, y)
	p.lineTo(r-rx, y)
	p.cubicTo(r-rx+kx, y, r, y+ry-ky, r, y+ry)
	p.lineTo(r, b-ry)
	p.cubicTo(r, b-ry+ky, r-rx+kx, b, r-rx, b)
	p.lineTo(x+rx, b)
	p.cubicTo(x+rx-kx, b, x, b-ry+ky, x, b-ry)
	p.lineTo(x, y+ry)
	p.cubicTo(x, y+ry-ky, x+rx-kx, y, x+rx, y)
	p.close()
}

func (p *Path) ellipse(cx, cy, rx, ry float64) {
	kx, ky := rx*kappa, ry*kappa
	p.moveTo(cx+rx, cy)
	p.cubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.cubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.cubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.cubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.close()
}

// polyline is a flattened subpath.
type polyline struct {
	pts    [][2]float64
	closed bool
}

// flatten turns p into polylines through m, curves within tol of the
// exact ones (in m's units).
func (p Path) flatten(m Matrix, tol float64) []polyline {
	var out []polyline
	var cur *polyline
	var pen, start [2]float64
	apply := func(q [2]float64) [2]float64 {
		x, y := m.Apply(q[0], q[1])
		return [2]float64{x, y}
	}
	ensure := func() {
		if cur == nil {
			out = append(out, polyline{pts: [][2]float64{pen}})
			cur = &out[len(out)-1]
		}
	}
	for _, s := range p {
		switch s.op {
		case opMove:
			cur = nil
			pen = apply(s.pts[0])
			start = pen
		case opLine:
			ensure()
			pen = apply(s.pts[0])
			cur.pts = append(cur.pts, pen)
		case opQuad, opCubic:
			ensure()
			c1, c2 := apply(s.pts[0]), apply(s.pts[1])
			var e [2]float64
			if s.op == opQuad {
				e = c2
				cur.pts = flattenCubic(cur.pts, pen, lerp2(pen, c1, 2.0/3), lerp2(e, c1, 2.0/3), e, tol)
			} else {
				e = apply(s.pts[2])
				cur.pts = flattenCubic(cur.pts, pen, c1, c2, e, tol)
			}
			pen = e
		case opClose:
			if cur != nil {
				cur.closed = true
				cur = nil
			}
			pen = start
		}
	}
	return out
}

func lerp2(a, b [2]float64, t float64) [2]float64 {
	return [2]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}
}

// flattenCubic appends the cubic's points after p0, as many as Wang's
// formula needs for tolerance tol.
func flattenCubic(out [][2]float64, p0, p1, p2, p3 [2]float64, tol float64) [][2]float64 {
	dd := 0.0
	for _, q := range [2][3][2]float64{{p0, p1, p2}, {p1, p2, p3}} {
		dd = max(dd, math.Hypot(q[0][0]-2*q[1][0]+q[2][0], q[0][1]-2*q[1][1]+q[2][1]))
	}
	n := int(math.Ceil(math.Sqrt(0.75 * dd / tol)))
	n = min(max(n, 1), 500)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		out = append(out, [2]float64{
			a*p0[0] + b*p1[0] + c*p2[0] + d*p3[0],
			a*p0[1] + b*p1[1] + c*p2[1] + d*p3[1],
		})
	}
	return out
}

// rect is a bounding box; empty when nothing was added.
type rect struct {
	x0, y0, x1, y1 float64
	ok             bool
}

func (r *rect) add(x, y float64) {
	if !r.ok {
		*r = rect{x, y, x, y, true}
		return
	}
	r.x0, r.y0, r.x1, r.y1 = min(r.x0, x), min(r.y0, y), max(r.x1, x), max(r.y1, y)
}

func (r *rect) union(o rect) {
	if o.ok {
		r.add(o.x0, o.y0)
		r.add(o.x1, o.y1)
	}
}

// bounds is p's tight bounding box through m.
func (p Path) bounds(m Matrix) rect {
	var r rect
	for _, l := range p.flatten(m, 0.01) {
		for _, q := range l.pts {
			r.add(q[0], q[1])
		}
	}
	return r
}

// bboxUnits maps the unit square onto r, for objectBoundingBox units.
func bboxUnits(r rect) Matrix {
	return Matrix{r.x1 - r.x0, 0, 0, r.y1 - r.y0, r.x0, r.y0}
}

// hrefID is the element an href names within the document.
func hrefID(n *node) string {
	return strings.TrimPrefix(strings.TrimSpace(n.attr("href")), "#")
}
