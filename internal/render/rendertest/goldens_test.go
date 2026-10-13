package rendertest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust tests/golden_harness.rs's harness tests (update mode,
// failure artefacts, alpha), plus the Go goldens beside the Rust ones.

func TestTheDifferenceImageMarksOutliersInMagenta(t *testing.T) {
	golden := solid(2, 1, [4]byte{200, 100, 40, 255})
	actual := withPixel(golden, 1, 0, [4]byte{0, 0, 0, 255})
	diff := DiffImage(actual, golden, 2)
	if got := diff.Pixel(1, 0); got != [4]byte{255, 0, 255, 255} {
		t.Errorf("outlier %v, want magenta", got)
	}
	if got := diff.Pixel(0, 0); got[0] != got[1] || got[1] != got[2] || got[3] != 255 || got[0] != (200+100+40)/12 {
		t.Errorf("matching pixel %v, want the golden dimmed to grey", got)
	}
}

func TestUpdateModeIsReadFromTheEnvironmentValue(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "yes": true, "0": false, "": false} {
		if got := UpdateRequested(value); got != want {
			t.Errorf("UpdateRequested(%q) = %v, want %v", value, got, want)
		}
	}
}

func goldens(dir string, update bool) *Goldens {
	return &Goldens{Root: filepath.Join(dir, "goldens"), Failures: filepath.Join(dir, "failures"), Update: update}
}

func TestUpdateModeWritesTheGoldenAndCompareModeThenMatchesIt(t *testing.T) {
	dir := t.TempDir()
	img := solid(5, 5, [4]byte{9, 8, 7, 255})
	v, err := goldens(dir, true).Check("set", "scene@1.0", img, Offscreen)
	if err != nil || !v.Updated {
		t.Fatalf("update: %+v, %v", v, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "goldens/go/set/scene@1.0.png")); err != nil {
		t.Fatalf("no Go golden written: %v", err)
	}
	v, err = goldens(dir, false).Check("set", "scene@1.0", img, Offscreen)
	if err != nil || v.Updated || v.Comparison.Outliers != 0 {
		t.Errorf("compare: %+v, %v", v, err)
	}
}

func TestAMissingGoldenFailsAndSaysHowToMakeIt(t *testing.T) {
	_, err := goldens(t.TempDir(), false).Check("set", "absent", solid(1, 1, [4]byte{}), Offscreen)
	if err == nil || !strings.Contains(err.Error(), "absent.png") || !strings.Contains(err.Error(), UpdateEnv) {
		t.Errorf("err %v", err)
	}
}

func TestAMismatchFailsAndLeavesTheRenderAndADifferenceImage(t *testing.T) {
	dir := t.TempDir()
	if _, err := goldens(dir, true).Check("set", "s", solid(4, 4, [4]byte{0, 0, 0, 255}), Offscreen); err != nil {
		t.Fatal(err)
	}
	wrong := withPixel(solid(4, 4, [4]byte{0, 0, 0, 255}), 2, 2, [4]byte{255, 255, 255, 255})
	_, err := goldens(dir, false).Check("set", "s", wrong, Offscreen)
	actual := filepath.Join(dir, "failures/set/s.actual.png")
	if err == nil || !strings.Contains(err.Error(), "1 of 16 pixels") || !strings.Contains(err.Error(), actual) {
		t.Fatalf("err %v", err)
	}
	if got, err := ReadPNG(actual); err != nil || !bytes.Equal(got.Pixels, wrong.Pixels) {
		t.Errorf("actual %v, %v", got, err)
	}
	diff, err := ReadPNG(filepath.Join(dir, "failures/set/s.diff.png"))
	if err != nil || diff.Pixel(2, 2) != [4]byte{255, 0, 255, 255} {
		t.Errorf("diff %v, %v", diff, err)
	}
}

func TestStraightAlphaGoldensArePremultipliedWhenReadAndDemultipliedWhenWritten(t *testing.T) {
	dir := t.TempDir()
	straight := solid(1, 1, [4]byte{255, 64, 0, 128})
	writeGolden(t, filepath.Join(dir, "goldens/set/s.png"), straight)
	premultiplied := solid(1, 1, [4]byte{128, 32, 0, 128})
	g := goldens(dir, false)
	g.Alpha = Straight
	v, err := g.Check("set", "s", premultiplied, Offscreen)
	if err != nil || v.Comparison.Worst > 1 {
		t.Fatalf("%+v, %v", v, err)
	}
	g.Update = true
	if _, err := g.Check("set", "t", premultiplied, Offscreen); err != nil {
		t.Fatal(err)
	}
	written, err := ReadPNG(filepath.Join(dir, "goldens/go/set/t.png"))
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := Compare(written, straight, 2); c.Worst > 1 {
		t.Errorf("written %v, want about %v", written.Pixels, straight.Pixels)
	}
}

// writeGolden puts a Rust golden in place, as the checked-in ones are.
func writeGolden(t *testing.T, path string, img *render.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WritePNG(path, img); err != nil {
		t.Fatal(err)
	}
}

func TestAGoGoldenStandsInForTheRustOneItDiffersFrom(t *testing.T) {
	dir := t.TempDir()
	writeGolden(t, filepath.Join(dir, "goldens/set/s.png"), solid(2, 2, [4]byte{0, 0, 0, 255}))
	ours := solid(2, 2, [4]byte{9, 0, 0, 255})
	if _, err := goldens(dir, false).Check("set", "s", ours, Offscreen); err == nil {
		t.Fatal("the Rust golden matched a different image")
	}
	writeGolden(t, filepath.Join(dir, "goldens/go/set/s.png"), ours)
	if _, err := goldens(dir, false).Check("set", "s", ours, Offscreen); err != nil {
		t.Errorf("the Go golden was not used: %v", err)
	}
}

func TestUpdateModeNeverRewritesARustGolden(t *testing.T) {
	dir := t.TempDir()
	rust := filepath.Join(dir, "goldens/set/s.png")
	writeGolden(t, rust, solid(2, 2, [4]byte{0, 0, 0, 255}))
	before, _ := os.ReadFile(rust)

	// Within tolerance of the Rust golden: no Go golden is needed, and a
	// stale one goes.
	stale := filepath.Join(dir, "goldens/go/set/s.png")
	writeGolden(t, stale, solid(2, 2, [4]byte{50, 0, 0, 255}))
	v, err := goldens(dir, true).Check("set", "s", solid(2, 2, [4]byte{1, 0, 0, 255}), Offscreen)
	if err != nil || v.Updated || v.Comparison.Worst != 1 {
		t.Fatalf("matching update: %+v, %v", v, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the redundant Go golden stayed: %v", err)
	}

	// Beyond it: a Go golden, and how it differs from the Rust one.
	v, err = goldens(dir, true).Check("set", "s", solid(2, 2, [4]byte{9, 0, 0, 255}), Offscreen)
	if err != nil || !v.Updated || v.Path != stale {
		t.Fatalf("differing update: %+v, %v", v, err)
	}
	if v.Rust == nil || v.Rust.Outliers != 4 || v.Rust.Worst != 9 {
		t.Errorf("against the Rust golden: %+v", v.Rust)
	}
	if after, _ := os.ReadFile(rust); !bytes.Equal(before, after) {
		t.Error("update mode rewrote the Rust golden")
	}
}

func TestScaleSuffixesNameTheScale(t *testing.T) {
	for n, want := range map[uint32]string{120: "1.0", 156: "1.3", 240: "2.0", 150: "1.25", 180: "1.5"} {
		if got := ScaleSuffix(scale.From120ths(n)); got != want {
			t.Errorf("ScaleSuffix(%d/120) = %q, want %q", n, got, want)
		}
	}
}
