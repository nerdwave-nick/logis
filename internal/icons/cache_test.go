package icons

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// fakeAtlas records inserted images; fail makes inserts fail.
type fakeAtlas struct {
	images []*render.Image
	fail   error
}

func (a *fakeAtlas) Insert(img *render.Image) (render.ImageID, error) {
	if a.fail != nil {
		return 0, a.fail
	}
	a.images = append(a.images, img)
	return render.ImageID(len(a.images) - 1), nil
}

var scale13 = scale.From120ths(156)

func forgeCache(t testing.TB) *Cache { return NewCache(forge(t)) }

func cached(t *testing.T, c *Cache, a Inserter, name string, size uint32, s scale.Scale) render.ImageID {
	t.Helper()
	id, err := c.Image(a, name, size, s)
	if err != nil {
		t.Fatalf("%s at %d, %v: %v", name, size, s, err)
	}
	return id
}

func TestTheCacheRasterisesEachFileOncePerPixelSize(t *testing.T) {
	c, a := forgeCache(t), &fakeAtlas{}
	cog := cached(t, c, a, "forge-cog", 32, scale.One)
	if again := cached(t, c, a, "forge-cog", 32, scale.One); again != cog {
		t.Errorf("again: %d, want %d", again, cog)
	}
	// Another name for the same file at the same pixel size shares it.
	if other := cached(t, c, a, "forge-cog.png", 32, scale.One); other != cog {
		t.Errorf("forge-cog.png: %d, want %d", other, cog)
	}
	if len(a.images) != 1 || a.images[0].Width != 32 {
		t.Fatalf("%d images inserted", len(a.images))
	}
	// At 1.3 the same icon is 42 pixels: a second image.
	cog13 := cached(t, c, a, "forge-cog", 32, scale13)
	if cog13 == cog || len(a.images) != 2 || a.images[1].Width != 42 {
		t.Errorf("1.3: id %d, %d images", cog13, len(a.images))
	}
	if again := cached(t, c, a, "forge-cog", 32, scale13); again != cog13 || len(a.images) != 2 {
		t.Errorf("1.3 again: %d, %d images", again, len(a.images))
	}
}

func TestTheCacheRemembersMisses(t *testing.T) {
	c, a := forgeCache(t), &fakeAtlas{}
	for range 2 {
		_, err := c.Image(a, "no-such-icon", 32, scale.One)
		var nf *NotFoundError
		if !errors.As(err, &nf) || nf.Name != "no-such-icon" {
			t.Fatalf("%v, want NotFoundError", err)
		}
		if want := "no icon theme, hicolor or pixmaps directory holds the icon 'no-such-icon'"; err.Error() != want {
			t.Errorf("%q, want %q", err, want)
		}
	}
	broken := filepath.Join(fixtures(t), "pixmaps/broken.svg")
	_, err := c.Image(a, broken, 32, scale.One)
	var re *RasterError
	if !errors.As(err, &re) || !strings.HasSuffix(re.Path, "broken.svg") {
		t.Errorf("%v, want a RasterError for broken.svg", err)
	}
	if len(a.images) != 0 {
		t.Errorf("%d images inserted", len(a.images))
	}
	if got := c.Themes().Names(); !slices.Equal(got, []string{"logis-forge", "logis-cogitator", "hicolor"}) {
		t.Errorf("themes %q", got)
	}
}

func TestTheCacheRemembersAtlasRefusals(t *testing.T) {
	c := forgeCache(t)
	full := &render.AtlasFullError{Side: 8192}
	a := &fakeAtlas{fail: full}
	if _, err := c.Image(a, "forge-cog", 32, scale.One); !errors.Is(err, full) {
		t.Fatalf("%v, want the atlas's refusal", err)
	}
	a.fail = nil
	if _, err := c.Image(a, "forge-cog", 32, scale.One); !errors.Is(err, full) {
		t.Errorf("%v: the refusal was not remembered", err)
	}
}

func TestFrameIconsAskTheCacheAtTheFramesScale(t *testing.T) {
	a := &fakeAtlas{}
	var src Source = &FrameIcons{Cache: forgeCache(t), Atlas: a, Scale: scale13}
	if _, ok := src.Image("forge-cog", 32); !ok || a.images[0].Width != 42 {
		t.Errorf("forge-cog: ok %v, %d images", ok, len(a.images))
	}
	if _, ok := src.Image("no-such-icon", 32); ok {
		t.Error("a missing icon came back")
	}
	if _, ok := (NoIcons{}).Image("forge-cog", 32); ok {
		t.Error("NoIcons has an icon")
	}
}
