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
	// Along each row: taps 4 bytes apart, rows w*4.
	sample(src, w, h, nw, true, axis{tap: 4, line: w * 4}, tmp, axis{tap: 4, line: nw * 4})
	out := make([]byte, nw*nh*4)
	// Along each column of tmp: taps a row (nw*4) apart, columns 4.
	sample(tmp, h, nw, nh, false, axis{tap: nw * 4, line: 4}, out, axis{tap: nw * 4, line: 4})
	return out
}

// axis is how an image is walked: bytes from one sample to the next along
// the resampled direction (tap), and from one line to the next.
type axis struct{ tap, line int }

// window is one output sample's filter: its first input sample and its
// weights.
type window struct {
	left    int
	weights []float32
	isum    float32
}

// sample is swash's sample_dir: n samples along an axis per each of lines
// lines resampled to m. Each output's window and weights are computed
// once and each line's samples converted once; then every line is
// filtered, in the same arithmetic as the straightforward port.
func sample(in []byte, n, lines, m int, premultiplied bool, from axis, out []byte, to axis) {
	windows := filters(n, m)
	const s = 1.0 / 255
	// One line's samples as floats (v·s), each converted once.
	v := make([]float32, n*4)
	for line := range lines {
		at := line * from.line
		for i := range n {
			p := in[at : at+4 : at+4]
			v[4*i], v[4*i+1], v[4*i+2], v[4*i+3] = float32(p[0])*s, float32(p[1])*s, float32(p[2])*s, float32(p[3])*s
			at += from.tap
		}
		for o, win := range windows {
			var acc [4]float32
			taps := v[4*win.left : 4*(win.left+len(win.weights))]
			for i, wt := range win.weights {
				p := taps[4*i : 4*i+4 : 4*i+4]
				a := p[3]
				if premultiplied {
					acc[0] += p[0] * wt
					acc[1] += p[1] * wt
					acc[2] += p[2] * wt
				} else {
					acc[0] += p[0] * wt * a
					acc[1] += p[1] * wt * a
					acc[2] += p[2] * wt * a
				}
				acc[3] += a * wt
			}
			if acc[3] != 0 {
				ia := 1 / acc[3]
				acc[0], acc[1], acc[2], acc[3] = acc[0]*ia, acc[1]*ia, acc[2]*ia, acc[3]*win.isum
			}
			q := out[line*to.line+o*to.tap:]
			q = q[:4:4]
			for c := range 4 {
				q[c] = byte(acc[c] * 255)
			}
		}
	}
}

// filters are the windows of m outputs over n inputs (swash's support,
// clamping and at most 64 taps).
func filters(n, m int) []window {
	const support = 2
	const maxWeights = 64
	ratio := float32(n) / float32(m)
	sratio := max(ratio, 1)
	srcSupport := support * sratio
	isratio := 1 / sratio
	windows := make([]window, m)
	all := make([]float32, 0, m*min(maxWeights, int(2*srcSupport)+3))
	for o := range m {
		inx := (float32(o) + 0.5) * ratio
		left := min(max(int(math.Floor(float64(inx-srcSupport))), 0), n-1)
		right := min(max(int(math.Ceil(float64(inx+srcSupport))), left+1), n)
		inx -= 0.5
		for right-left > maxWeights {
			right--
			left++
		}
		start := len(all)
		var sum float32
		for i := range right - left {
			wt := mitchell((float32(left+i) - inx) * isratio)
			all = append(all, wt)
			sum += wt
		}
		windows[o] = window{left: left, weights: all[start:len(all):len(all)], isum: 1 / sum}
	}
	return windows
}
