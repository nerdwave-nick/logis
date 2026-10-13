package icons

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/svg"
)

// Icon files to pixels (Rust src/icons/raster.rs): SVG through logis's
// own SVG subset, PNG through image/png, both into a square, premultiplied
// RGBA8 image of the requested pixel size, the content fitted with its
// aspect ratio kept and centred.

// MaxIconPixels is the largest icon side logis rasterises, in pixels.
const MaxIconPixels = 1024

// RasterError is why an icon file could not be rasterised.
type RasterError struct {
	// Path is the icon file.
	Path string
	// Reason is what went wrong, in plain words.
	Reason string
}

func (e *RasterError) Error() string {
	return fmt.Sprintf("the icon %s resists the rite of rasterisation: %s", e.Path, e.Reason)
}

// Rasterise renders the PNG or SVG at path into a pixels × pixels image.
// SVG features outside logis's subset are left out (see package svg).
func Rasterise(path string, pixels int) (*render.Image, error) {
	img, _, err := rasteriseReport(path, pixels)
	return img, err
}

// rasteriseReport is Rasterise with what the SVG subset left out.
func rasteriseReport(path string, pixels int) (*render.Image, []string, error) {
	fail := func(format string, args ...any) error {
		return &RasterError{Path: path, Reason: fmt.Sprintf(format, args...)}
	}
	if pixels < 1 || pixels > MaxIconPixels {
		return nil, nil, fail("%d pixels is outside 1..=%d", pixels, MaxIconPixels)
	}
	decode := map[string]func([]byte, int) (*image.RGBA, []string, error){
		".svg": decodeSVG, ".png": decodePNG,
	}[strings.ToLower(filepath.Ext(path))]
	if decode == nil {
		return nil, nil, fail("only PNG and SVG icons are supported")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fail("%s", errorReason(err))
	}
	out, report, err := decode(data, pixels)
	if err != nil {
		return nil, nil, fail("%s", err.Error())
	}
	return &render.Image{Width: pixels, Height: pixels, Pixels: out.Pix}, report, nil
}

// errorReason is an error's words without Go's operation and path prefix.
func errorReason(err error) string {
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		return pe.Err.Error()
	}
	return err.Error()
}

func decodeSVG(data []byte, pixels int) (*image.RGBA, []string, error) {
	doc, err := svg.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	w, h := doc.Size()
	out := image.NewRGBA(image.Rect(0, 0, pixels, pixels))
	report := doc.Render(out, fit(w, h, pixels))
	return out, report, nil
}

// fit scales w × h to fit a pixels square and centres it.
func fit(w, h float64, pixels int) svg.Matrix {
	side := float64(pixels)
	s := min(side/w, side/h)
	return svg.Translate((side-w*s)/2, (side-h*s)/2).Mul(svg.Scale(s, s))
}
