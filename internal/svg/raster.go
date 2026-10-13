package svg

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// cov is anti-aliased coverage over a rectangle of the canvas, 0 to
// 1 per pixel.
type cov struct {
	r image.Rectangle
	a []float32
}

func (c *cov) at(x, y int) float32 {
	if !(image.Point{x, y}.In(c.r)) {
		return 0
	}
	return c.a[(y-c.r.Min.Y)*c.r.Dx()+x-c.r.Min.X]
}

// fillCoverage rasterises p through m within bounds. vector accumulates
// signed area and takes its magnitude, which is the nonzero rule; the
// even-odd rule combines each subpath's coverage by exclusive or, exact
// for subpaths that do not cross themselves.
func fillCoverage(p Path, m Matrix, evenOdd bool, bounds image.Rectangle) *cov {
	r := deviceBounds(p.bounds(m), bounds)
	if r.Empty() {
		return nil
	}
	if !evenOdd {
		return rasterise(r, func(z *vector.Rasterizer, o [2]float64) { feedPath(z, p, m, o) })
	}
	var acc *cov
	for _, sub := range p.subpaths() {
		c := rasterise(r, func(z *vector.Rasterizer, o [2]float64) { feedPath(z, sub, m, o) })
		if acc == nil {
			acc = c
			continue
		}
		for i, v := range c.a {
			acc.a[i] = acc.a[i] + v - 2*acc.a[i]*v
		}
	}
	return acc
}

// polygonCoverage rasterises closed polygons (in device space) together
// with the nonzero rule.
func polygonCoverage(polys [][][2]float64, bounds image.Rectangle) *cov {
	var b rect
	for _, poly := range polys {
		for _, q := range poly {
			b.add(q[0], q[1])
		}
	}
	r := deviceBounds(b, bounds)
	if r.Empty() {
		return nil
	}
	return rasterise(r, func(z *vector.Rasterizer, o [2]float64) {
		for _, poly := range polys {
			for i, q := range poly {
				x, y := float32(q[0]-o[0]), float32(q[1]-o[1])
				if i == 0 {
					z.MoveTo(x, y)
				} else {
					z.LineTo(x, y)
				}
			}
			if len(poly) > 0 {
				z.ClosePath()
			}
		}
	})
}

func deviceBounds(b rect, bounds image.Rectangle) image.Rectangle {
	if !b.ok || math.IsNaN(b.x0+b.y0+b.x1+b.y1) {
		return image.Rectangle{}
	}
	clamp := func(v float64) int { return int(max(min(v, 1<<20), -1<<20)) }
	r := image.Rect(clamp(math.Floor(b.x0)), clamp(math.Floor(b.y0)), clamp(math.Ceil(b.x1))+1, clamp(math.Ceil(b.y1))+1)
	return r.Intersect(bounds)
}

func rasterise(r image.Rectangle, feed func(*vector.Rasterizer, [2]float64)) *cov {
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	feed(z, [2]float64{float64(r.Min.X), float64(r.Min.Y)})
	mask := image.NewAlpha(image.Rect(0, 0, r.Dx(), r.Dy()))
	z.DrawOp = draw.Src
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	c := &cov{r: r, a: make([]float32, len(mask.Pix))}
	for i, v := range mask.Pix {
		c.a[i] = float32(v) / 255
	}
	return c
}

// fillTolerance is how far, in pixels, flattened curves may stray from
// the exact ones. vector's own flattening is coarser (made for glyphs)
// and loses area on round shapes, so curves arrive as lines.
const fillTolerance = 0.02

// feedPath gives p, through m and shifted by −o, to the rasteriser,
// closing every subpath (vector's MoveTo does not).
func feedPath(z *vector.Rasterizer, p Path, m Matrix, o [2]float64) {
	shifted := Translate(-o[0], -o[1]).Mul(m)
	for _, l := range p.flatten(shifted, fillTolerance) {
		for i, q := range l.pts {
			if i == 0 {
				z.MoveTo(float32(q[0]), float32(q[1]))
			} else {
				z.LineTo(float32(q[0]), float32(q[1]))
			}
		}
		z.ClosePath()
	}
}

// subpaths splits p at each move.
func (p Path) subpaths() []Path {
	var out []Path
	for i, s := range p {
		if s.op == opMove || i == 0 {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], s)
	}
	return out
}
