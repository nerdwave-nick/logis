// Package rendertest helps tests check what internal/render draws: a
// headless renderer that skips without EGL (unless LOGIS_REQUIRE_EGL is
// set) and, for a test of the pixels it draws, under go test -short
// (SkipPixels); image comparison with a tolerance (a per-channel
// difference and a fraction of outlier pixels); and PNG files holding
// premultiplied bytes as the checked-in goldens do.
//
// The golden harness proper (Goldens: update mode, failure artefacts;
// CheckScene) is in goldens.go.
package rendertest

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// PixelsReason is why go test -short skips a pixel check.
const PixelsReason = "gpu pixels: the verdict rests on how this machine's GPU and driver " +
	"round what they draw; -short (the PKGBUILD's check()) leaves it to CI and a plain go test"

// SkipPixels skips t under go test -short. A test whose verdict rests on
// pixels a GPU drew (a golden comparison, or pixels read back and held to
// values) depends on the builder's GPU and driver, which round and
// anti-alias differently: like a wall-clock limit, it is for CI and
// development, never for the install check.
func SkipPixels(t testing.TB) {
	t.Helper()
	if testing.Short() {
		t.Skip(PixelsReason)
	}
}

// Headless is a headless renderer for a test that checks the pixels it
// draws, owned by the test's goroutine and closed when the test ends.
// Under go test -short the test is skipped first (SkipPixels). Without a
// surfaceless EGL it is skipped too, unless LOGIS_REQUIRE_EGL is set (CI
// with llvmpipe), so GL tests cannot skip unseen.
func Headless(t testing.TB) *render.Headless {
	t.Helper()
	SkipPixels(t)
	return HeadlessForStructure(t)
}

// HeadlessForStructure is Headless for a test that holds no pixel the GPU
// drew to a value: sizes, errors, atlas and glyph bookkeeping, text
// measures. It runs under go test -short too; a test that also checks
// pixels calls SkipPixels before it reads them.
func HeadlessForStructure(t testing.TB) *render.Headless {
	t.Helper()
	h, err := render.OpenHeadless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatalf("LOGIS_REQUIRE_EGL is set, but: %v", err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	t.Cleanup(h.Close)
	return h
}

// Tolerance is how far an image may stray from its golden.
type Tolerance struct {
	// Channel is the largest per-channel difference a matching pixel has.
	Channel uint8
	// Outliers is the fraction of pixels (0 to 1) allowed beyond Channel.
	Outliers float64
}

var (
	// Offscreen is for offscreen renders: rounding between GPUs only.
	Offscreen = Tolerance{Channel: 2}
	// Capture is for a compositor's capture of a panel: the compositor
	// composites the translucent panel itself.
	Capture = Tolerance{Channel: 4, Outliers: 0.001}
)

// Comparison is how an image differs from its golden.
type Comparison struct {
	// Outliers is the number of pixels beyond the channel tolerance.
	Outliers int
	// Worst is the largest per-channel difference.
	Worst uint8
	// Total is the number of pixels.
	Total int
}

// Passes reports whether c is within tol.
func (c Comparison) Passes(tol Tolerance) bool {
	return float64(c.Outliers) <= tol.Outliers*float64(c.Total)
}

func (c Comparison) String() string {
	return fmt.Sprintf("%d of %d pixels beyond tolerance, worst channel difference %d", c.Outliers, c.Total, c.Worst)
}

// Compare compares two images of the same size, counting pixels with a
// channel more than channel apart.
func Compare(actual, golden *render.Image, channel uint8) (Comparison, error) {
	if actual.Width != golden.Width || actual.Height != golden.Height {
		return Comparison{}, fmt.Errorf("the image is %dx%d but its golden is %dx%d",
			actual.Width, actual.Height, golden.Width, golden.Height)
	}
	c := Comparison{Total: actual.Width * actual.Height}
	for i := 0; i < len(golden.Pixels); i += 4 {
		var d uint8
		for k := range 4 {
			a, g := actual.Pixels[i+k], golden.Pixels[i+k]
			d = max(d, max(a, g)-min(a, g))
		}
		c.Worst = max(c.Worst, d)
		if d > channel {
			c.Outliers++
		}
	}
	return c, nil
}

// ReadPNG reads a PNG's RGBA bytes exactly as stored (goldens store the
// renderer's premultiplied bytes as they are).
func ReadPNG(path string) (*render.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	decoded, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	b := decoded.Bounds()
	img := &render.Image{Width: b.Dx(), Height: b.Dy(), Pixels: make([]byte, 0, b.Dx()*b.Dy()*4)}
	// RGBA files decode as NRGBA (the bytes as stored); opaque RGB files as
	// RGBA, whose bytes are the stored ones too.
	var pix []byte
	var stride int
	switch d := decoded.(type) {
	case *image.NRGBA:
		pix, stride = d.Pix, d.Stride
	case *image.RGBA:
		pix, stride = d.Pix, d.Stride
	default:
		return nil, fmt.Errorf("%s: an %T PNG, not 8-bit RGB(A)", path, decoded)
	}
	for y := range img.Height {
		img.Pixels = append(img.Pixels, pix[y*stride:y*stride+img.Width*4]...)
	}
	return img, nil
}

// WritePNG writes img's bytes as they are into an 8-bit RGBA PNG.
func WritePNG(path string, img *render.Image) error {
	out := &image.NRGBA{Pix: img.Pixels, Stride: img.Width * 4, Rect: image.Rect(0, 0, img.Width, img.Height)}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, out); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Golden is the path of a checked-in golden, tests/goldens/<set>/<name>.png,
// found from the working directory upwards (go test runs in the package's
// directory).
func Golden(t testing.TB, set, name string) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "tests", "goldens", set, name+".png")
}

// moduleRoot is the directory of go.mod, from the working directory
// upwards.
func moduleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// Check fails t unless img is within tol of the golden at path. With
// LOGIS_RENDER_DUMP set, a failing image is written there as
// <name>.actual.png.
func Check(t testing.TB, img *render.Image, path string, tol Tolerance) {
	t.Helper()
	golden, err := ReadPNG(path)
	if err != nil {
		t.Fatalf("golden: %v", err)
	}
	c, err := Compare(img, golden, tol.Channel)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if c.Passes(tol) {
		t.Logf("%s: %v", filepath.Base(path), c)
		return
	}
	msg := fmt.Sprintf("%s: %v", path, c)
	if dir := os.Getenv("LOGIS_RENDER_DUMP"); dir != "" {
		actual := filepath.Join(dir, filepath.Base(path[:len(path)-len(".png")])+".actual.png")
		if err := WritePNG(actual, img); err != nil {
			msg += fmt.Sprintf(" (cannot write %s: %v)", actual, err)
		} else {
			msg += " (rendered: " + actual + ")"
		}
	}
	t.Error(msg)
}
