package render

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// postPass is the post-processing pass (Rust src/render/post.rs): when a
// scene asks for effects, the renderer draws it into an intermediate frame
// of the target's size, then this pass draws that frame into the target
// with the effects applied. The frame is kept while the target's size
// stays; nothing here waits on the GPU.
type postPass struct {
	program                                                *gles.Program
	size, scale, time, body, radius, inset, a, b, frameLoc int32
	vao                                                    gles.VertexArray
	frame                                                  *gles.Target
}

func newPostPass() (*postPass, error) {
	prog, err := gles.NewProgram(postVertex, postFragment)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	return &postPass{
		program: prog, vao: gles.NewVertexArray(),
		size: prog.Uniform("u_size"), scale: prog.Uniform("u_scale"), time: prog.Uniform("u_time"),
		body: prog.Uniform("u_body"), radius: prog.Uniform("u_radius"), inset: prog.Uniform("u_inset"),
		a: prog.Uniform("u_a"), b: prog.Uniform("u_b"), frameLoc: prog.Uniform("u_frame"),
	}, nil
}

// source is the framebuffer a frame of size is drawn into before the
// effects, made (or remade) to that size.
func (p *postPass) source(size scale.Size) uint32 {
	if p.frame == nil || p.frame.W != int(size.W) || p.frame.H != int(size.H) {
		if p.frame != nil {
			p.frame.Delete()
		}
		p.frame = gles.NewTarget(int(size.W), int(size.H))
	}
	return p.frame.FBO
}

// draw draws the intermediate frame into t with post's effects (call
// source for t's size first). Lengths go to the shader in buffer pixels.
func (p *postPass) draw(t Target, post Post) {
	s := t.Scale.Factor()
	x, y, w, h := post.Rect[0]*s, post.Rect[1]*s, post.Rect[2]*s, post.Rect[3]*s
	half := max(min(w, h), 0) / 2
	gles.BindFramebuffer(t.FBO)
	gles.Viewport(0, 0, int(t.Size.W), int(t.Size.H))
	gles.NoScissor()
	gles.NoBlend()
	p.program.Use()
	gles.Texture(p.frame.Tex).Bind()
	gles.Uniform1i(p.frameLoc, 0)
	gles.Uniform2f(p.size, float32(t.Size.W), float32(t.Size.H))
	gles.Uniform1f(p.scale, s)
	gles.Uniform1f(p.time, post.Time)
	gles.Uniform4f(p.body, x, y, w, h)
	gles.Uniform1f(p.radius, min(max(post.Radius*s, 0), half))
	gles.Uniform1f(p.inset, min(max(post.Inset*s, 0), half))
	gles.Uniform4f(p.a, clamp01(post.Scanlines), clamp01(post.Grain), clamp01(post.Flicker), clamp01(post.Vignette))
	gles.Uniform4f(p.b, clamp01(post.Bloom), clamp01(post.Curvature), clamp01(post.Aberration), 0)
	p.vao.Bind()
	gles.DrawTriangle()
	gles.VertexArray(0).Bind()
}

func (p *postPass) close() {
	if p.frame != nil {
		p.frame.Delete()
		p.frame = nil
	}
	p.vao.Delete()
	p.program.Delete()
}
