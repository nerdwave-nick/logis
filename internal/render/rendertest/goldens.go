package rendertest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The golden harness (spec §6 "offscreen golden renders"): compare an
// image (an offscreen render or an E2E capture) with a checked-in PNG under
// a Tolerance, or write it in update mode.
//
//   - Files: tests/goldens/<set>/<name>.png. Scaled variants are named
//     <name>@<scale> (ScaleSuffix).
//   - Update mode: LOGIS_UPDATE_GOLDENS=1 go test … renders instead of
//     comparing. An image within tolerance of the checked-in golden leaves
//     it untouched; any other is written as the golden, and the verdict
//     says how it differs from the one it replaces. Review before
//     committing.
//   - Failures name the golden and leave <name>.actual.png and
//     <name>.diff.png (outliers magenta over the dimmed golden) under
//     $LOGIS_GOLDEN_FAILURES/<set>/ (default: the temp dir's
//     logis-golden-failures). A passing check removes them, so a leftover
//     is never taken for a later run's.

// UpdateEnv is the variable that switches to update mode.
const UpdateEnv = "LOGIS_UPDATE_GOLDENS"

// FailuresEnv names the directory failure artefacts go to.
const FailuresEnv = "LOGIS_GOLDEN_FAILURES"

// UpdateRequested reports whether a value of UpdateEnv asks for update
// mode: non-empty and not 0.
func UpdateRequested(value string) bool { return value != "" && value != "0" }

// Alpha is how a golden file stores alpha.
type Alpha int

const (
	// Premultiplied stores the renderer's bytes as they are (the default).
	Premultiplied Alpha = iota
	// Straight stores PNG's straight alpha (the icon and theme goldens).
	Straight
)

// Premultiply turns straight alpha into premultiplied, rounding to nearest.
func Premultiply(img *render.Image) *render.Image {
	out := &render.Image{Width: img.Width, Height: img.Height, Pixels: make([]byte, len(img.Pixels))}
	for i := 0; i < len(img.Pixels); i += 4 {
		a := uint16(img.Pixels[i+3])
		for k := range 3 {
			out.Pixels[i+k] = byte((uint16(img.Pixels[i+k])*a + 127) / 255)
		}
		out.Pixels[i+3] = img.Pixels[i+3]
	}
	return out
}

// Demultiply turns premultiplied alpha into straight, rounding to nearest.
func Demultiply(img *render.Image) *render.Image {
	out := &render.Image{Width: img.Width, Height: img.Height, Pixels: make([]byte, len(img.Pixels))}
	for i := 0; i < len(img.Pixels); i += 4 {
		a := uint16(img.Pixels[i+3])
		for k := range 3 {
			if a != 0 {
				out.Pixels[i+k] = byte(min((uint16(img.Pixels[i+k])*255+a/2)/a, 255))
			}
		}
		out.Pixels[i+3] = img.Pixels[i+3]
	}
	return out
}

// DiffImage is the golden dimmed to grey, with every pixel differing from
// actual by more than channel magenta.
func DiffImage(actual, golden *render.Image, channel uint8) *render.Image {
	out := &render.Image{Width: golden.Width, Height: golden.Height, Pixels: make([]byte, 0, len(golden.Pixels))}
	for i := 0; i < len(golden.Pixels); i += 4 {
		var d uint8
		for k := range 4 {
			a, g := actual.Pixels[i+k], golden.Pixels[i+k]
			d = max(d, max(a, g)-min(a, g))
		}
		if d > channel {
			out.Pixels = append(out.Pixels, 255, 0, 255, 255)
			continue
		}
		g := golden.Pixels[i : i+3]
		grey := byte((uint16(g[0]) + uint16(g[1]) + uint16(g[2])) / 12)
		out.Pixels = append(out.Pixels, grey, grey, grey, 255)
	}
	return out
}

// Goldens is a directory of goldens and where failure artefacts go.
type Goldens struct {
	Root, Failures string
	// Update writes goldens instead of comparing.
	Update bool
	Alpha  Alpha
}

// CheckedIn is the repository's tests/goldens, in update mode when
// UpdateEnv asks for it.
func CheckedIn(t testing.TB) *Goldens {
	t.Helper()
	failures := os.Getenv(FailuresEnv)
	if failures == "" {
		failures = filepath.Join(os.TempDir(), "logis-golden-failures")
	}
	return &Goldens{
		Root:     filepath.Join(moduleRoot(t), "tests", "goldens"),
		Failures: failures,
		Update:   UpdateRequested(os.Getenv(UpdateEnv)),
	}
}

// Path is golden set/name's file.
func (g *Goldens) Path(set, name string) string {
	return filepath.Join(g.Root, set, name+".png")
}

func (g *Goldens) read(path string) (*render.Image, error) {
	img, err := ReadPNG(path)
	if err != nil {
		return nil, err
	}
	if g.Alpha == Straight {
		img = Premultiply(img)
	}
	return img, nil
}

// Load reads golden set/name (Path), premultiplied.
func (g *Goldens) Load(set, name string) (*render.Image, error) {
	path := g.Path(set, name)
	img, err := g.read(path)
	if err != nil {
		return nil, fmt.Errorf("golden %s cannot be read (%v); render it with %s=1 and review it", path, err, UpdateEnv)
	}
	return img, nil
}

// Verdict is what a check did.
type Verdict struct {
	// Updated: update mode wrote the golden at Path.
	Updated bool
	// Path is the golden compared with or written.
	Path string
	// Comparison is how the image compared with the golden at Path
	// (compare mode, or update mode when the golden was kept).
	Comparison Comparison
	// Previous is how a written golden differs from the one it replaced,
	// if there was one of the same size.
	Previous *Comparison
}

// Check compares img with golden set/name, or in update mode writes it
// (see the package's golden harness notes). A mismatch leaves the image and
// a difference image under Failures and names them in the error.
func (g *Goldens) Check(set, name string, img *render.Image, tol Tolerance) (Verdict, error) {
	if g.Update {
		return g.update(set, name, img, tol)
	}
	golden, err := g.Load(set, name)
	if err != nil {
		return Verdict{}, err
	}
	path := g.Path(set, name)
	c, err := Compare(img, golden, tol.Channel)
	if err != nil {
		return Verdict{}, fmt.Errorf("%v (%s)", err, path)
	}
	dir := filepath.Join(g.Failures, set)
	actual := filepath.Join(dir, name+".actual.png")
	marked := filepath.Join(dir, name+".diff.png")
	if c.Passes(tol) {
		// An earlier failure's artefacts would pass for this run's.
		for _, stale := range []string{actual, marked} {
			if err := os.Remove(stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return Verdict{}, fmt.Errorf("cannot remove the stale failure artefact %s: %v", stale, err)
			}
		}
		return Verdict{Path: path, Comparison: c}, nil
	}
	if err := writeAll(dir, map[string]*render.Image{actual: img, marked: DiffImage(img, golden, tol.Channel)}); err != nil {
		return Verdict{}, fmt.Errorf("cannot write failure artefacts in %s: %v", dir, err)
	}
	return Verdict{}, fmt.Errorf("%d of %d pixels differ from golden %s by more than %d (worst %d, allowed %.3f%%); see %s and %s",
		c.Outliers, c.Total, path, tol.Channel, c.Worst, tol.Outliers*100, actual, marked)
}

func (g *Goldens) update(set, name string, img *render.Image, tol Tolerance) (Verdict, error) {
	path := g.Path(set, name)
	v := Verdict{Path: path, Updated: true}
	if previous, err := g.read(path); err == nil {
		c, err := Compare(img, previous, tol.Channel)
		if err == nil && c.Passes(tol) {
			return Verdict{Path: path, Comparison: c}, nil
		}
		if err == nil {
			v.Previous = &c
		}
	}
	stored := img
	if g.Alpha == Straight {
		stored = Demultiply(img)
	}
	if err := writeAll(filepath.Dir(path), map[string]*render.Image{path: stored}); err != nil {
		return Verdict{}, fmt.Errorf("cannot write golden %s: %v", path, err)
	}
	return v, nil
}

func writeAll(dir string, images map[string]*render.Image) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for path, img := range images {
		if err := WritePNG(path, img); err != nil {
			return err
		}
	}
	return nil
}

// Assert is Check, failing t on a mismatch and logging the verdict.
func (g *Goldens) Assert(t testing.TB, set, name string, img *render.Image, tol Tolerance) Verdict {
	t.Helper()
	v, err := g.Check(set, name, img, tol)
	switch {
	case err != nil:
		t.Error(err)
	case v.Updated && v.Previous != nil:
		t.Logf("wrote golden %s; against the previous one: %v — review it", v.Path, *v.Previous)
	case v.Updated:
		t.Logf("wrote golden %s (there was none of this size) — review it", v.Path)
	default:
		t.Logf("%s: %v", v.Path, v.Comparison)
	}
	return v
}

// ScaleSuffix is a scale's file-name suffix: 1.0, 1.3, 2.0, 1.25 …
func ScaleSuffix(s scale.Scale) string {
	n := s.As120ths()
	fraction := strconv.FormatFloat(float64(n%120)/120, 'f', 3, 64)
	fraction = strings.TrimRight(strings.TrimPrefix(fraction, "0"), "0")
	if fraction == "." {
		fraction = ".0"
	}
	return strconv.FormatUint(uint64(n/120), 10) + fraction
}

// CheckScene renders scene at logical size at each of scales through h and
// checks each against golden set/name@<scale> with the offscreen tolerance.
// Under go test -short it skips t (SkipPixels).
func CheckScene(t testing.TB, h *render.Headless, set, name string, scene *render.Scene, logical scale.Size, scales ...scale.Scale) {
	t.Helper()
	SkipPixels(t)
	g := CheckedIn(t)
	for _, s := range scales {
		img, err := h.Offscreen(scene, logical, s)
		if err != nil {
			t.Fatalf("%s/%s@%s: %v", set, name, ScaleSuffix(s), err)
		}
		g.Assert(t, set, name+"@"+ScaleSuffix(s), img, Offscreen)
	}
}
