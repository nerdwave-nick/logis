package icons_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/icons"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust tests/icons.rs's cache and golden tests on the GPU: the
// icon cache over the fixture themes filling the renderer's real atlas,
// and golden renders of fixture icons at scales 1.0 and 1.3. (The atlas
// and image pass tests are internal/render's.)

var scale13 = scale.From120ths(156)

func fixtureCache(t *testing.T) *icons.Cache {
	t.Helper()
	root, err := filepath.Abs("../../tests/fixtures/icons")
	if err != nil {
		t.Fatal(err)
	}
	paths := icons.SearchPaths{
		IconDirs:   []string{filepath.Join(root, "home-icons"), filepath.Join(root, "share/icons")},
		PixmapDirs: []string{filepath.Join(root, "pixmaps")},
	}
	return icons.NewCache(icons.LoadThemes(paths, "logis-forge"))
}

func TestTheCacheFillsTheAtlasOncePerFileAndPixelSize(t *testing.T) {
	h := rendertest.Headless(t)
	c, atlas := fixtureCache(t), h.Atlas()
	cog, err := c.Image(atlas, "forge-cog", 32, scale.One)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := c.Image(atlas, "forge-cog.png", 32, scale.One)
	cog13, err := c.Image(atlas, "forge-cog", 32, scale13)
	if err != nil {
		t.Fatal(err)
	}
	if again != cog || cog13 == cog || atlas.Len() != 2 {
		t.Errorf("ids %d %d %d, %d images; want one image per pixel size", cog, again, cog13, atlas.Len())
	}
	for _, tc := range []struct {
		id   render.ImageID
		side int
	}{{cog, 32}, {cog13, 42}} {
		if w, hh, ok := atlas.SizeOf(tc.id); !ok || w != tc.side || hh != tc.side {
			t.Errorf("image %d is %dx%d, want %d square", tc.id, w, hh, tc.side)
		}
	}
	if _, err := c.Image(atlas, "no-such-icon", 32, scale.One); err == nil || atlas.Len() != 2 {
		t.Errorf("a missing icon: %v, %d images", err, atlas.Len())
	}
}

// The Rust goldens (tests/goldens/icons, straight alpha, written through
// resvg and tiny-skia) are matched within Rust's GOLDEN_TOLERANCE of 3
// levels by every PNG icon and the backdrop. The two SVG icons differ on
// anti-aliased edges: tiny-skia's supersampled coverage against
// x/image/vector's area coverage. Against an 8×8 oversampled resvg render
// at 32 px, logis's SVG pixels are the closer ones (forge-banner: mean
// 0.21 vs resvg's 1.27 levels, worst 12.0 vs 59.5; cogitator-skull: mean
// 0.58 vs 2.45, worst 6.8 vs 37.2). So the Rust goldens bound the
// outliers (measured 159 of 11904 pixels at 1.0, worst 48; 285 of 19964
// at 1.3, worst 33) and logis's own goldens in tests/goldens/go/icons,
// reviewed against the Rust ones, are matched within the offscreen
// tolerance. LOGIS_UPDATE_GOLDENS=1 rewrites logis's own.
var (
	rustTolerance = rendertest.Tolerance{Channel: 3, Outliers: 0.015}
	goTolerance   = rendertest.Offscreen
)

// goldenScene is Rust's: a backdrop panel and one icon from each lookup
// source, 32 logical pixels each, 8 apart.
func goldenScene(t *testing.T, h *render.Headless, s scale.Scale) (*render.Scene, scale.Size) {
	names := []string{
		"forge-cog",       // PNG: 32x32 at 1.0, 32x32@2 at 1.3
		"forge-banner",    // SVG, wide, from the scalable directory
		"cogitator-skull", // SVG from the parent theme
		"home-sigil",      // PNG from the earlier base dir
		"hicolor-only",    // translucent PNG from hicolor
		"pixmap-only",     // indexed PNG from pixmaps, resampled
	}
	c := fixtureCache(t)
	sc := &render.Scene{}
	for i, name := range names {
		id, err := c.Image(h.Atlas(), name, 32, s)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sc.Images = append(sc.Images, render.ImageQuad{Image: id, Origin: [2]float32{8 + 40*float32(i), 8}, Opacity: 1})
	}
	size := scale.Size{W: uint32(8 + 40*len(names)), H: 48}
	sc.Shapes = []render.RoundedRect{{
		Rect: [4]float32{2, 2, float32(size.W) - 4, 44}, Radius: 8, Fill: render.Solid(render.Hex(0x1a1410)),
	}}
	return sc, size
}

// straightGolden reads a golden stored with straight alpha (as tiny-skia
// wrote the icon goldens) premultiplied, rounding as Rust's harness does.
func straightGolden(t *testing.T, name string) *render.Image {
	t.Helper()
	img, err := rendertest.ReadPNG(rendertest.Golden(t, "icons", name))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(img.Pixels); i += 4 {
		a := uint16(img.Pixels[i+3])
		for k := range 3 {
			img.Pixels[i+k] = uint8((uint16(img.Pixels[i+k])*a + 127) / 255)
		}
	}
	return img
}

func TestGoldenIcons(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    scale.Scale
		w, h int
	}{{"icons-1.0", scale.One, 248, 48}, {"icons-1.3", scale13, 322, 62}} {
		t.Run(tc.name, func(t *testing.T) {
			h := rendertest.Headless(t) // GL is bound to this goroutine's thread
			sc, size := goldenScene(t, h, tc.s)
			img, err := h.Offscreen(sc, size, tc.s)
			if err != nil {
				t.Fatal(err)
			}
			if img.Width != tc.w || img.Height != tc.h {
				t.Fatalf("%dx%d, want %dx%d", img.Width, img.Height, tc.w, tc.h)
			}
			own := rendertest.Golden(t, filepath.Join("go", "icons"), tc.name)
			if os.Getenv("LOGIS_UPDATE_GOLDENS") != "" {
				if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := rendertest.WritePNG(own, img); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated golden %s", own)
			}
			rendertest.Check(t, img, own, goTolerance)
			c, err := rendertest.Compare(img, straightGolden(t, tc.name), rustTolerance.Channel)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s against Rust's golden: %v", tc.name, c)
			if !c.Passes(rustTolerance) {
				t.Errorf("%s against Rust's golden: %v", tc.name, c)
			}
		})
	}
}
