package render_test

import (
	"bytes"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The post-processing pass (spec §4.3, §4.9; Rust tests/post.rs):
// scanlines, grain, flicker, vignette, bloom, curvature and chromatic
// aberration over the panel's body, each a strength where 0 is off. Read
// back pixel by pixel; the time is frozen, so every term is deterministic.

var (
	postLogical = scale.Size{W: 120, H: 90}
	// postBody is the body the effects cover.
	postBody = [4]float32{10, 10, 100, 70}
)

func postRender(t *testing.T, h *render.Headless, sc *render.Scene, s scale.Scale) *render.Image {
	t.Helper()
	img, err := h.Offscreen(sc, postLogical, s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func solidRect(rect [4]float32, c render.Color) render.RoundedRect {
	return render.RoundedRect{Rect: rect, Fill: render.Solid(c)}
}

// postScene is a grey body on a black surface, with extra shapes over it.
func postScene(post render.Post, extra ...render.RoundedRect) *render.Scene {
	return &render.Scene{
		Clear:  render.Hex(0x000000),
		Shapes: append([]render.RoundedRect{solidRect(postBody, render.Hex(0x808080))}, extra...),
		Post:   post,
	}
}

func bodyPost() render.Post { return render.Post{Rect: postBody} }

func luma(px [4]byte) int { return int(px[0]) + int(px[1]) + int(px[2]) }

func spread(ls []int) int { return slicesMax(ls) - slicesMin(ls) }

func slicesMax(ls []int) int {
	m := ls[0]
	for _, l := range ls {
		m = max(m, l)
	}
	return m
}

func slicesMin(ls []int) int {
	m := ls[0]
	for _, l := range ls {
		m = min(m, l)
	}
	return m
}

func TestPostStrengthsOfZeroChangeNothing(t *testing.T) {
	h := rendertest.Headless(t)
	plain := postRender(t, h, postScene(render.Post{}), scale.One)
	off := postRender(t, h, postScene(bodyPost()), scale.One)
	if !bytes.Equal(plain.Pixels, off.Pixels) {
		t.Error("a pass with every effect off changed the image")
	}
}

func TestPostScanlinesDarkenRowsInsideTheBodyOnly(t *testing.T) {
	p := bodyPost()
	p.Scanlines = 0.5
	img := postRender(t, rendertest.Headless(t), postScene(p), scale.One)
	var column []int
	for y := 12; y < 78; y++ {
		column = append(column, luma(img.Pixel(60, y)))
	}
	dark, light := slicesMin(column), slicesMax(column)
	if light-dark <= 60 {
		t.Errorf("rows %v", column)
	}
	darkened := 0
	for _, l := range column {
		if l < (dark+light)/2 {
			darkened++
		}
	}
	if darkened <= 10 || darkened >= 50 {
		t.Errorf("%d dark rows of 66: every few rows, not one dark band", darkened)
	}
	if px := img.Pixel(5, 40); px != [4]byte{0, 0, 0, 255} {
		t.Errorf("outside the body: %v", px)
	}
}

func TestPostAVignetteDarkensTheEdgesMoreThanTheCentre(t *testing.T) {
	p := bodyPost()
	p.Vignette = 0.8
	img := postRender(t, rendertest.Headless(t), postScene(p), scale.One)
	centre, corner := luma(img.Pixel(60, 45)), luma(img.Pixel(12, 12))
	if centre <= corner+100 || centre < 3*0x80-6 {
		t.Errorf("centre %d, corner %d", centre, corner)
	}
}

func TestPostGrainIsTheSameAtAFrozenTimeAndMovesWithTime(t *testing.T) {
	h := rendertest.Headless(t)
	p := bodyPost()
	p.Grain = 0.6
	a := postRender(t, h, postScene(p), scale.One)
	b := postRender(t, h, postScene(p), scale.One)
	if !bytes.Equal(a.Pixels, b.Pixels) {
		t.Error("grain at one time differs between frames")
	}
	var lumas []int
	for x := 20; x < 60; x++ {
		lumas = append(lumas, luma(a.Pixel(x, 40)))
	}
	if spread(lumas) <= 20 {
		t.Errorf("%v", lumas)
	}
	p.Time = 1.5
	if later := postRender(t, h, postScene(p), scale.One); bytes.Equal(a.Pixels, later.Pixels) {
		t.Error("grain did not move with time")
	}
}

func TestPostFlickerTremblesTheBrightnessOverTime(t *testing.T) {
	h := rendertest.Headless(t)
	var lumas []int
	for _, at := range []float32{0, 0.01, 0.02, 0.033, 0.05} {
		p := bodyPost()
		p.Flicker, p.Time = 0.5, at
		lumas = append(lumas, luma(postRender(t, h, postScene(p), scale.One).Pixel(60, 45)))
	}
	if spread(lumas) <= 15 {
		t.Errorf("%v", lumas)
	}
}

func TestPostBloomSpreadsLightFromBrightParts(t *testing.T) {
	h := rendertest.Headless(t)
	dot := solidRect([4]float32{58, 43, 4, 4}, render.Hex(0xffffff))
	dark := func(p render.Post) *render.Scene {
		return postScene(p, solidRect(postBody, render.Hex(0x101010)), dot)
	}
	plain := postRender(t, h, dark(bodyPost()), scale.One)
	p := bodyPost()
	p.Bloom = 0.8
	bloomed := postRender(t, h, dark(p), scale.One)
	if near, was := luma(bloomed.Pixel(66, 45)), luma(plain.Pixel(66, 45)); near <= was+15 {
		t.Errorf("%d vs %d", near, was)
	}
	if far, was := luma(bloomed.Pixel(100, 70)), luma(plain.Pixel(100, 70)); far-was > 3 || was-far > 3 {
		t.Errorf("bloom reached the far corner: %d vs %d", far, was)
	}
}

func TestPostCurvatureBendsStraightLinesTowardsTheEdges(t *testing.T) {
	line := solidRect([4]float32{10, 20, 100, 2}, render.Hex(0xffffff))
	p := bodyPost()
	p.Curvature = 1
	bent := postRender(t, rendertest.Headless(t), postScene(p, line), scale.One)
	brightRows := func(x int) []int {
		var rows []int
		for y := 10; y < 80; y++ {
			if luma(bent.Pixel(x, y)) > 500 {
				rows = append(rows, y)
			}
		}
		return rows
	}
	middle, edge := brightRows(60), brightRows(24)
	if len(middle) == 0 || len(edge) == 0 || edge[0] <= middle[0] {
		t.Errorf("the line does not bend: %v %v", middle, edge)
	}
}

func TestPostAberrationPartsRedFromBlueAtAnEdge(t *testing.T) {
	bar := solidRect([4]float32{40, 10, 40, 70}, render.Hex(0xffffff))
	p := bodyPost()
	p.Aberration = 1
	img := postRender(t, rendertest.Headless(t), postScene(p, solidRect(postBody, render.Hex(0)), bar), scale.One)
	for x := 34; x < 46; x++ {
		if px := img.Pixel(x, 45); int(px[0])-int(px[2]) > 60 || int(px[2])-int(px[0]) > 60 {
			return
		}
	}
	t.Error("no coloured fringe at the bar's edge")
}

func TestPostThePassScalesWithTheOutput(t *testing.T) {
	h := rendertest.Headless(t)
	p := bodyPost()
	p.Scanlines, p.Vignette, p.Grain = 0.5, 0.4, 0.3
	for _, s := range []scale.Scale{scale.From120ths(156), scale.FromInteger(2)} {
		img := postRender(t, h, postScene(p), s)
		size := s.Physical(postLogical)
		if img.Width != int(size.W) || img.Height != int(size.H) {
			t.Errorf("%dx%d at %v", img.Width, img.Height, s)
		}
		if px := img.Pixel(2, img.Height/2); px != [4]byte{0, 0, 0, 255} {
			t.Errorf("outside the body: %v", px)
		}
	}
}

// Curved glass bows the screen inside the panel's edge, never through it:
// what the curve uncovers is the dark glass, as opaque as the body, so a
// compositor's blur behind the body never shows through, and the edge (the
// border) stays straight.
func TestPostCurvatureNeverUncoversWhatLiesBehindTheBody(t *testing.T) {
	body := solidRect(postBody, render.Hex(0x808080))
	body.Border = render.Border{Width: 2, Paint: render.Solid(render.Hex(0x2fd36a))}
	p := bodyPost()
	p.Curvature, p.Inset = 1, 2
	sc := &render.Scene{Clear: render.Transparent, Shapes: []render.RoundedRect{body}, Post: p}
	img := postRender(t, rendertest.Headless(t), sc, scale.One)
	for y := 10; y < 80; y++ {
		for x := 10; x < 110; x++ {
			if px := img.Pixel(x, y); px[3] != 255 {
				t.Fatalf("(%d, %d) is see-through: %v", x, y, px)
			}
		}
	}
	if top := img.Pixel(60, 10); top[1] <= 150 || top[0] >= 100 {
		t.Errorf("the border moved: %v", top)
	}
}

// The pass reads the frame drawn before it at the target's size; a target
// of another size afterwards is drawn whole (the intermediate frame
// follows the target).
func TestPostTheIntermediateFrameFollowsTheTargetsSize(t *testing.T) {
	h := rendertest.Headless(t)
	p := bodyPost()
	p.Vignette = 0.5
	small := postRender(t, h, postScene(p), scale.One)
	big := postRender(t, h, postScene(p), scale.FromInteger(2))
	again := postRender(t, h, postScene(p), scale.One)
	if !bytes.Equal(small.Pixels, again.Pixels) {
		t.Error("a frame after a bigger one differs")
	}
	if px := big.Pixel(120, 90); luma(px) < 3*0x70 {
		t.Errorf("the bigger frame's centre: %v", px)
	}
}
