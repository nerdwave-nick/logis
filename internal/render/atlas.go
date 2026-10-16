package render

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/gles"
)

// ImageID is an image in a renderer's Atlas.
type ImageID uint32

const (
	// initialSide is the atlas texture's side when the renderer starts.
	initialSide = 512
	// maxSide is the largest side it grows to (or the context's limit).
	maxSide = 8192
)

// Atlas is one GPU texture holding every image the renderer draws (icons,
// later glyph-like images), each uploaded once (Rust src/render/atlas.rs).
// Pixels are premultiplied RGBA8 with sRGB-encoded channels, exactly as
// the sdf program takes its colours. When full, the texture doubles on the
// GPU and every ImageID stays valid.
type Atlas struct {
	tex     gles.Texture
	packer  *shelfPacker
	rects   [][4]uint32 // x, y, width, height in texels, by ImageID
	maxSide int
}

func newAtlas() *Atlas {
	limit := min(maxSide, gles.MaxTextureSize())
	side := min(initialSide, limit)
	return &Atlas{
		tex:     gles.NewTexture(side, side),
		packer:  newShelfPacker(uint32(side), uint32(side)),
		maxSide: limit,
	}
}

// MalformedImageError: the image has no pixels, or fewer bytes than its
// size needs.
type MalformedImageError struct{ Width, Height int }

func (e *MalformedImageError) Error() string {
	return fmt.Sprintf("a malformed %dx%d image was refused by the image atlas", e.Width, e.Height)
}

// ImageTooLargeError: a side of the image reaches the atlas's largest side.
type ImageTooLargeError struct{ Width, Height, Max int }

func (e *ImageTooLargeError) Error() string {
	return fmt.Sprintf("a %dx%d image cannot fit the image atlas's largest side of %d", e.Width, e.Height, e.Max)
}

// AtlasFullError: the atlas is at its largest and has no room left.
type AtlasFullError struct{ Side int }

func (e *AtlasFullError) Error() string {
	return fmt.Sprintf("the %dx%d image atlas is full; no reliquary remains for another image", e.Side, e.Side)
}

// Insert uploads img (premultiplied RGBA8) and returns its handle. Growing
// and uploading are queued on the GPU; nothing waits.
func (a *Atlas) Insert(img *Image) (ImageID, error) {
	w, h := img.Width, img.Height
	if w <= 0 || h <= 0 || len(img.Pixels) < w*h*4 {
		return 0, &MalformedImageError{Width: w, Height: h}
	}
	if max(w, h) >= a.maxSide {
		return 0, &ImageTooLargeError{Width: w, Height: h, Max: a.maxSide}
	}
	x, y, ok := a.packer.allocate(uint32(w), uint32(h))
	for !ok {
		side, _ := a.packer.size()
		if int(side)*2 > a.maxSide {
			return 0, &AtlasFullError{Side: int(side)}
		}
		a.grow()
		x, y, ok = a.packer.allocate(uint32(w), uint32(h))
	}
	a.tex.Upload(int(x), int(y), w, h, img.Pixels[:w*h*4])
	a.rects = append(a.rects, [4]uint32{x, y, uint32(w), uint32(h)})
	return ImageID(len(a.rects) - 1), nil
}

// SizeOf is the pixel size of image; !ok for a handle from elsewhere.
func (a *Atlas) SizeOf(image ImageID) (w, h int, ok bool) {
	r, ok := a.rect(image)
	return int(r[2]), int(r[3]), ok
}

// Len is how many images the atlas holds.
func (a *Atlas) Len() int { return len(a.rects) }

// Side is the texture's side in pixels.
func (a *Atlas) Side() int {
	side, _ := a.packer.size()
	return int(side)
}

func (a *Atlas) rect(image ImageID) ([4]uint32, bool) {
	if int(image) >= len(a.rects) {
		return [4]uint32{}, false
	}
	return a.rects[image], true
}

// grow doubles the texture and copies the old one into its top-left corner
// on the GPU, so every placed image keeps its texels.
func (a *Atlas) grow() {
	old := a.Side()
	a.packer.grow()
	tex := gles.NewTexture(a.Side(), a.Side())
	gles.CopyTexture(a.tex, tex, old, old)
	a.tex.Delete()
	a.tex = tex
}

func (a *Atlas) close() { a.tex.Delete() }
