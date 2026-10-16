package render

import (
	"fmt"
	"slices"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Target is where a frame goes.
type Target struct {
	// FBO is the framebuffer to draw into: 0 is the current window
	// surface, others an offscreen gles.Target.
	FBO uint32
	// Size is its size in buffer pixels.
	Size scale.Size
	// Scale is logical → buffer pixels.
	Scale scale.Scale
}

// Renderer draws scenes: the sdf program for shapes and the image program
// for atlas quads, with their buffers and the atlas.
type Renderer struct {
	sdf               *gles.Program
	sdfSize, sdfScale int32
	shapes            gles.VertexArray
	shapeBuf          gles.Buffer
	packed            []float32 // this frame's instances, reused
	images            *imagePass
	glyphs            *glyphPass
	// passes are the post-processing passes in order (the glitch pass,
	// the CRT pass, then the custom pass); frames the intermediate frames
	// they read.
	passes []pass
	frames frames
	custom *customPass
}

// New compiles the shaders and makes the buffers and the atlas, on the
// context current on the calling (locked) thread.
func New() (*Renderer, error) {
	sdf, err := gles.NewProgram(sdfVertex, sdfFragment)
	if err != nil {
		return nil, fmt.Errorf("sdf: %w", err)
	}
	r := &Renderer{
		sdf:      sdf,
		sdfSize:  sdf.Uniform("u_size"),
		sdfScale: sdf.Uniform("u_scale"),
		shapes:   gles.NewVertexArray(),
		shapeBuf: gles.NewBuffer(),
	}
	r.shapes.Bind()
	r.shapeBuf.Bind()
	for i := range instanceFloats / 4 {
		gles.InstanceAttrib(i, 4, instanceFloats, i*4)
	}
	gles.VertexArray(0).Bind()
	if r.images, err = newImagePass(); err != nil {
		r.Close()
		return nil, fmt.Errorf("images: %w", err)
	}
	if r.glyphs, err = newGlyphPass(); err != nil {
		r.Close()
		return nil, fmt.Errorf("glyphs: %w", err)
	}
	glitch, err := newGlitchPass()
	if err != nil {
		r.Close()
		return nil, err
	}
	r.passes = append(r.passes, glitch)
	crt, err := newCRTPass()
	if err != nil {
		r.Close()
		return nil, err
	}
	r.passes = append(r.passes, crt)
	r.custom = newCustomPass()
	r.passes = append(r.passes, r.custom)
	if err := gles.Err(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// SetLog says in the log, through logf, what goes wrong while drawing: a
// custom shader that is skipped.
func (r *Renderer) SetLog(logf func(string)) { r.custom.logf = logf }

// Atlas is the image atlas the scene's ImageQuads refer to.
func (r *Renderer) Atlas() *Atlas { return r.images.atlas }

// Draw draws sc into t: it clears t to sc.Clear, then paints every shape,
// then every image, then every text, back to front in premultiplied alpha,
// then the post-processing passes the scene's Post wants, if any (into a
// frame of t's size first, which the passes then draw into t). Buffers are
// streamed and the GPU is never waited for.
func (r *Renderer) Draw(t Target, sc *Scene) {
	if n := r.wanted(sc.Post); n > 0 {
		r.drawPassed(t, sc, n)
		return
	}
	r.drawScene(t, sc)
}

func (r *Renderer) drawScene(t Target, sc *Scene) {
	w, h := float32(t.Size.W), float32(t.Size.H)
	gles.BindFramebuffer(t.FBO)
	gles.Viewport(0, 0, int(t.Size.W), int(t.Size.H))
	gles.NoScissor()
	c := sc.Clear.Premultiplied()
	gles.ClearColor(c[0], c[1], c[2], c[3])
	gles.Clear()
	gles.BlendPremultiplied()
	// Text is shaped first: its underlines are drawn with the shapes.
	r.glyphs.prepare(sc.Texts, t.Scale)
	shapes := sc.Shapes
	if deco := r.glyphs.decorations(); len(deco) > 0 {
		shapes = append(slices.Clip(sc.Shapes), deco...)
	}
	if len(shapes) > 0 {
		r.packed = r.packed[:0]
		for _, s := range shapes {
			p := pack(s)
			r.packed = append(r.packed, p[:]...)
		}
		r.sdf.Use()
		gles.Uniform2f(r.sdfSize, w, h)
		gles.Uniform1f(r.sdfScale, t.Scale.Factor())
		r.shapes.Bind()
		r.shapeBuf.Stream(r.packed)
		gles.DrawQuads(len(shapes))
	}
	r.images.draw(t, sc.Images)
	r.glyphs.draw(t)
	gles.VertexArray(0).Bind()
	gles.NoBlend()
}

// Close releases the renderer's GL objects (on the same thread, before
// its context goes).
func (r *Renderer) Close() {
	if r.sdf == nil {
		return
	}
	if r.images != nil {
		r.images.close()
	}
	if r.glyphs != nil {
		r.glyphs.close()
	}
	for _, p := range r.passes {
		p.close()
	}
	r.passes = nil
	r.frames.close()
	r.sdf.Delete()
	r.shapes.Delete()
	r.shapeBuf.Delete()
	r.sdf = nil
}
