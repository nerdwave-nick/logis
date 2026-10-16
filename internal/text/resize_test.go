package text

import "testing"

// The filter is the Mitchell–Netravali cubic (B = C = 1/3) with its
// negative lobes taken positive, as logis's emoji scaling defines it.
func TestTheFilterIsTheMitchellCubicWithItsNegativeLobesFolded(t *testing.T) {
	for _, tc := range []struct{ x, want float32 }{
		{0, 16.0 / 18}, {0.5, 0.5347222}, {1, 1.0 / 18}, {-1, 1.0 / 18}, {1.5, 0.0347222}, {2, 0}, {3, 0},
	} {
		if got := mitchell(tc.x); got < tc.want-1e-6 || got > tc.want+1e-6 {
			t.Errorf("mitchell(%g) = %g, want %g", tc.x, got, tc.want)
		}
	}
}

func solid(w, h int, px [4]byte) []byte {
	out := make([]byte, 0, w*h*4)
	for range w * h {
		out = append(out, px[:]...)
	}
	return out
}

func TestResizingKeepsAUniformColour(t *testing.T) {
	// Opaque orange stays orange; the 8-bit truncation per pass may lose a
	// level per channel.
	out := resize(solid(136, 128, [4]byte{250, 120, 30, 255}), 136, 128, 19, 18)
	if len(out) != 19*18*4 {
		t.Fatalf("%d bytes", len(out))
	}
	for i := 0; i < len(out); i += 4 {
		p := out[i : i+4]
		if p[0] < 248 || p[1] < 118 || p[1] > 120 || p[2] < 28 || p[2] > 30 || p[3] < 253 {
			t.Fatalf("pixel %d is %v", i/4, p)
		}
	}
}

func TestTransparentPixelsLendNoColour(t *testing.T) {
	// Left half transparent black, right half opaque white: colour is
	// weighted by alpha, so every pixel with any alpha is white, while
	// alpha itself fades across the edge.
	src := make([]byte, 0, 16*4*4)
	for range 4 {
		for x := range 16 {
			if x < 8 {
				src = append(src, 0, 0, 0, 0)
			} else {
				src = append(src, 255, 255, 255, 255)
			}
		}
	}
	out := resize(src, 16, 4, 4, 1)
	var alphas []byte
	for i := 0; i < len(out); i += 4 {
		p := out[i : i+4]
		alphas = append(alphas, p[3])
		if p[3] > 0 && (p[0] < 254 || p[1] < 254 || p[2] < 254) {
			t.Errorf("pixel %d is %v: transparent black bled in", i/4, p)
		}
	}
	if alphas[0] >= alphas[1] || alphas[1] >= alphas[2] || alphas[2] > alphas[3] {
		t.Errorf("alpha does not rise across the edge: %v", alphas)
	}
}

// FreeType's colour strikes are premultiplied; the resize reads them as
// they are and returns straight colour.
func TestPremultipliedInputComesOutStraight(t *testing.T) {
	// Half-transparent red, premultiplied (128, 0, 0, 128), comes out
	// straight: full red at half alpha.
	out := resize(solid(8, 8, [4]byte{128, 0, 0, 128}), 8, 8, 4, 4)
	for i := 0; i < len(out); i += 4 {
		if p := out[i : i+4]; p[0] < 253 || p[1] != 0 || p[3] < 126 || p[3] > 128 {
			t.Fatalf("pixel %d is %v", i/4, p)
		}
	}
}
