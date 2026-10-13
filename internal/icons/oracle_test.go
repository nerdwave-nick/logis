package icons

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A test-only differential oracle: the resvg binary (when installed, never
// linked) renders an SVG at 8× the size; averaging each 8×8 block gives
// coverage close to exact, against which logis's render is measured. A
// plain resvg render at the target size is no oracle for edge pixels: its
// supersampled anti-aliasing strays further from that average than
// logis's does (see gpu_test.go).

const oversample = 8

// resvgTruth is resvg's render of path, oversampled and averaged into a
// pixels square, fitted and centred as Rasterise does; premultiplied, 0
// to 1.
func resvgTruth(path string, pixels int) ([][4]float64, error) {
	big := pixels * oversample
	cmd := exec.Command("resvg", "-w", fmt.Sprint(big), "-h", fmt.Sprint(big), path, "-c")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resvg: %v: %s", err, stderr.String())
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	offX, offY := (big-b.Dx())/2, (big-b.Dy())/2
	truth := make([][4]float64, pixels*pixels)
	for y := range big {
		for x := range big {
			sx, sy := x-offX, y-offY
			if sx < 0 || sy < 0 || sx >= b.Dx() || sy >= b.Dy() {
				continue
			}
			r, g, bl, a := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA() // premultiplied
			p := &truth[(y/oversample)*pixels+x/oversample]
			for k, v := range [4]uint32{r, g, bl, a} {
				p[k] += float64(v) / 0xffff / (oversample * oversample)
			}
		}
	}
	return truth, nil
}

// truthError is how far logis's render of path at pixels strays from the
// oracle, in levels of 255: the worst channel and the mean of each
// pixel's worst channel.
type truthError struct{ worst, mean float64 }

func compareTruth(path string, pixels int) (truthError, []string, error) {
	truth, err := resvgTruth(path, pixels)
	if err != nil {
		return truthError{}, nil, err
	}
	img, report, err := rasteriseReport(path, pixels)
	if err != nil {
		return truthError{}, nil, err
	}
	var e truthError
	for i, want := range truth {
		d := 0.0
		for k := range 4 {
			d = max(d, math.Abs(float64(img.Pixels[i*4+k])-want[k]*255))
		}
		e.worst = max(e.worst, d)
		e.mean += d / float64(len(truth))
	}
	return e, report, nil
}

func requireResvg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("resvg"); err != nil {
		if os.Getenv("LOGIS_REQUIRE_RESVG") != "" {
			t.Fatalf("LOGIS_REQUIRE_RESVG is set, but: %v", err)
		}
		t.Skipf("no resvg binary as oracle: %v", err)
	}
}

func TestSVGsAgreeWithAnOversampledResvg(t *testing.T) {
	requireResvg(t)
	files, _ := filepath.Glob("../svg/testdata/*.svg")
	for _, rel := range []string{
		forgeDir + "/scalable/apps/forge-cog.svg",
		forgeDir + "/scalable/apps/forge-banner.svg",
		"share/icons/logis-cogitator/scalable/apps/cogitator-skull.svg",
	} {
		files = append(files, fixture(t, rel))
	}
	if len(files) < 6 {
		t.Fatalf("only %d SVGs", len(files))
	}
	for _, f := range files {
		for _, pixels := range []int{32, 42, 64} {
			e, report, err := compareTruth(f, pixels)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			t.Logf("%s at %d: worst %.1f, mean %.2f levels", filepath.Base(f), pixels, e.worst, e.mean)
			if len(report) > 0 {
				t.Errorf("%s: unsupported %q", f, report)
			}
			// Edge pixels may differ by flattening and coverage rounding;
			// a wrong shape, paint or position costs far more.
			if e.mean > 1.5 || e.worst > 32 {
				t.Errorf("%s at %d: worst %.1f, mean %.2f levels from the oracle", filepath.Base(f), pixels, e.worst, e.mean)
			}
		}
	}
}
