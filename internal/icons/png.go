package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// PNG icons: decoded by image/png into premultiplied RGBA8, then resampled
// as Rust v1 did with tiny-skia: halved with 2×2 averaging while at least
// twice too big, so large downscales skip no source pixels, then fitted
// with a bicubic (Mitchell–Netravali, B = C = 1/3) filter on premultiplied
// values, the edges clamped.

func decodePNG(data []byte, pixels int) (*image.RGBA, []string, error) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	src := premultiply(decoded)
	if src.Rect.Dx() == pixels && src.Rect.Dy() == pixels {
		return src, nil, nil
	}
	for min(src.Rect.Dx(), src.Rect.Dy()) >= 2*pixels {
		src = halve(src)
	}
	return bicubicFit(src, pixels), nil, nil
}

// premultiply converts any decoded PNG to premultiplied RGBA8, rounding
// each channel × alpha / 255 to the nearest level (16-bit samples keep
// their high byte first).
func premultiply(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	nrgba, _ := img.(*image.NRGBA)
	for y := range b.Dy() {
		for x := range b.Dx() {
			var c color.NRGBA
			if nrgba != nil {
				o := nrgba.PixOffset(b.Min.X+x, b.Min.Y+y)
				c = color.NRGBA{nrgba.Pix[o], nrgba.Pix[o+1], nrgba.Pix[o+2], nrgba.Pix[o+3]}
			} else {
				c = nrgbaOf(img.At(b.Min.X+x, b.Min.Y+y))
			}
			o := out.PixOffset(x, y)
			a := uint32(c.A)
			out.Pix[o] = uint8((uint32(c.R)*a + 127) / 255)
			out.Pix[o+1] = uint8((uint32(c.G)*a + 127) / 255)
			out.Pix[o+2] = uint8((uint32(c.B)*a + 127) / 255)
			out.Pix[o+3] = c.A
		}
	}
	return out
}

// nrgbaOf is a colour's straight 8-bit value, 16-bit channels cut to their
// high byte.
func nrgbaOf(c color.Color) color.NRGBA {
	switch v := c.(type) {
	case color.NRGBA:
		return v
	case color.NRGBA64:
		return color.NRGBA{uint8(v.R >> 8), uint8(v.G >> 8), uint8(v.B >> 8), uint8(v.A >> 8)}
	case color.Gray:
		return color.NRGBA{v.Y, v.Y, v.Y, 255}
	case color.Gray16:
		y := uint8(v.Y >> 8)
		return color.NRGBA{y, y, y, 255}
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// halve is src at half size (sides rounded up): each pixel the average of
// a 2×2 block, rows and columns beyond the edge repeating the last one.
func halve(src *image.RGBA) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, (w+1)/2, (h+1)/2))
	for y := range out.Rect.Dy() {
		for x := range out.Rect.Dx() {
			o := out.PixOffset(x, y)
			for c := range 4 {
				sum := 0
				for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					sx, sy := min(2*x+d[0], w-1), min(2*y+d[1], h-1)
					sum += int(src.Pix[src.PixOffset(sx, sy)+c])
				}
				out.Pix[o+c] = uint8((sum + 2) / 4)
			}
		}
	}
	return out
}

// mitchell is the bicubic weight of a sample t away (Skia's bicubic_near
// for t < 1, bicubic_far for 1 ≤ t < 2).
func mitchell(t float64) float64 {
	t = math.Abs(t)
	switch {
	case t < 1:
		return (7.0/6*t-2)*t*t + 8.0/9
	case t < 2:
		u := 2 - t
		return u * u * (7.0/18*u - 6.0/18)
	}
	return 0
}

// bicubicFit resamples src into a pixels square, its aspect kept and
// centred; pixel centres outside the fitted rectangle stay transparent.
func bicubicFit(src *image.RGBA, pixels int) *image.RGBA {
	w, h := float64(src.Rect.Dx()), float64(src.Rect.Dy())
	side := float64(pixels)
	s := min(side/w, side/h)
	tx, ty := (side-w*s)/2, (side-h*s)/2
	out := image.NewRGBA(image.Rect(0, 0, pixels, pixels))
	for y := range pixels {
		cy := float64(y) + 0.5
		if cy < ty || cy >= ty+h*s {
			continue
		}
		sy := (cy-ty)/s - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		for x := range pixels {
			cx := float64(x) + 0.5
			if cx < tx || cx >= tx+w*s {
				continue
			}
			sx := (cx-tx)/s - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			var acc [4]float64
			for j := -1; j <= 2; j++ {
				wy := mitchell(float64(j) - fy)
				yy := min(max(y0+j, 0), src.Rect.Dy()-1)
				for i := -1; i <= 2; i++ {
					k := wy * mitchell(float64(i)-fx)
					o := src.PixOffset(min(max(x0+i, 0), src.Rect.Dx()-1), yy)
					for c := range 4 {
						acc[c] += k * float64(src.Pix[o+c])
					}
				}
			}
			o := out.PixOffset(x, y)
			a := math.Round(min(max(acc[3], 0), 255))
			out.Pix[o+3] = uint8(a)
			for c := range 3 {
				out.Pix[o+c] = uint8(math.Round(min(max(acc[c], 0), a)))
			}
		}
	}
	return out
}
