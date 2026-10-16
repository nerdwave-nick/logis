package render

// Map is the same paint with every stop's colour through color and, for a
// gradient, its line's ends through point (a whole scene moved and faded,
// as a panel opens and closes).
func (p Paint) Map(color func(Color) Color, point func([2]float32) [2]float32) Paint {
	for i := range p.n {
		p.stops[i] = color(p.stops[i])
	}
	if p.n > 1 {
		p.Start, p.End = point(p.Start), point(p.End)
	}
	return p
}
