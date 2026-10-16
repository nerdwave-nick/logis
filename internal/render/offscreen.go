package render

import (
	"fmt"
	"runtime"

	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Image is a rendered frame or an atlas image: premultiplied RGBA8, rows
// top to bottom, no padding.
type Image struct {
	Width, Height int
	// Pixels are Width × Height × 4 bytes.
	Pixels []byte
}

// Pixel is the premultiplied RGBA of one pixel.
func (i *Image) Pixel(x, y int) [4]byte {
	if x < 0 || y < 0 || x >= i.Width || y >= i.Height {
		panic(fmt.Sprintf("render: pixel (%d, %d) is outside a %dx%d image", x, y, i.Width, i.Height))
	}
	o := (y*i.Width + x) * 4
	return [4]byte(i.Pixels[o : o+4])
}

// ReadbackError is why an offscreen render produced no image.
type ReadbackError struct{ Reason string }

func (e *ReadbackError) Error() string {
	return "the GPU withheld the rendered image: " + e.Reason
}

// Offscreen renders sc at logical size and scale s into an RGBA8 texture
// and reads it back: the image is s.Physical(logical) pixels. Unlike Draw
// it waits for the GPU — for goldens and checks only, never the frame loop.
func (r *Renderer) Offscreen(sc *Scene, logical scale.Size, s scale.Scale) (*Image, error) {
	size := s.Physical(logical)
	w, h := int(size.W), int(size.H)
	target := gles.NewTarget(w, h)
	defer target.Delete()
	r.Draw(Target{FBO: target.FBO, Size: size, Scale: s}, sc)
	gles.BindFramebuffer(target.FBO)
	bottomUp := gles.ReadPixels(0, 0, w, h)
	gles.BindFramebuffer(0)
	if err := gles.Err(); err != nil {
		return nil, &ReadbackError{Reason: err.Error()}
	}
	img := &Image{Width: w, Height: h, Pixels: make([]byte, len(bottomUp))}
	row := w * 4
	for y := range h {
		copy(img.Pixels[y*row:(y+1)*row], bottomUp[(h-1-y)*row:(h-y)*row])
	}
	return img, nil
}

// Headless is a renderer on its own offscreen GL context (EGL surfaceless),
// for goldens and tests. The goroutine that opens it owns it: Open locks it
// to its OS thread, Close unlocks it.
type Headless struct {
	ctx *egl.Context
	*Renderer
}

// OpenHeadless wakes an offscreen OpenGL ES 3 context and a renderer on it.
func OpenHeadless() (*Headless, error) {
	runtime.LockOSThread()
	ctx, err := egl.NewSurfaceless()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	r, err := New()
	if err != nil {
		ctx.Close()
		runtime.UnlockOSThread()
		return nil, err
	}
	return &Headless{ctx: ctx, Renderer: r}, nil
}

// Info names the GL implementation.
func (h *Headless) Info() egl.Info { return h.ctx.Info() }

// Close releases the renderer and its context and unlocks the owner
// goroutine.
func (h *Headless) Close() {
	if h.ctx == nil {
		return
	}
	h.Renderer.Close()
	h.ctx.Close()
	h.ctx = nil
	runtime.UnlockOSThread()
}
