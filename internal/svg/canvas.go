package svg

import (
	"image"
	"math"
)

// canvas is premultiplied RGBA in float32, so layers and translucent
// paints round once, at the end.
type canvas struct {
	r   image.Rectangle
	pix []float32
}

func newCanvas(r image.Rectangle) *canvas {
	return &canvas{r: r, pix: make([]float32, r.Dx()*r.Dy()*4)}
}

func (c *canvas) offset(x, y int) int { return ((y-c.r.Min.Y)*c.r.Dx() + x - c.r.Min.X) * 4 }

// paint composites sh over the canvas where cv covers it (source over).
func (c *canvas) paint(cv *cov, sh shader) {
	if cv == nil {
		return
	}
	s, isSolid := sh.(solid)
	r := cv.r.Intersect(c.r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			k := cv.at(x, y)
			if k == 0 {
				continue
			}
			src := [4]float32(s)
			if !isSolid {
				src = sh.at(float64(x)+0.5, float64(y)+0.5)
			}
			o := c.offset(x, y)
			inv := 1 - src[3]*k
			for i := range 4 {
				c.pix[o+i] = src[i]*k + c.pix[o+i]*inv
			}
		}
	}
}

// composite draws layer over the canvas at opacity, each pixel scaled by
// mask's value when there is a mask.
func (c *canvas) composite(layer *canvas, opacity float32, mask []float32) {
	for i := 0; i < len(c.pix); i += 4 {
		k := opacity
		if mask != nil {
			k *= mask[i/4]
		}
		if k == 0 || layer.pix[i+3] == 0 {
			continue
		}
		inv := 1 - layer.pix[i+3]*k
		for j := range 4 {
			c.pix[i+j] = layer.pix[i+j]*k + c.pix[i+j]*inv
		}
	}
}

// alphaMask is the layer's coverage per pixel, for clipping.
func (c *canvas) alphaMask() []float32 {
	m := make([]float32, len(c.pix)/4)
	for i := range m {
		m[i] = c.pix[i*4+3]
	}
	return m
}

// luminanceMask is SVG's luminance mask of the layer: the luminance of
// each straight colour times its alpha (as resvg, on sRGB values).
func (c *canvas) luminanceMask() []float32 {
	m := make([]float32, len(c.pix)/4)
	for i := range m {
		p := c.pix[i*4 : i*4+4]
		// Premultiplied channels already carry the alpha factor.
		m[i] = min(max(0.2125*p[0]+0.7154*p[1]+0.0721*p[2], 0), 1)
	}
	return m
}

// drawOver composites the canvas onto dst (premultiplied RGBA8), rounding
// to the nearest level.
func (c *canvas) drawOver(dst *image.RGBA) {
	r := c.r.Intersect(dst.Rect)
	q := func(v float32) uint8 { return uint8(math.Round(float64(min(max(v, 0), 1) * 255))) }
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			s := c.pix[c.offset(x, y):]
			d := dst.Pix[dst.PixOffset(x, y):]
			inv := 1 - s[3]
			for i := range 4 {
				d[i] = q(s[i] + float32(d[i])/255*inv)
			}
		}
	}
}
