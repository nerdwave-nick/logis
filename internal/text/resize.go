package text

import "math"

// mitchell is swash's resampling kernel: the Mitchell–Netravali cubic with
// absolute values taken, so its negative lobes count positive.
func mitchell(x float32) float32 {
	x = abs32(x)
	switch {
	case x < 1:
		return abs32((16 + x*x*(21*x-36)) / 18)
	case x < 2:
		return abs32((32 + x*(-60+x*(36-7*x))) / 18)
	}
	return 0
}

func abs32(x float32) float32 { return math.Float32frombits(math.Float32bits(x) &^ (1 << 31)) }

// resize scales a w×h premultiplied RGBA8 image to nw×nh straight RGBA8,
// porting swash's bitmap::resize with its Mitchell filter (support 2):
// separable, rows then columns, colour weighted by alpha, each pass
// truncated to bytes. swash reads its PNGs straight and weights colour by
// alpha; FreeType's bitmaps are premultiplied, so the first pass reads
// the products as they are.
func resize(src []byte, w, h, nw, nh int) []byte {
	tmp := make([]byte, nw*h*4)
	sample(src, w, h, nw, true, func(x, y int) int { return (y*w + x) * 4 },
		tmp, func(x, y int) int { return (y*nw + x) * 4 })
	out := make([]byte, nw*nh*4)
	sample(tmp, h, nw, nh, false, func(y, x int) int { return (y*nw + x) * 4 },
		out, func(y, x int) int { return (y*nw + x) * 4 })
	return out
}

// sample is swash's sample_dir: n samples along an axis per each of lines
// lines resampled to m, at offsets at(i, line) in in and put(o, line) in
// out.
func sample(in []byte, n, lines, m int, premultiplied bool, at func(i, line int) int, out []byte, put func(o, line int) int) {
	const support = 2
	const maxWeights = 64
	var weights [maxWeights]float32
	ratio := float32(n) / float32(m)
	sratio := max(ratio, 1)
	srcSupport := support * sratio
	isratio := 1 / sratio
	const s = 1.0 / 255
	for o := range m {
		inx := (float32(o) + 0.5) * ratio
		left := min(max(int(math.Floor(float64(inx-srcSupport))), 0), n-1)
		right := min(max(int(math.Ceil(float64(inx+srcSupport))), left+1), n)
		inx -= 0.5
		for right-left > maxWeights {
			right--
			left++
		}
		k := right - left
		var sum float32
		for i := range k {
			weights[i] = mitchell((float32(left+i) - inx) * isratio)
			sum += weights[i]
		}
		isum := 1 / sum
		for line := range lines {
			var acc [4]float32
			for i, wt := range weights[:k] {
				p := in[at(left+i, line):]
				a := float32(p[3]) * s
				for c := range 3 {
					v := float32(p[c]) * s
					if premultiplied {
						acc[c] += v * wt
					} else {
						acc[c] += v * wt * a
					}
				}
				acc[3] += a * wt
			}
			if acc[3] != 0 {
				ia := 1 / acc[3]
				acc[0], acc[1], acc[2], acc[3] = acc[0]*ia, acc[1]*ia, acc[2]*ia, acc[3]*isum
			}
			q := out[put(o, line):]
			for c := range 4 {
				q[c] = byte(acc[c] * 255)
			}
		}
	}
}
