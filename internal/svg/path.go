package svg

import "math"

// op is one path command in absolute coordinates.
type op uint8

const (
	opMove op = iota
	opLine
	opQuad
	opCubic
	opClose
)

// segment is a command with its points: the end point last.
type segment struct {
	op  op
	pts [3][2]float64
}

// Path is an outline in user units: subpaths of lines and Bézier curves.
type Path []segment

func (p *Path) moveTo(x, y float64) { *p = append(*p, segment{op: opMove, pts: [3][2]float64{{x, y}}}) }
func (p *Path) lineTo(x, y float64) { *p = append(*p, segment{op: opLine, pts: [3][2]float64{{x, y}}}) }
func (p *Path) close()              { *p = append(*p, segment{op: opClose}) }

func (p *Path) quadTo(x1, y1, x, y float64) {
	*p = append(*p, segment{op: opQuad, pts: [3][2]float64{{x1, y1}, {x, y}}})
}

func (p *Path) cubicTo(x1, y1, x2, y2, x, y float64) {
	*p = append(*p, segment{op: opCubic, pts: [3][2]float64{{x1, y1}, {x2, y2}, {x, y}}})
}

// parsePathData parses a d attribute. As SVG requires, a path draws up to
// its first error.
func parsePathData(d string) Path {
	var p Path
	sc := scanner{s: d}
	var cur, start, ctrl [2]float64 // ctrl: the last control point, for S and T
	var prev, cmd byte
	closed := false
	for {
		sc.skipSpace()
		if sc.done() {
			return p
		}
		if c := sc.peek(); c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			cmd = c
			sc.i++
		} else if cmd == 0 {
			return p
		}
		if len(p) == 0 && cmd != 'M' && cmd != 'm' {
			return p
		}
		rel := cmd >= 'a'
		var base [2]float64
		if rel {
			base = cur
		}
		args := func(n int) ([]float64, bool) {
			out := make([]float64, n)
			for i := range out {
				if i > 0 {
					sc.skipSeparators()
				}
				v, ok := sc.number()
				if !ok {
					return nil, false
				}
				out[i] = v
			}
			sc.skipSeparators()
			return out, true
		}
		pt := func(x, y float64) [2]float64 { return [2]float64{base[0] + x, base[1] + y} }
		if closed && cmd|0x20 != 'm' && cmd|0x20 != 'z' {
			// Drawing on after a close starts at the closed subpath's start.
			p.moveTo(start[0], start[1])
		}
		closed = false
		switch cmd | 0x20 {
		case 'z':
			if p[len(p)-1].op != opClose {
				p.close()
			}
			cur, ctrl, closed = start, start, true
			sc.skipSeparators()
			prev = cmd
			continue
		case 'm', 'l', 't':
			a, ok := args(2)
			if !ok {
				return p
			}
			e := pt(a[0], a[1])
			switch cmd | 0x20 {
			case 'm':
				p.moveTo(e[0], e[1])
				start = e
				ctrl = e
			case 'l':
				p.lineTo(e[0], e[1])
				ctrl = e
			case 't':
				c := cur
				if q := prev | 0x20; q == 'q' || q == 't' {
					c = [2]float64{2*cur[0] - ctrl[0], 2*cur[1] - ctrl[1]}
				}
				p.quadTo(c[0], c[1], e[0], e[1])
				ctrl = c
			}
			cur = e
		case 'h', 'v':
			a, ok := args(1)
			if !ok {
				return p
			}
			if cmd|0x20 == 'h' {
				cur[0] = base[0] + a[0]
			} else {
				cur[1] = base[1] + a[0]
			}
			p.lineTo(cur[0], cur[1])
			ctrl = cur
		case 'c', 's', 'q':
			n := map[byte]int{'c': 6, 's': 4, 'q': 4}[cmd|0x20]
			a, ok := args(n)
			if !ok {
				return p
			}
			switch cmd | 0x20 {
			case 'c':
				c1, c2, e := pt(a[0], a[1]), pt(a[2], a[3]), pt(a[4], a[5])
				p.cubicTo(c1[0], c1[1], c2[0], c2[1], e[0], e[1])
				ctrl, cur = c2, e
			case 's':
				c1 := cur
				if q := prev | 0x20; q == 'c' || q == 's' {
					c1 = [2]float64{2*cur[0] - ctrl[0], 2*cur[1] - ctrl[1]}
				}
				c2, e := pt(a[0], a[1]), pt(a[2], a[3])
				p.cubicTo(c1[0], c1[1], c2[0], c2[1], e[0], e[1])
				ctrl, cur = c2, e
			case 'q':
				c, e := pt(a[0], a[1]), pt(a[2], a[3])
				p.quadTo(c[0], c[1], e[0], e[1])
				ctrl, cur = c, e
			}
		case 'a':
			rx, ok1 := sc.number()
			sc.skipSeparators()
			ry, ok2 := sc.number()
			sc.skipSeparators()
			rot, ok3 := sc.number()
			large, ok4 := sc.flag()
			sweep, ok5 := sc.flag()
			sc.skipSeparators()
			e, ok6 := args(2)
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
				return p
			}
			end := pt(e[0], e[1])
			p.arcTo(cur, rx, ry, rot, large, sweep, end)
			cur, ctrl = end, end
		default:
			return p
		}
		prev = cmd
		switch cmd { // coordinates after a move are implicit line-tos
		case 'M':
			cmd = 'L'
		case 'm':
			cmd = 'l'
		}
	}
}

// arcTo appends an elliptical arc from cur to end as cubic curves (SVG
// implementation notes F.6: out-of-range radii are corrected).
func (p *Path) arcTo(cur [2]float64, rx, ry, rotDeg float64, large, sweep bool, end [2]float64) {
	if cur == end {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.lineTo(end[0], end[1])
		return
	}
	sinPhi, cosPhi := math.Sincos(rotDeg * math.Pi / 180)
	dx, dy := (cur[0]-end[0])/2, (cur[1]-end[1])/2
	x1 := cosPhi*dx + sinPhi*dy
	y1 := -sinPhi*dx + cosPhi*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	coef := math.Sqrt(max(num/den, 0))
	if large == sweep {
		coef = -coef
	}
	cx1, cy1 := coef*rx*y1/ry, -coef*ry*x1/rx
	cx := cosPhi*cx1 - sinPhi*cy1 + (cur[0]+end[0])/2
	cy := sinPhi*cx1 + cosPhi*cy1 + (cur[1]+end[1])/2
	angle := func(ux, uy, vx, vy float64) float64 { return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy) }
	t1 := angle(1, 0, (x1-cx1)/rx, (y1-cy1)/ry)
	dt := angle((x1-cx1)/rx, (y1-cy1)/ry, (-x1-cx1)/rx, (-y1-cy1)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dt) / (math.Pi / 2)))
	step := dt / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	point := func(t float64) (x, y, tx, ty float64) {
		s, c := math.Sincos(t)
		ex, ey := rx*c, ry*s
		dxp, dyp := -rx*s, ry*c
		return cosPhi*ex - sinPhi*ey + cx, sinPhi*ex + cosPhi*ey + cy,
			cosPhi*dxp - sinPhi*dyp, sinPhi*dxp + cosPhi*dyp
	}
	for i := range n {
		a, b := t1+float64(i)*step, t1+float64(i+1)*step
		x0, y0, tx0, ty0 := point(a)
		x3, y3, tx3, ty3 := point(b)
		if i == n-1 {
			x3, y3 = end[0], end[1]
		}
		p.cubicTo(x0+k*tx0, y0+k*ty0, x3-k*tx3, y3-k*ty3, x3, y3)
	}
}
