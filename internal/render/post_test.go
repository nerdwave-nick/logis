package render_test

import (
	"bytes"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The post-processing pass (spec §4.3, §4.9): scanlines, grain, flicker,
// vignette, bloom, curvature and chromatic aberration over the panel's
// body, each a strength where 0 is off. Read back pixel by pixel; the time
// is frozen, so every term is deterministic.

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

func bodyPost() render.Post { return render.Post{Rect: postBody, CRT: effects.Defaults()} }

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
	p.CRT.Scanlines.Strength = 0.5
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
	p.CRT.Roundness.Vignette = 0.8
	img := postRender(t, rendertest.Headless(t), postScene(p), scale.One)
	centre, corner := luma(img.Pixel(60, 45)), luma(img.Pixel(12, 12))
	if centre <= corner+100 || centre < 3*0x80-6 {
		t.Errorf("centre %d, corner %d", centre, corner)
	}
}

func TestPostGrainIsTheSameAtAFrozenTimeAndMovesWithTime(t *testing.T) {
	h := rendertest.Headless(t)
	p := bodyPost()
	p.CRT.Grain.Strength = 0.6
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
		p.CRT.Flicker.Strength, p.Time = 0.5, at
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
	p.CRT.Glow.Strength = 0.8
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
	p.CRT.Roundness.Curvature = 1
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
	p.CRT.Aberration.Amount = 1
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
	p.CRT.Scanlines.Strength, p.CRT.Roundness.Vignette, p.CRT.Grain.Strength = 0.5, 0.4, 0.3
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

// Curvature bends the whole panel as one tube face (cd816303), not only
// what lies inside a straight border: the middles of its edges stay, its
// corners draw in, so the outline turns convex and what the bend uncovers
// beyond it is the frame there (here transparent); and nothing of the
// unbent picture is left behind (no ghost of the unbent text's alpha in the
// dark it uncovers, over a light desktop).
func TestPostCurvatureBendsTheWholePanel(t *testing.T) {
	body := solidRect(postBody, render.Hex(0x808080).WithAlpha(0.9))
	body.Border = render.Border{Width: 2, Paint: render.Solid(render.Hex(0x2fd36a))}
	bar := solidRect([4]float32{14, 14, 40, 5}, render.Hex(0xffffff))
	p := bodyPost()
	p.CRT.Roundness.Curvature, p.Inset = 1, 2
	sc := &render.Scene{Clear: render.Transparent, Shapes: []render.RoundedRect{body, bar}, Post: p}
	img := postRender(t, rendertest.Headless(t), sc, scale.One)
	if top := img.Pixel(60, 10); top[1] <= 150 || top[0] >= 100 || top[3] != 255 {
		t.Errorf("the top edge's middle moved: %v", top)
	}
	if corner := img.Pixel(11, 11); corner[3] > 40 {
		t.Errorf("the body's corner did not draw in: %v", corner)
	}
	for y := 12; y < 78; y++ {
		for x := 12; x < 108; x++ {
			if px := img.Pixel(x, y); px[3] == 255 && luma(px) < 3*40 {
				t.Fatalf("(%d, %d) is dark yet opaque, a ghost of the unbent picture: %v", x, y, px)
			}
		}
	}
}

// The pass covers the whole surface: bent, the glow around the panel
// follows its outline.
func TestPostTheGlowBendsWithThePanel(t *testing.T) {
	h := rendertest.Headless(t)
	body := solidRect(postBody, render.Hex(0x101010))
	body.Glow = render.Glow{Radius: 9, Color: render.Hex(0xb30f0d)}
	p := bodyPost()
	p.CRT.Roundness.Vignette = 0.1
	flat := postRender(t, h, &render.Scene{Clear: render.Transparent, Shapes: []render.RoundedRect{body}, Post: p}, scale.One)
	p.CRT.Roundness.Curvature = 0.5
	bent := postRender(t, h, &render.Scene{Clear: render.Transparent, Shapes: []render.RoundedRect{body}, Post: p}, scale.One)
	if a, b := flat.Pixel(7, 7), bent.Pixel(7, 7); a == b {
		t.Errorf("the glow beyond the body's corner stayed: %v", a)
	}
	if a, b := flat.Pixel(60, 5), bent.Pixel(60, 5); luma(a) == 0 || a != b && luma(b) < luma(a)/2 {
		t.Errorf("the glow beside an edge's middle: %v, bent %v", a, b)
	}
}

// Regions are further screens of glass (the tab strip's plates): the
// effects cover them as they cover the body, the frame between them stays
// as drawn, and only the first MaxRegions count.
func TestPostRegionsAreGlassToo(t *testing.T) {
	h := rendertest.Headless(t)
	body := [4]float32{10, 40, 100, 45}
	plate := [4]float32{10, 6, 45, 28}
	sc := func(regions []render.Region) *render.Scene {
		p := render.Post{Rect: body, Regions: regions, CRT: effects.Defaults()}
		p.CRT.Scanlines.Strength = 0.6
		return &render.Scene{Clear: render.Hex(0), Post: p, Shapes: []render.RoundedRect{
			solidRect(body, render.Hex(0x808080)), solidRect(plate, render.Hex(0x808080))}}
	}
	column := func(img *render.Image) []int {
		var ls []int
		for y := 9; y < 31; y++ {
			ls = append(ls, luma(img.Pixel(30, y)))
		}
		return ls
	}
	plain := postRender(t, h, sc(nil), scale.One)
	if spread(column(plain)) != 0 {
		t.Fatalf("without regions the plate has scanlines: %v", column(plain))
	}
	glass := postRender(t, h, sc([]render.Region{{Rect: plate, Radius: 4}}), scale.One)
	if spread(column(glass)) <= 60 {
		t.Errorf("the plate has no scanlines: %v", column(glass))
	}
	if a, b := plain.Pixel(80, 20), glass.Pixel(80, 20); a != b {
		t.Errorf("beside the plate: %v, with the region %v", a, b)
	}
	far := render.Region{Rect: [4]float32{-50, -50, 10, 10}}
	fifth := postRender(t, h, sc([]render.Region{far, far, far, far, {Rect: plate, Radius: 4}}), scale.One)
	if spread(column(fifth)) != 0 {
		t.Errorf("a fifth region counted: %v", column(fifth))
	}
}

// The pass reads the frame drawn before it at the target's size; a target
// of another size afterwards is drawn whole (the intermediate frame
// follows the target).
func TestPostTheIntermediateFrameFollowsTheTargetsSize(t *testing.T) {
	h := rendertest.Headless(t)
	p := bodyPost()
	p.CRT.Roundness.Vignette = 0.5
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
