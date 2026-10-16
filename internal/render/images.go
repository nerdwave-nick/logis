package render

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// quadFloats is floats per image instance: rect, then atlas texel and
// opacity.
const quadFloats = 8

// imagePass draws a scene's ImageQuads from the atlas after the shapes,
// with the same premultiplied blending.
type imagePass struct {
	atlas        *Atlas
	program      *gles.Program
	size, sample int32
	vao          gles.VertexArray
	buf          gles.Buffer
	packed       []float32
}

func newImagePass() (*imagePass, error) {
	program, err := gles.NewProgram(imageVertex, imageFragment)
	if err != nil {
		return nil, err
	}
	p := &imagePass{
		atlas:   newAtlas(),
		program: program,
		size:    program.Uniform("u_size"),
		sample:  program.Uniform("u_atlas"),
		vao:     gles.NewVertexArray(),
		buf:     gles.NewBuffer(),
	}
	p.vao.Bind()
	p.buf.Bind()
	gles.InstanceAttrib(0, 4, quadFloats, 0)
	gles.InstanceAttrib(1, 4, quadFloats, 4)
	gles.VertexArray(0).Bind()
	return p, nil
}

// draw draws quads into t, skipping invisible ones and images not in the
// atlas. The blend state and target are already set.
func (p *imagePass) draw(t Target, quads []ImageQuad) {
	p.packed = p.packed[:0]
	for _, q := range quads {
		rect, ok := p.atlas.rect(q.Image)
		if !ok || !(q.Opacity > 0) {
			continue
		}
		packed := packQuad(q.Origin, rect, t.Scale, q.Opacity)
		p.packed = append(p.packed, packed[:]...)
	}
	if len(p.packed) == 0 {
		return
	}
	p.program.Use()
	gles.Uniform2f(p.size, float32(t.Size.W), float32(t.Size.H))
	gles.Uniform1i(p.sample, 0)
	p.atlas.tex.Bind()
	p.vao.Bind()
	p.buf.Stream(p.packed)
	gles.DrawQuads(len(p.packed) / quadFloats)
}

func (p *imagePass) close() {
	p.atlas.close()
	p.program.Delete()
	p.vao.Delete()
	p.buf.Delete()
}

// packQuad is one instance: the image's rect in buffer pixels (its corner
// snapped to the pixel grid, rounding half away from zero; its size its
// own pixel size), its top-left atlas texel and its opacity.
func packQuad(origin [2]float32, rect [4]uint32, s scale.Scale, opacity float32) [quadFloats]float32 {
	// The product in f32, then rounded.
	f := s.Factor()
	snap := func(v float32) float32 { return float32(math.Round(float64(v * f))) }
	return [quadFloats]float32{
		snap(origin[0]), snap(origin[1]), float32(rect[2]), float32(rect[3]),
		float32(rect[0]), float32(rect[1]), clamp01(opacity), 0,
	}
}
