package svg

import (
	"math"
	"strings"
)

// Matrix is an affine transform mapping (x, y) to
// (A·x + C·y + E, B·x + D·y + F), as SVG's matrix(a b c d e f).
type Matrix struct{ A, B, C, D, E, F float64 }

// Identity is the transform that changes nothing.
var Identity = Matrix{A: 1, D: 1}

// Translate moves by (x, y).
func Translate(x, y float64) Matrix { return Matrix{1, 0, 0, 1, x, y} }

// Scale scales by (x, y) about the origin.
func Scale(x, y float64) Matrix { return Matrix{x, 0, 0, y, 0, 0} }

// rotate turns by deg degrees about the origin.
func rotate(deg float64) Matrix {
	s, c := math.Sincos(deg * math.Pi / 180)
	return Matrix{c, s, -s, c, 0, 0}
}

// Mul is the transform applying n first, then m.
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.C*n.B,
		B: m.B*n.A + m.D*n.B,
		C: m.A*n.C + m.C*n.D,
		D: m.B*n.C + m.D*n.D,
		E: m.A*n.E + m.C*n.F + m.E,
		F: m.B*n.E + m.D*n.F + m.F,
	}
}

// Apply maps a point.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// det is the determinant; zero for a transform that flattens everything.
func (m Matrix) det() float64 { return m.A*m.D - m.B*m.C }

// Invert is the inverse transform; !ok when m is singular.
func (m Matrix) Invert() (Matrix, bool) {
	d := m.det()
	if d == 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		return Matrix{}, false
	}
	return Matrix{
		A: m.D / d, B: -m.B / d, C: -m.C / d, D: m.A / d,
		E: (m.C*m.F - m.D*m.E) / d,
		F: (m.B*m.E - m.A*m.F) / d,
	}, true
}

// expansion is the largest factor m stretches a length by, for choosing
// curve flattening tolerances in user space.
func (m Matrix) expansion() float64 {
	return max(math.Hypot(m.A, m.B), math.Hypot(m.C, m.D), 1e-9)
}

// parseTransform parses a transform list such as "translate(4 2)
// rotate(45, 8 8)". An invalid list is the identity, as an invalid
// attribute value is ignored.
func parseTransform(s string) Matrix {
	m := Identity
	sc := scanner{s: s}
	for {
		sc.skipSeparators()
		if sc.done() {
			return m
		}
		name := sc.ident()
		sc.skipSpace()
		if name == "" || !sc.eat('(') {
			return Identity
		}
		var args []float64
		for {
			sc.skipSeparators()
			if sc.eat(')') {
				break
			}
			v, ok := sc.number()
			if !ok {
				return Identity
			}
			args = append(args, v)
		}
		t, ok := transformFunction(strings.TrimSpace(name), args)
		if !ok {
			return Identity
		}
		m = m.Mul(t)
	}
}

func transformFunction(name string, a []float64) (Matrix, bool) {
	n := len(a)
	switch {
	case name == "matrix" && n == 6:
		return Matrix{a[0], a[1], a[2], a[3], a[4], a[5]}, true
	case name == "translate" && (n == 1 || n == 2):
		if n == 1 {
			return Translate(a[0], 0), true
		}
		return Translate(a[0], a[1]), true
	case name == "scale" && (n == 1 || n == 2):
		if n == 1 {
			return Scale(a[0], a[0]), true
		}
		return Scale(a[0], a[1]), true
	case name == "rotate" && n == 1:
		return rotate(a[0]), true
	case name == "rotate" && n == 3:
		return Translate(a[1], a[2]).Mul(rotate(a[0])).Mul(Translate(-a[1], -a[2])), true
	case name == "skewX" && n == 1:
		return Matrix{1, 0, math.Tan(a[0] * math.Pi / 180), 1, 0, 0}, true
	case name == "skewY" && n == 1:
		return Matrix{1, math.Tan(a[0] * math.Pi / 180), 0, 1, 0, 0}, true
	}
	return Matrix{}, false
}
