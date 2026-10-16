package svg

import "math"

// The stroker turns polylines into filled outlines: per subpath one
// polygon walking the left side forward and the right side back (two
// loops for a closed subpath), with joins on the outer side and the
// vertex as pivot on the inner one, as Skia's stroker does. Drawn with
// the nonzero rule, the pieces overlap without seams and every edge of
// the stroke is a single edge, so anti-aliasing stays exact.

type stroker struct {
	hw        float64 // half the width
	cap, join string
	limit     float64 // miter limit
	tol       float64 // flattening tolerance for round parts
	out       [][][2]float64
}

func vsub(a, b [2]float64) [2]float64           { return [2]float64{a[0] - b[0], a[1] - b[1]} }
func vadd(a, b [2]float64) [2]float64           { return [2]float64{a[0] + b[0], a[1] + b[1]} }
func vscale(a [2]float64, s float64) [2]float64 { return [2]float64{a[0] * s, a[1] * s} }
func vdot(a, b [2]float64) float64              { return a[0]*b[0] + a[1]*b[1] }

// stroke outlines one polyline.
func (s *stroker) stroke(l polyline) {
	pts := dedupe(l.pts)
	if l.closed && len(pts) > 1 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	if len(pts) == 1 {
		s.dot(pts[0])
		return
	}
	n := len(pts)
	segs := n - 1
	if l.closed {
		segs = n
	}
	normals := make([][2]float64, segs)
	dirs := make([][2]float64, segs)
	for i := range segs {
		d := vsub(pts[(i+1)%n], pts[i])
		d = vscale(d, 1/math.Hypot(d[0], d[1]))
		dirs[i] = d
		normals[i] = [2]float64{-d[1] * s.hw, d[0] * s.hw}
	}
	if l.closed {
		for _, side := range [2]float64{1, -1} {
			var loop [][2]float64
			for k := range n {
				prev := (k - 1 + segs) % segs
				loop = s.joinAt(loop, pts[k], dirs[prev], dirs[k], normals[prev], normals[k], side)
			}
			if side < 0 {
				reverse(loop)
			}
			s.out = append(s.out, loop)
		}
		return
	}
	var poly [][2]float64
	poly = append(poly, vadd(pts[0], normals[0]))
	for k := 1; k < n-1; k++ {
		poly = s.joinAt(poly, pts[k], dirs[k-1], dirs[k], normals[k-1], normals[k], 1)
	}
	last := segs - 1
	poly = append(poly, vadd(pts[n-1], normals[last]))
	poly = s.capAt(poly, pts[n-1], dirs[last], normals[last])
	for k := n - 2; k >= 1; k-- {
		back := func(i int) [2]float64 { return vscale(dirs[i], -1) }
		poly = s.joinAt(poly, pts[k], back(k), back(k-1), vscale(normals[k], -1), vscale(normals[k-1], -1), 1)
	}
	poly = append(poly, vsub(pts[0], normals[0]))
	poly = s.capAt(poly, pts[0], vscale(dirs[0], -1), vscale(normals[0], -1))
	s.out = append(s.out, poly)
}

// joinAt adds the corner at p between incoming direction d0 and outgoing
// d1 on the side of sign side (+1: the normals' side).
func (s *stroker) joinAt(out [][2]float64, p, d0, d1, n0, n1 [2]float64, side float64) [][2]float64 {
	a, b := vadd(p, vscale(n0, side)), vadd(p, vscale(n1, side))
	turn := vdot(vscale(n0, side), d1)
	if math.Abs(d0[0]*d1[1]-d0[1]*d1[0]) < 1e-9 && vdot(d0, d1) > 0 {
		return append(out, a) // straight on
	}
	if turn > 0 {
		return append(out, a, p, b) // the inner side pivots on the vertex
	}
	switch s.join {
	case "round":
		return s.arc(out, p, vscale(n0, side), vscale(n1, side))
	case "bevel":
		return append(out, a, b)
	}
	cos := vdot(n0, n1) / (s.hw * s.hw)
	ratio := math.Sqrt(2 / math.Max(1+cos, 1e-12))
	if ratio > s.limit {
		return append(out, a, b)
	}
	mid := vadd(n0, n1)
	mid = vscale(mid, side*s.hw*ratio/math.Hypot(mid[0], mid[1]))
	return append(out, a, vadd(p, mid), b)
}

// capAt adds the cap at end point p of a side ending in direction d, from
// p+n round to p−n.
func (s *stroker) capAt(out [][2]float64, p, d, n [2]float64) [][2]float64 {
	switch s.cap {
	case "square":
		ext := vscale(d, s.hw)
		return append(out, vadd(vadd(p, n), ext), vadd(vsub(p, n), ext), vsub(p, n))
	case "round":
		out = s.arc(out, p, n, vscale(d, s.hw))
		return s.arc(out, p, vscale(d, s.hw), vscale(n, -1))
	}
	return append(out, vsub(p, n))
}

// arc adds points on the circle about p from offset u to offset v, the
// shorter way.
func (s *stroker) arc(out [][2]float64, p, u, v [2]float64) [][2]float64 {
	a0 := math.Atan2(u[1], u[0])
	sweep := math.Atan2(u[0]*v[1]-u[1]*v[0], vdot(u, v))
	step := 2 * math.Acos(math.Max(1-s.tol/s.hw, -1))
	steps := int(math.Ceil(math.Abs(sweep) / math.Max(step, 1e-3)))
	steps = min(max(steps, 1), 64)
	for i := range steps + 1 {
		t := a0 + sweep*float64(i)/float64(steps)
		out = append(out, [2]float64{p[0] + s.hw*math.Cos(t), p[1] + s.hw*math.Sin(t)})
	}
	return out
}

// dot is a zero-length subpath: round and square caps still draw.
func (s *stroker) dot(p [2]float64) {
	switch s.cap {
	case "round":
		s.out = append(s.out, s.arc(s.arc(nil, p, [2]float64{s.hw, 0}, [2]float64{0, s.hw}), p, [2]float64{0, s.hw}, [2]float64{-s.hw, 0}))
		s.out[len(s.out)-1] = s.arc(s.out[len(s.out)-1], p, [2]float64{-s.hw, 0}, [2]float64{0, -s.hw})
		s.out[len(s.out)-1] = s.arc(s.out[len(s.out)-1], p, [2]float64{0, -s.hw}, [2]float64{s.hw, 0})
	case "square":
		h := s.hw
		s.out = append(s.out, [][2]float64{{p[0] - h, p[1] - h}, {p[0] + h, p[1] - h}, {p[0] + h, p[1] + h}, {p[0] - h, p[1] + h}})
	}
}

func dedupe(pts [][2]float64) [][2]float64 {
	out := pts[:0:0]
	for i, p := range pts {
		if i == 0 || math.Hypot(p[0]-out[len(out)-1][0], p[1]-out[len(out)-1][1]) > 1e-9 {
			out = append(out, p)
		}
	}
	return out
}

func reverse(pts [][2]float64) {
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
}

// dash splits polylines into the dashes of pattern (lengths alternating
// on and off) shifted by offset.
func dash(lines []polyline, pattern []float64, offset float64) []polyline {
	total := 0.0
	for _, v := range pattern {
		total += v
	}
	var out []polyline
	for _, l := range lines {
		pts := l.pts
		if l.closed && len(pts) > 0 {
			pts = append(append([][2]float64{}, pts...), pts[0])
		}
		i := 0
		pos := math.Mod(offset, total)
		if pos < 0 {
			pos += total
		}
		for pos >= pattern[i] { // find where the offset falls in the pattern
			pos -= pattern[i]
			i = (i + 1) % len(pattern)
		}
		left := pattern[i] - pos // what remains of the current dash or gap
		var cur *polyline
		if i%2 == 0 {
			out = append(out, polyline{pts: [][2]float64{pts[0]}})
			cur = &out[len(out)-1]
		}
		for k := 1; k < len(pts); k++ {
			a, b := pts[k-1], pts[k]
			segLen := math.Hypot(b[0]-a[0], b[1]-a[1])
			at := 0.0
			for segLen-at > left {
				at += left
				q := lerp2(a, b, at/segLen)
				if cur != nil {
					cur.pts = append(cur.pts, q)
					cur = nil
				} else {
					out = append(out, polyline{pts: [][2]float64{q}})
					cur = &out[len(out)-1]
				}
				i = (i + 1) % len(pattern)
				left = pattern[i]
			}
			left -= segLen - at
			if cur != nil {
				cur.pts = append(cur.pts, b)
			}
		}
	}
	return out
}
