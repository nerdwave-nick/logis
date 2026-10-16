package e2e

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// wl_shm formats a capture may come in (little-endian 32-bit).
const (
	ShmARGB8888 = wlproto.WlShmFormatArgb8888
	ShmXRGB8888 = wlproto.WlShmFormatXrgb8888
	ShmABGR8888 = wlproto.WlShmFormatAbgr8888
	ShmXBGR8888 = wlproto.WlShmFormatXbgr8888
)

// wl_output transforms a capture is turned upright from; rotated outputs
// are not used by the test compositors.
const (
	TransformNormal     = wlproto.WlOutputTransformNormal
	Transform180        = wlproto.WlOutputTransform180
	TransformFlipped    = wlproto.WlOutputTransformFlipped
	TransformFlipped180 = wlproto.WlOutputTransformFlipped180
)

// capture is the progress of one screencopy frame.
type capture struct {
	format, width, height, stride uint32
	buffer, bufferDone, yInvert   bool
	ready, failed                 bool
}

// Capture captures the whole output (without the cursor) as opaque RGBA,
// upright, through wlr-screencopy over wl_shm (niri and sway offer both).
func (h *Harness) Capture() *render.Image {
	h.t.Helper()
	var c capture
	frame := h.screencopy.CaptureOutput(0, h.output)
	defer frame.Destroy()
	frame.OnBuffer = func(format, w, ht, stride uint32) {
		c.format, c.width, c.height, c.stride, c.buffer = format, w, ht, stride, true
	}
	frame.OnBufferDone = func() { c.bufferDone = true }
	frame.OnFlags = func(flags uint32) { c.yInvert = flags&wlproto.ZwlrScreencopyFrameV1FlagsYInvert != 0 }
	frame.OnReady = func(_, _, _ uint32) { c.ready = true }
	frame.OnFailed = func() { c.failed = true }
	v3 := frame.Version() >= 3
	if err := h.dispatchUntil("screencopy buffer", func() bool { return c.failed || c.buffer && (!v3 || c.bufferDone) }); err != nil {
		h.t.Fatal(err)
	}
	if c.failed {
		h.t.Fatal("the compositor refused the capture")
	}
	size := int(c.stride * c.height)
	path := filepath.Join(h.runtimeDir, "capture.shm")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err == nil {
		err = f.Truncate(int64(size))
	}
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	pool := h.shm.CreatePool(int(f.Fd()), int32(size))
	buffer := pool.CreateBuffer(0, int32(c.width), int32(c.height), int32(c.stride), c.format)
	frame.Copy(buffer)
	err = h.dispatchUntil("screencopy", func() bool { return c.ready || c.failed })
	buffer.Destroy()
	pool.Destroy()
	_ = f.Close()
	if err != nil {
		h.t.Fatal(err)
	}
	if c.failed {
		h.t.Fatal("the capture failed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	img, err := ToRGBA(data, c.format, int(c.width), int(c.height), int(c.stride), c.yInvert)
	if err == nil {
		img, err = Upright(img, h.transform)
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return img
}

// SettledCapture captures until two consecutive captures agree, so a
// frame the compositor has not shown yet is never compared.
func (h *Harness) SettledCapture() *render.Image {
	h.t.Helper()
	last := h.Capture()
	for range 50 {
		next := h.Capture()
		if string(next.Pixels) == string(last.Pixels) {
			return next
		}
		last = next
	}
	h.t.Fatal("the output never settled")
	return nil
}

// ToRGBA converts a little-endian 32-bit shm image to opaque RGBA rows top
// down.
func ToRGBA(data []byte, format uint32, width, height, stride int, yInvert bool) (*render.Image, error) {
	// Byte order in memory of each format's red, green and blue.
	var r, g, b int
	switch format {
	case ShmARGB8888, ShmXRGB8888:
		r, g, b = 2, 1, 0
	case ShmABGR8888, ShmXBGR8888:
		r, g, b = 0, 1, 2
	default:
		return nil, fmt.Errorf("the harness cannot read shm format %#x", format)
	}
	img := &render.Image{Width: width, Height: height, Pixels: make([]byte, 0, width*height*4)}
	for y := range height {
		row := y
		if yInvert {
			row = height - 1 - y
		}
		line := data[row*stride : row*stride+width*4]
		for i := 0; i < len(line); i += 4 {
			img.Pixels = append(img.Pixels, line[i+r], line[i+g], line[i+b], 255)
		}
	}
	return img, nil
}

// Upright applies an output's transform to a buffer-space capture, as the
// output shows it.
func Upright(img *render.Image, transform int32) (*render.Image, error) {
	var mirrorX, mirrorY bool
	switch transform {
	case TransformNormal:
	case Transform180:
		mirrorX, mirrorY = true, true
	case TransformFlipped:
		mirrorX = true
	case TransformFlipped180:
		mirrorY = true
	default:
		return nil, fmt.Errorf("the harness does not turn outputs of transform %d upright", transform)
	}
	w, ht := img.Width, img.Height
	out := &render.Image{Width: w, Height: ht, Pixels: make([]byte, 0, len(img.Pixels))}
	for y := range ht {
		sy := y
		if mirrorY {
			sy = ht - 1 - y
		}
		for x := range w {
			sx := x
			if mirrorX {
				sx = w - 1 - x
			}
			i := (sy*w + sx) * 4
			out.Pixels = append(out.Pixels, img.Pixels[i:i+4]...)
		}
	}
	return out, nil
}

// Crop is the w×h rectangle of img at (x, y); it must lie inside img.
func Crop(img *render.Image, x, y, w, h int) *render.Image {
	if x < 0 || y < 0 || x+w > img.Width || y+h > img.Height {
		panic(fmt.Sprintf("a %dx%d crop at (%d, %d) leaves the %dx%d capture", w, h, x, y, img.Width, img.Height))
	}
	out := &render.Image{Width: w, Height: h, Pixels: make([]byte, 0, w*h*4)}
	for row := y; row < y+h; row++ {
		start := (row*img.Width + x) * 4
		out.Pixels = append(out.Pixels, img.Pixels[start:start+w*4]...)
	}
	return out
}

// CropCentred crops a centred w×h surface (a layer surface without
// anchors) out of an output capture. Compositors round an odd remainder
// either way, so with a reference the offsets within one pixel of the
// centre are tried and the closest crop is returned.
func CropCentred(img *render.Image, w, h int, reference *render.Image) *render.Image {
	x, y := (img.Width-w)/2, (img.Height-h)/2
	if reference == nil {
		return Crop(img, x, y, w, h)
	}
	var best *render.Image
	bestOutliers := -1
	for _, dy := range []int{0, 1} {
		for _, dx := range []int{0, 1} {
			if x+dx+w > img.Width || y+dy+h > img.Height {
				continue
			}
			candidate := Crop(img, x+dx, y+dy, w, h)
			c, err := rendertest.Compare(candidate, reference, rendertest.Offscreen.Channel)
			if err == nil && (bestOutliers < 0 || c.Outliers < bestOutliers) {
				best, bestOutliers = candidate, c.Outliers
			}
		}
	}
	if best == nil {
		return Crop(img, x, y, w, h)
	}
	return best
}
