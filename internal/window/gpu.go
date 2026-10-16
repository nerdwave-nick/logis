package window

import (
	"unsafe"

	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// gpu is what a window draws with: in logis the EGL context on the
// session's display, woken when the window opens.
type gpu interface {
	newSurface(surface *wlproto.WlSurface, size scale.Size) (presenter, error)
	info() egl.Info
	close()
}

// presenter presents one surface's frames: an EGL window surface.
type presenter interface {
	resize(size scale.Size)
	makeCurrent() error
	swap() error
	destroy()
}

// openGPU wakes the GPU on a wl_display*.
type openGPU func(display unsafe.Pointer) (gpu, error)

// openEGL is the EGL Wayland platform with an OpenGL ES 3 context.
func openEGL(display unsafe.Pointer) (gpu, error) {
	c, err := egl.NewWayland(display)
	if err != nil {
		return nil, err
	}
	return eglGPU{c}, nil
}

type eglGPU struct{ c *egl.Context }

func (g eglGPU) info() egl.Info { return g.c.Info() }
func (g eglGPU) close()         { g.c.Close() }
func (g eglGPU) newSurface(s *wlproto.WlSurface, size scale.Size) (presenter, error) {
	w, err := g.c.NewWindow(s.Ptr(), size)
	if err != nil {
		return nil, err
	}
	return eglPresenter{w}, nil
}

type eglPresenter struct{ w *egl.Window }

func (p eglPresenter) resize(size scale.Size) { p.w.Resize(size) }
func (p eglPresenter) makeCurrent() error     { return p.w.MakeCurrent() }
func (p eglPresenter) swap() error            { return p.w.Swap() }
func (p eglPresenter) destroy()               { p.w.Destroy() }
