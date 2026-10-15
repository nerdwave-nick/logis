package render_test

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// Ports of the rendering half of Rust tests/text.rs (and the face change
// of tests/faces.rs) for simple runs. Fonts are pinned as there: an
// isolated fontconfig that knows only the OFL subsets in tests/fonts.

var pinnedFiles = []string{
	"NotoSans-Regular.ttf",
	"NotoSansHebrew-Regular.ttf",
	"NotoSansArabic-Regular.ttf",
	"NotoSansCJKjp-Regular.otf",
	"NotoColorEmoji.ttf",
	"NotoSansMono-Regular.ttf",
	"NotoSansMono-SemiBold.ttf",
	"NotoSerif-SemiBold.ttf",
}

const pinnedRules = `<fontconfig>
  <alias binding="same"><family>sans-serif</family><prefer><family>Noto Sans</family></prefer></alias>
  <alias binding="same"><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias binding="same"><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>`

// pinnedFonts are the test fonts, resolved for sans-serif.
func pinnedFonts(t testing.TB) *text.Fonts {
	t.Helper()
	var paths []string
	for _, f := range pinnedFiles {
		p, err := filepath.Abs(filepath.Join("..", "..", "tests", "fonts", f))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	c, err := text.Isolated(pinnedRules, paths)
	if err != nil {
		t.Fatal(err)
	}
	f, err := text.New(c, "sans-serif")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// withFonts is a headless renderer drawing text with the pinned fonts, for
// a test that checks the pixels (rendertest.Headless).
func withFonts(t testing.TB) *render.Headless {
	h := rendertest.Headless(t)
	h.SetFonts(pinnedFonts(t))
	return h
}

// measuringFonts is withFonts for a test that reads no pixel back
// (rendertest.HeadlessForStructure).
func measuringFonts(t testing.TB) *render.Headless {
	h := rendertest.HeadlessForStructure(t)
	h.SetFonts(pinnedFonts(t))
	return h
}

func texts(ts ...render.Text) *render.Scene { return &render.Scene{Texts: ts} }

func scales() []scale.Scale { return []scale.Scale{at(120), at(156), at(240)} }

func offscreen(t *testing.T, h *render.Headless, sc *render.Scene, w, hgt uint32, s scale.Scale) *render.Image {
	t.Helper()
	img, err := h.Offscreen(sc, scale.Size{W: w, H: hgt}, s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// inkBox is the bounding box of pixels with any coverage, and their count.
func inkBox(img *render.Image) (left, top, right, bottom, n int) {
	left, top = img.Width, img.Height
	for y := range img.Height {
		for x := range img.Width {
			if img.Pixel(x, y)[3] > 0 {
				left, top, right, bottom = min(left, x), min(top, y), max(right, x), max(bottom, y)
				n++
			}
		}
	}
	return
}

func TestTextIsDrawnWhereItIsPlaced(t *testing.T) {
	h := withFonts(t)
	for _, s := range scales() {
		f := s.Factor()
		img := offscreen(t, h, texts(render.NewText("Hail", [2]float32{10, 8}, 16, white)), 120, 40, s)
		left, top, right, bottom, n := inkBox(img)
		if n <= 40 {
			t.Fatalf("@%g: only %d inked pixels", f, n)
		}
		if float32(left) < 10*f-1 || float32(left) > 13*f {
			t.Errorf("@%g: left %d", f, left)
		}
		if float32(top) < 8*f || float32(top) > 16*f {
			t.Errorf("@%g: top %d", f, top)
		}
		if float32(right) > 50*f || float32(bottom) > 8*f+16*1.3*f {
			t.Errorf("@%g: right %d, bottom %d", f, right, bottom)
		}
	}
}

func TestTextKeepsItsColour(t *testing.T) {
	h := withFonts(t)
	img := offscreen(t, h, texts(render.NewText("IIII", [2]float32{4, 4}, 48, blue)), 120, 80, at(120))
	solid := 0
	for i := 0; i < len(img.Pixels); i += 4 {
		px := [4]byte(img.Pixels[i : i+4])
		if px[3] != 255 {
			continue
		}
		solid++
		near(t, "solid glyph pixel", px, [4]byte{0x40, 0xa0, 0xff, 0xff}, 2)
	}
	if solid <= 100 {
		t.Errorf("only %d solid pixels", solid)
	}
}

func TestColourEmojiKeepTheirOwnColours(t *testing.T) {
	// A white mask would give grey pixels; the fire emoji is orange and red.
	img := offscreen(t, withFonts(t), texts(render.NewText("🔥", [2]float32{4, 4}, 40, white)), 64, 64, at(120))
	coloured := 0
	for i := 0; i < len(img.Pixels); i += 4 {
		px := img.Pixels[i : i+4]
		if px[3] == 255 && max(px[0], px[2])-min(px[0], px[2]) > 80 {
			coloured++
		}
	}
	if coloured <= 50 {
		t.Errorf("only %d coloured pixels", coloured)
	}
}

func TestTextIsDrawnOverImages(t *testing.T) {
	h := withFonts(t)
	red := &render.Image{Width: 60, Height: 30, Pixels: make([]byte, 60*30*4)}
	for i := 0; i < len(red.Pixels); i += 4 {
		red.Pixels[i], red.Pixels[i+3] = 255, 255
	}
	id, err := h.Atlas().Insert(red)
	if err != nil {
		t.Fatal(err)
	}
	sc := texts(render.NewText("MMM", [2]float32{4, 2}, 20, white))
	sc.Images = []render.ImageQuad{{Image: id, Opacity: 1}}
	img := offscreen(t, h, sc, 60, 30, at(120))
	whiteN := 0
	for i := 0; i < len(img.Pixels); i += 4 {
		if [4]byte(img.Pixels[i:i+4]) == [4]byte{255, 255, 255, 255} {
			whiteN++
		}
	}
	if whiteN <= 30 {
		t.Errorf("only %d white text pixels over the red image", whiteN)
	}
}

func TestTextOutputStaysPremultiplied(t *testing.T) {
	h := withFonts(t)
	ink := render.Hex(0xe8d8b0)
	sc := texts(
		render.NewText("The machine spirit wakes: 0123456789", [2]float32{8, 6}, 16, ink),
		render.NewText("機械の精霊 万机之神 기계 정령", [2]float32{8, 30}, 16, ink.WithAlpha(0.6)),
		render.NewText("Praise 🔥💀🤖 the Omnissiah", [2]float32{8, 54}, 16, ink),
	)
	for _, s := range scales() {
		premultiplied(t, offscreen(t, h, sc, 330, 90, s))
	}
}

func TestALineBreakStaysOnOneLine(t *testing.T) {
	h := withFonts(t)
	broken := offscreen(t, h, texts(render.NewText("a\nb", [2]float32{4, 4}, 16, white)), 60, 30, at(120))
	spaced := offscreen(t, h, texts(render.NewText("a b", [2]float32{4, 4}, 16, white)), 60, 30, at(120))
	if string(broken.Pixels) != string(spaced.Pixels) {
		t.Error("a line break is not drawn as a space")
	}
}

func TestWithoutFontsTextIsSkipped(t *testing.T) {
	img := offscreen(t, rendertest.Headless(t), texts(render.NewText("mute", [2]float32{4, 4}, 16, white)), 60, 30, at(120))
	if _, _, _, _, n := inkBox(img); n != 0 {
		t.Errorf("%d inked pixels without fonts", n)
	}
}

func TestChangingTheFacesReshapesTheText(t *testing.T) {
	h := withFonts(t)
	if err := h.SetFaces([]text.Face{{Families: []string{"monospace"}, Weight: 400}}); err != nil {
		t.Fatal(err)
	}
	sc := texts(render.NewText("do-not-disturb", [2]float32{8, 8}, 16, white))
	mono := offscreen(t, h, sc, 220, 36, at(120))
	if err := h.SetFaces([]text.Face{{Families: []string{"serif"}, Weight: 600}}); err != nil {
		t.Fatal(err)
	}
	if serif := offscreen(t, h, sc, 220, 36, at(120)); string(serif.Pixels) == string(mono.Pixels) {
		t.Error("the old shapes were reused")
	}
}

func TestDrawingAgainUsesTheGlyphsAlreadyInTheAtlas(t *testing.T) {
	h := measuringFonts(t)
	sc := texts(render.NewText("The machine spirit wakes", [2]float32{4, 4}, 16, white))
	first := offscreen(t, h, sc, 240, 30, at(156))
	glyphs := h.GlyphCount()
	again := offscreen(t, h, sc, 240, 30, at(156))
	if h.GlyphCount() != glyphs {
		t.Errorf("%d glyphs, then %d", glyphs, h.GlyphCount())
	}
	rendertest.SkipPixels(t)
	if string(first.Pixels) != string(again.Pixels) {
		t.Error("drawing again changed the image")
	}
}
