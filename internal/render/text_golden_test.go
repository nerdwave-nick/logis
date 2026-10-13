package render_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// The Rust text goldens (tests/goldens/text/corpus@<scale>.png, from
// tests/text.rs corpus_scene) against the Go text pass: the corpus lines
// on a panel, plus an ellipsised and a clipped column whose line features
// (clipping, the ellipsis) belong to the shaping slice.

var goldenSize = scale.Size{W: 330, H: 184}

var corpusInk = render.Hex(0xe8d8b0)

func corpus(t testing.TB) []string {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fonts", "corpus.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// corpusScene is Rust's corpus_scene without the two column lines.
func corpusScene(lines []string) *render.Scene {
	sc := &render.Scene{Clear: render.Hex(0x140c0a)}
	for _, y := range []float32{126, 150} {
		sc.Shapes = append(sc.Shapes, render.RoundedRect{Rect: [4]float32{8, y, 150, 21}, Radius: 3, Fill: render.Solid(render.Hex(0x3a2a20))})
	}
	for i, l := range lines {
		sc.Texts = append(sc.Texts, render.NewText(l, [2]float32{8, 6 + 24*float32(i)}, 16, corpusInk))
	}
	return sc
}

// band is one corpus line's rows compared with the golden.
type band struct {
	meanAbs     float64 // mean absolute channel difference
	over2       float64 // share of pixels with a channel more than 2 off
	worst       int
	goldInk     [4]int // ink box: left, top, right, bottom
	goInk       [4]int
	goldMass    float64 // ink in fully inked pixels
	goMass      float64
	pixels      int
	outlierRows int
}

func lum(p [4]byte) float64 {
	return 0.2126*float64(p[0]) + 0.7152*float64(p[1]) + 0.0722*float64(p[2])
}

// compareBand compares rows y0..y1 of got with gold, ink measured against
// the scene's background.
func compareBand(gold, got *render.Image, x0, x1, y0, y1 int) band {
	bg, ink := lum([4]byte{0x14, 0x0c, 0x0a}), lum([4]byte{0xe8, 0xd8, 0xb0})
	b := band{goldInk: [4]int{1 << 30, 1 << 30, -1, -1}, goInk: [4]int{1 << 30, 1 << 30, -1, -1}}
	var sum float64
	for y := y0; y < min(y1, gold.Height); y++ {
		rowOut := false
		for x := x0; x < min(x1, gold.Width); x++ {
			p, q := gold.Pixel(x, y), got.Pixel(x, y)
			m := 0
			for c := range 3 {
				d := int(p[c]) - int(q[c])
				d = max(d, -d)
				sum += float64(d)
				m = max(m, d)
			}
			b.worst = max(b.worst, m)
			if m > 2 {
				b.over2++
				rowOut = true
			}
			b.pixels++
			grow := func(box *[4]int, l float64, mass *float64) {
				*mass += math.Max(0, (l-bg)/(ink-bg))
				if l-bg > 24 {
					box[0], box[1], box[2], box[3] = min(box[0], x), min(box[1], y), max(box[2], x), max(box[3], y)
				}
			}
			grow(&b.goldInk, lum(p), &b.goldMass)
			grow(&b.goInk, lum(q), &b.goMass)
		}
		if rowOut {
			b.outlierRows++
		}
	}
	b.meanAbs = sum / float64(3*b.pixels)
	b.over2 /= float64(b.pixels)
	return b
}

var corpusScales = []struct {
	n    uint32
	name string
}{{120, "1.0"}, {156, "1.3"}, {240, "2.0"}}

// bandRows is corpus line i's band in buffer rows at s: its line box
// plus the gap to the next.
func bandRows(i int, s scale.Scale) (int, int) {
	f := float64(s.Factor())
	return int(math.Round((6 + 24*float64(i)) * f)), int(math.Round((6 + 24*float64(i+1)) * f))
}

func renderCorpus(t *testing.T, h *render.Headless, lines []string, hinting text.Hinting, s scale.Scale) *render.Image {
	t.Helper()
	fonts := pinnedFonts(t)
	fonts.SetHinting(hinting)
	h.SetFonts(fonts)
	img, err := h.Offscreen(corpusScene(lines), goldenSize, s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestTextComparisonWithTheRustGoldens reports, per load target, scale
// and corpus line, how the Go render differs from the Rust golden. It is
// the evidence for text.DefaultHinting; run it with LOGIS_TEXT_COMPARE=1.
func TestTextComparisonWithTheRustGoldens(t *testing.T) {
	if os.Getenv("LOGIS_TEXT_COMPARE") == "" {
		t.Skip("set LOGIS_TEXT_COMPARE=1 for the comparison table")
	}
	h := rendertest.Headless(t)
	lines := corpus(t)
	var out strings.Builder
	fmt.Fprintf(&out, "\n%-8s %-5s %-5s %8s %8s %6s %6s %18s %18s %8s %8s\n", "target", "scale", "line", "meanAbs", ">2", "worst", "rows", "gold ink", "go ink", "goldInk", "goInk")
	for _, target := range []text.Hinting{text.Light, text.Normal, text.LCD, text.Unhinted} {
		for _, sc := range corpusScales {
			s := scale.From120ths(sc.n)
			gold, err := rendertest.ReadPNG(rendertest.Golden(t, "text", "corpus@"+sc.name))
			if err != nil {
				t.Fatal(err)
			}
			got := renderCorpus(t, h, lines, target, s)
			if dump := os.Getenv("LOGIS_RENDER_DUMP"); dump != "" {
				_ = rendertest.WritePNG(filepath.Join(dump, fmt.Sprintf("corpus-%v@%s.png", target, sc.name)), got)
			}
			for i := range lines {
				y0, y1 := bandRows(i, s)
				b := compareBand(gold, got, 0, gold.Width, y0, y1)
				fmt.Fprintf(&out, "%-8v %-5s %-5d %8.3f %7.2f%% %6d %6d %18v %18v %8.1f %8.1f\n",
					target, sc.name, i+1, b.meanAbs, 100*b.over2, b.worst, b.outlierRows, b.goldInk, b.goInk, b.goldMass, b.goMass)
			}
		}
	}
	t.Log(out.String())
}

// inScope is the part of corpus line i that simple runs shape (columns
// left of x1 at f): whole Latin and CJK lines, the Latin before the
// Hebrew of line 4, and line 5 up to the skin-tone and ZWJ sequences (the
// rest needs the OpenType shaper and bidi).
func inScope(h *render.Headless, lines []string, i int, f float32) int {
	stop := map[int]string{3: "ש", 4: "\U0001F44D"}[i]
	if stop == "" {
		return 1 << 30
	}
	line := h.Fonts().Shape(lines[i], 0, 16*f, 0)
	for _, g := range line.Glyphs {
		if strings.HasPrefix(lines[i][g.Cluster:], stop) {
			return int(round(8*f)) + int(g.X)
		}
	}
	return 1 << 30
}

func round(v float32) float32 { return float32(math.Round(float64(v))) }

// The bounds measured when text.DefaultHinting was chosen (G6 handoff):
// at 1.0 within the Rust tolerance (2 levels, a stray 3); at 1.3 and 2.0
// identical ink boxes and stems, with anti-aliasing of curve pixels a few
// levels apart (FreeType vs skrifa point interpolation and rasterisers)
// and a few glyphs up to 28 levels on single pixels.
var rustBounds = map[string]struct {
	over2 float64
	worst int
}{"1.0": {0.0002, 3}, "1.3": {0.025, 30}, "2.0": {0.015, 30}}

func TestSimpleRunsMatchTheRustGoldensWithinTheirBounds(t *testing.T) {
	h := rendertest.Headless(t)
	lines := corpus(t)
	for _, sc := range corpusScales {
		s := scale.From120ths(sc.n)
		gold, err := rendertest.ReadPNG(rendertest.Golden(t, "text", "corpus@"+sc.name))
		if err != nil {
			t.Fatal(err)
		}
		h.SetFonts(pinnedFonts(t))
		got, err := h.Offscreen(corpusScene(lines), goldenSize, s)
		if err != nil {
			t.Fatal(err)
		}
		bound := rustBounds[sc.name]
		for i := range lines {
			y0, y1 := bandRows(i, s)
			b := compareBand(gold, got, 0, inScope(h, lines, i, s.Factor()), y0, y1)
			if b.goldInk != b.goInk || math.Abs(b.goMass-b.goldMass) > 0.01*b.goldMass ||
				b.over2 > bound.over2 || b.worst > bound.worst || b.meanAbs > 0.15 {
				t.Errorf("@%s line %d: %.2f%% beyond 2 levels, worst %d, mean %.3f, ink %v vs %v, mass %.1f vs %.1f",
					sc.name, i+1, 100*b.over2, b.worst, b.meanAbs, b.goInk, b.goldInk, b.goMass, b.goldMass)
			}
		}
	}
}

// The label line of tests/faces.rs's scene (faces@<scale>.png): a theme
// face (Cinzel, else serif, at 600: the pinned Noto Serif SemiBold) with
// 0.2 em of tracking. The rows below it hold colour spans, emphasis and
// underlines, the shaping slice's line features.
func TestAThemeFacesTrackedLabelMatchesTheRustGolden(t *testing.T) {
	h := rendertest.Headless(t)
	for _, sc := range corpusScales {
		s := scale.From120ths(sc.n)
		gold, err := rendertest.ReadPNG(rendertest.Golden(t, "text", "faces@"+sc.name))
		if err != nil {
			t.Fatal(err)
		}
		h.SetFonts(pinnedFonts(t))
		if err := h.SetFaces([]text.Face{
			{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 400},
			{Families: []string{"Cinzel", "serif"}, Weight: 600},
			{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 600},
		}); err != nil {
			t.Fatal(err)
		}
		label := render.NewText("INVOKE", [2]float32{8, 6}, 13, render.Hex(0xf2c14e))
		label.Face, label.Tracking = 1, 0.2
		got, err := h.Offscreen(&render.Scene{Clear: render.Hex(0x170e0b), Texts: []render.Text{label}}, scale.Size{W: 220, H: 84}, s)
		if err != nil {
			t.Fatal(err)
		}
		b := compareBand(gold, got, 0, gold.Width, 0, int(round(30*s.Factor())))
		t.Logf("@%s: %.2f%% beyond 2 levels, worst %d, mean %.3f, ink %v vs %v", sc.name, 100*b.over2, b.worst, b.meanAbs, b.goInk, b.goldInk)
		if bound := rustBounds[sc.name]; b.goldInk != b.goInk || b.over2 > bound.over2 || b.worst > bound.worst {
			t.Errorf("@%s: %.2f%% beyond 2 levels, worst %d, ink %v vs %v", sc.name, 100*b.over2, b.worst, b.goInk, b.goldInk)
		}
	}
}

// runsScene is the corpus as simple runs set it: the Latin, Latin
// Extended and CJK lines, and the Latin and emoji parts of the others.
func runsScene(t testing.TB) *render.Scene {
	lines := corpus(t)
	return corpusScene([]string{lines[0], lines[1], lines[2], "logis: (42) ok", "Praise \U0001F525⚙️\U0001F480\U0001F916 the Omnissiah"})
}

// Go's own goldens (spec §6: FreeType does not meet the Rust goldens'
// tolerance at 1.3 and 2.0), tests/goldens/go/text/runs@<scale>.png,
// reviewed against the Rust ones by the bounds above. With
// LOGIS_UPDATE_GOLDENS set they are written instead.
func TestSimpleRunsMatchTheirGoGoldens(t *testing.T) {
	h := rendertest.Headless(t)
	h.SetFonts(pinnedFonts(t))
	for _, sc := range corpusScales {
		img, err := h.Offscreen(runsScene(t), goldenSize, scale.From120ths(sc.n))
		if err != nil {
			t.Fatal(err)
		}
		path := rendertest.Golden(t, filepath.Join("go", "text"), "runs@"+sc.name)
		if os.Getenv("LOGIS_UPDATE_GOLDENS") != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := rendertest.WritePNG(path, img); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
			continue
		}
		rendertest.Check(t, img, path, rendertest.Offscreen)
	}
}
