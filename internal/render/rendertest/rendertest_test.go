package rendertest

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ports of Rust tests/golden_harness.rs's comparison and file tests.

func solid(w, h int, rgba [4]byte) *render.Image {
	return &render.Image{Width: w, Height: h, Pixels: bytes.Repeat(rgba[:], w*h)}
}

func withPixel(img *render.Image, x, y int, rgba [4]byte) *render.Image {
	out := &render.Image{Width: img.Width, Height: img.Height, Pixels: bytes.Clone(img.Pixels)}
	copy(out.Pixels[(y*img.Width+x)*4:], rgba[:])
	return out
}

func compare(t *testing.T, actual, golden *render.Image) Comparison {
	t.Helper()
	c, err := Compare(actual, golden, Offscreen.Channel)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestIdenticalImagesHaveNoOutliers(t *testing.T) {
	img := solid(4, 3, [4]byte{10, 20, 30, 255})
	c := compare(t, img, img)
	if c != (Comparison{Outliers: 0, Worst: 0, Total: 12}) || !c.Passes(Offscreen) {
		t.Errorf("%+v", c)
	}
}

func TestADifferenceWithinTheChannelToleranceIsNoOutlier(t *testing.T) {
	golden := solid(4, 3, [4]byte{10, 20, 30, 255})
	c := compare(t, withPixel(golden, 1, 1, [4]byte{12, 18, 30, 255}), golden)
	if c.Outliers != 0 || c.Worst != 2 || !c.Passes(Offscreen) {
		t.Errorf("%+v", c)
	}
}

func TestADifferenceBeyondTheChannelToleranceIsAnOutlier(t *testing.T) {
	golden := solid(4, 3, [4]byte{10, 20, 30, 255})
	c := compare(t, withPixel(golden, 3, 2, [4]byte{10, 20, 30, 250}), golden)
	if c.Outliers != 1 || c.Worst != 5 || c.Passes(Offscreen) {
		t.Errorf("%+v", c)
	}
}

func TestADifferenceOneBeyondTheChannelToleranceIsAnOutlier(t *testing.T) {
	golden := solid(2, 2, [4]byte{10, 20, 30, 255})
	c := compare(t, withPixel(golden, 0, 0, [4]byte{13, 20, 30, 255}), golden)
	if c.Outliers != 1 || c.Worst != 3 {
		t.Errorf("%+v", c)
	}
}

func TestAnOutlierFractionAllowsThatManyOutliersAndNoMore(t *testing.T) {
	golden := solid(10, 10, [4]byte{0, 0, 0, 255})
	one := withPixel(golden, 0, 0, [4]byte{90, 0, 0, 255})
	two := withPixel(one, 1, 0, [4]byte{90, 0, 0, 255})
	loose := Tolerance{Channel: 2, Outliers: 0.01}
	if !compare(t, one, golden).Passes(loose) {
		t.Error("one outlier in 100 should pass 1 %")
	}
	if compare(t, two, golden).Passes(loose) {
		t.Error("two outliers in 100 should fail 1 %")
	}
}

func TestImagesOfAnotherSizeDoNotCompare(t *testing.T) {
	_, err := Compare(solid(4, 3, [4]byte{}), solid(3, 4, [4]byte{}), 2)
	if err == nil || !strings.Contains(err.Error(), "4x3") || !strings.Contains(err.Error(), "3x4") {
		t.Errorf("err %v", err)
	}
}

func TestPNGRoundTripsRGBAExactly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.png")
	img := withPixel(solid(3, 2, [4]byte{1, 2, 3, 4}), 2, 1, [4]byte{250, 128, 0, 255})
	if err := WritePNG(path, img); err != nil {
		t.Fatal(err)
	}
	back, err := ReadPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Width != 3 || back.Height != 2 || !bytes.Equal(back.Pixels, img.Pixels) {
		t.Errorf("read back %dx%d %v, wrote %v", back.Width, back.Height, back.Pixels, img.Pixels)
	}
}

func TestTheCheckedInGoldensAreFound(t *testing.T) {
	img, err := ReadPNG(Golden(t, "harness", "reference@1.3"))
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 156 || img.Height != 104 {
		t.Errorf("reference@1.3 is %dx%d", img.Width, img.Height)
	}
}

// fake records failures instead of failing the test.
type fake struct {
	testing.TB
	errors []string
}

func (f *fake) Helper()                         {}
func (f *fake) Error(args ...any)               { f.errors = append(f.errors, args[0].(string)) }
func (f *fake) Logf(format string, args ...any) {}

func TestCheckFailsAMismatchAndDumpsTheRender(t *testing.T) {
	dir := t.TempDir()
	golden := filepath.Join(dir, "s.png")
	if err := WritePNG(golden, solid(4, 4, [4]byte{0, 0, 0, 255})); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOGIS_RENDER_DUMP", dir)
	wrong := withPixel(solid(4, 4, [4]byte{0, 0, 0, 255}), 2, 2, [4]byte{255, 255, 255, 255})
	f := &fake{TB: t}
	Check(f, wrong, golden, Offscreen)
	if len(f.errors) != 1 || !strings.Contains(f.errors[0], "1 of 16 pixels") {
		t.Fatalf("errors %q", f.errors)
	}
	dumped, err := ReadPNG(filepath.Join(dir, "s.actual.png"))
	if err != nil || !bytes.Equal(dumped.Pixels, wrong.Pixels) {
		t.Errorf("dumped %v, %v", dumped, err)
	}
	f = &fake{TB: t}
	Check(f, solid(4, 4, [4]byte{1, 0, 0, 255}), golden, Offscreen)
	if len(f.errors) != 0 {
		t.Errorf("a match failed: %q", f.errors)
	}
}
