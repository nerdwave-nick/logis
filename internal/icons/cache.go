package icons

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Icons by name, ready to draw (Rust src/icons/cache.rs): looked up once
// per (name, size, scale), rasterised and uploaded once per (file, pixel
// size), misses remembered — a missing or broken icon costs one lookup,
// not one per frame.

// Inserter takes images into the renderer's atlas; *render.Atlas is one.
type Inserter interface {
	Insert(*render.Image) (render.ImageID, error)
}

// NotFoundError: no theme, hicolor or pixmaps directory has the icon.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no icon theme, hicolor or pixmaps directory holds the icon '%s'", e.Name)
}

type namedKey struct {
	name  string
	size  uint32
	scale scale.Scale
}

type fileKey struct {
	path   string
	pixels int
}

type entry struct {
	id  render.ImageID
	err error
}

// Cache is the icon cache. It owns the loaded theme chain; the atlas
// belongs to the renderer and is passed in. Like the renderer, it is used
// from one goroutine.
type Cache struct {
	themes *Themes
	named  map[namedKey]entry
	files  map[fileKey]entry
}

// NewCache is an empty cache over themes.
func NewCache(themes *Themes) *Cache {
	return &Cache{themes: themes, named: map[namedKey]entry{}, files: map[fileKey]entry{}}
}

// Themes is the theme chain lookups use.
func (c *Cache) Themes() *Themes { return c.themes }

// Image is the image for icon name at size logical pixels on an output of
// scale s, rasterised at s.Physical(size) pixels square: draw it with an
// ImageQuad in frames of that scale. Errors are *NotFoundError,
// *RasterError or the atlas's refusal, each remembered.
func (c *Cache) Image(atlas Inserter, name string, size uint32, s scale.Scale) (render.ImageID, error) {
	key := namedKey{name, size, s}
	if known, ok := c.named[key]; ok {
		return known.id, known.err
	}
	var result entry
	if path, ok := c.themes.Find(name, size, s); !ok {
		result.err = &NotFoundError{Name: name}
	} else {
		pixels := int(s.Physical(scale.Size{W: size, H: size}).W)
		fk := fileKey{path, pixels}
		var seen bool
		if result, seen = c.files[fk]; !seen {
			result = insert(atlas, path, pixels)
			c.files[fk] = result
		}
	}
	c.named[key] = result
	return result.id, result.err
}

func insert(atlas Inserter, path string, pixels int) entry {
	img, err := Rasterise(path, pixels)
	if err != nil {
		return entry{err: err}
	}
	id, err := atlas.Insert(img)
	return entry{id, err}
}

// Source is the icons of one frame: names to atlas images at the frame's
// scale.
type Source interface {
	// Image is icon name at size logical pixels, if it is found and
	// drawable.
	Image(name string, size uint32) (render.ImageID, bool)
}

// NoIcons is no icons at all (a scene drawn without an icon cache).
type NoIcons struct{}

// Image is never found.
func (NoIcons) Image(string, uint32) (render.ImageID, bool) { return 0, false }

// FrameIcons are the icons of a frame of Scale, from Cache into Atlas.
type FrameIcons struct {
	Cache *Cache
	Atlas Inserter
	Scale scale.Scale
}

// Image is icon name at size logical pixels.
func (f *FrameIcons) Image(name string, size uint32) (render.ImageID, bool) {
	id, err := f.Cache.Image(f.Atlas, name, size, f.Scale)
	return id, err == nil
}
