package text

import (
	"bytes"
	"math"
	"math/rand/v2"
	"testing"
)

// The resize is tuned for speed; it answers byte for byte as the
// straightforward version of the same algorithm it replaced
// (resizeReference, kept verbatim here), logis's own code.
func TestTheTunedResizeEqualsTheStraightforwardOneByteForByte(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, c := range [][4]int{{136, 128, 20, 18}, {136, 128, 26, 24}, {136, 128, 41, 38}, {7, 5, 13, 11}, {64, 64, 64, 64}, {3, 90, 2, 7}, {1, 1, 4, 4}} {
		w, h, nw, nh := c[0], c[1], c[2], c[3]
		img := make([]byte, w*h*4)
		for i := 0; i < len(img); i += 4 {
			a := byte(r.IntN(256))
			if r.IntN(4) == 0 {
				a = 0
			}
			for k := range 3 {
				img[i+k] = byte(r.IntN(int(a) + 1)) // premultiplied
			}
			img[i+3] = a
		}
		if got, want := resize(img, w, h, nw, nh), resizeReference(img, w, h, nw, nh); !bytes.Equal(got, want) {
			t.Fatalf("%v: the tuned resize differs from the straightforward one", c)
		}
	}
}

func BenchmarkResizeAnEmojiStrike(b *testing.B) {
	img := make([]byte, 136*128*4)
	for i := range img {
		img[i] = byte(i * 7)
	}
	for b.Loop() {
		resize(img, 136, 128, 20, 18)
	}
}

func resizeReference(src []byte, w, h, nw, nh int) []byte {
	tmp := make([]byte, nw*h*4)
	sampleReference(src, w, h, nw, true, func(x, y int) int { return (y*w + x) * 4 },
		tmp, func(x, y int) int { return (y*nw + x) * 4 })
	out := make([]byte, nw*nh*4)
	sampleReference(tmp, h, nw, nh, false, func(y, x int) int { return (y*nw + x) * 4 },
		out, func(y, x int) int { return (y*nw + x) * 4 })
	return out
}

// sample is swash's sample_dir: n samples along an axis per each of lines
// lines resampled to m, at offsets at(i, line) in in and put(o, line) in
// out.
func sampleReference(in []byte, n, lines, m int, premultiplied bool, at func(i, line int) int, out []byte, put func(o, line int) int) {
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

var _ = math.Floor

func BenchmarkResizeAnEmojiStrikeReference(b *testing.B) {
	img := make([]byte, 136*128*4)
	for i := range img {
		img[i] = byte(i * 7)
	}
	for b.Loop() {
		resizeReference(img, 136, 128, 20, 18)
	}
}
