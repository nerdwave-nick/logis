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

// fillTolerance is how far, in pixels, flattened curves may stray from
// the exact ones. vector's own flattening is coarser (made for glyphs)
// and loses area on round shapes, so curves arrive as lines.
const fillTolerance = 0.02

// fillCoverage rasterises p through m within bounds by the nonzero rule;
// the even-odd rule combines each subpath's coverage by exclusive or,
// exact for subpaths that do not cross themselves.
func fillCoverage(p Path, m Matrix, evenOdd bool, bounds image.Rectangle) *cov {
	r := deviceBounds(p.bounds(m), bounds)
	if r.Empty() {
		return nil
	}
	if !evenOdd {
		return rasterise(r, func(z *vector.Rasterizer, t Matrix, k float64) { feedPath(z, p, t.Mul(m), k) })
	}
	var acc *cov
	for _, sub := range p.subpaths() {
		c := rasterise(r, func(z *vector.Rasterizer, t Matrix, k float64) { feedPath(z, sub, t.Mul(m), k) })
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
// by the nonzero rule.
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
	return rasterise(r, func(z *vector.Rasterizer, t Matrix, _ float64) {
		for _, poly := range polys {
			for i, q := range poly {
				x, y := t.Apply(q[0], q[1])
				if i == 0 {
					z.MoveTo(float32(x), float32(y))
				} else {
					z.LineTo(float32(x), float32(y))
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

// subpixels is how many subpixels per side coverage is accumulated at for
// an area of n device pixels: vector's coverage is the magnitude of the
// summed signed area, which is the nonzero rule only where no more than
// one layer of outline overlaps; where overlapping parts (inner stroke
// joins, overlapping subpaths) meet an edge it over-counts. Clamping
// finer cells and averaging them cuts that error k-fold and leaves
// coverage of non-overlapping outlines as it was.
func subpixels(n int) int {
	switch {
	case n <= 256*256:
		return 4
	case n <= 512*512:
		return 2
	}
	return 1
}

// rasterise accumulates coverage over r: feed gets the matrix from device
// pixels to the subpixel grid and the subpixels per pixel.
func rasterise(r image.Rectangle, feed func(z *vector.Rasterizer, toRaster Matrix, k float64)) *cov {
	k := subpixels(r.Dx() * r.Dy())
	z := vector.NewRasterizer(r.Dx()*k, r.Dy()*k)
	feed(z, Scale(float64(k), float64(k)).Mul(Translate(-float64(r.Min.X), -float64(r.Min.Y))), float64(k))
	mask := image.NewAlpha(z.Bounds())
	z.DrawOp = draw.Src
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	c := &cov{r: r, a: make([]float32, r.Dx()*r.Dy())}
	w := r.Dx()
	norm := float32(255 * k * k)
	for y := range r.Dy() * k {
		row := mask.Pix[y*mask.Stride : y*mask.Stride+w*k]
		out := c.a[(y/k)*w : (y/k+1)*w]
		for x, v := range row {
			out[x/k] += float32(v) / norm
		}
	}
	return c
}

// feedPath gives p through m (onto the subpixel grid, k subpixels per
// pixel) to the rasteriser, closing every subpath (vector's MoveTo does
// not).
func feedPath(z *vector.Rasterizer, p Path, m Matrix, k float64) {
	for _, l := range p.flatten(m, fillTolerance*k) {
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
