package render

import (
	_ "embed" // crt.frag
	"fmt"
	"strconv"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/gles"
)

// crtFragment is the CRT pass's fragment shader: the clean-room crt.frag
// (MIT, the logis project; see its header), integrated with logis's
// bottom-up frames. It replaced the port of Rust's post.wgsl.
//
//go:embed crt.frag
var crtFragment string

// crtPass is the CRT pass: one full-screen triangle over the finished
// frame with the effects of effects.CRT, one named uniform per control.
type crtPass struct {
	program                                                 *gles.Program
	vao                                                     gles.VertexArray
	size, scale, time, body, radius, inset, frameLoc, count int32
	region, regionRadius                                    [MaxRegions]int32
	// controls are the uniforms of effects.Controls, in its order.
	controls []int32
}

func newCRTPass() (*crtPass, error) {
	prog, err := gles.NewProgram(postVertex, crtFragment)
	if err != nil {
		return nil, fmt.Errorf("crt: %w", err)
	}
	p := &crtPass{
		program: prog, vao: gles.NewVertexArray(),
		size: prog.Uniform("u_size"), scale: prog.Uniform("u_scale"), time: prog.Uniform("u_time"),
		body: prog.Uniform("u_body"), radius: prog.Uniform("u_radius"), inset: prog.Uniform("u_inset"),
		frameLoc: prog.Uniform("u_frame"), count: prog.Uniform("u_region_count"),
	}
	for i := range MaxRegions {
		at := "[" + strconv.Itoa(i) + "]"
		p.region[i], p.regionRadius[i] = prog.Uniform("u_region"+at), prog.Uniform("u_region_radius"+at)
	}
	for _, k := range effects.Controls {
		p.controls = append(p.controls, prog.Uniform(k.Uniform))
	}
	return p, nil
}

func (p *crtPass) wanted(post Post) bool { return post.CRT.Any() }

// draw draws frame into t with post's effects. Lengths go to the shader
// in buffer pixels for the geometry, logical pixels for the controls
// (the shader scales those); every control is held to its range.
func (p *crtPass) draw(frame gles.Texture, t Target, post Post) {
	s := t.Scale.Factor()
	x, y, w, h := post.Rect[0]*s, post.Rect[1]*s, post.Rect[2]*s, post.Rect[3]*s
	half := max(min(w, h), 0) / 2
	gles.BindFramebuffer(t.FBO)
	gles.Viewport(0, 0, int(t.Size.W), int(t.Size.H))
	gles.NoScissor()
	gles.NoBlend()
	p.program.Use()
	frame.Bind()
	gles.Uniform1i(p.frameLoc, 0)
	gles.Uniform2f(p.size, float32(t.Size.W), float32(t.Size.H))
	gles.Uniform1f(p.scale, s)
	gles.Uniform1f(p.time, post.Time)
	gles.Uniform4f(p.body, x, y, w, h)
	gles.Uniform1f(p.radius, min(max(post.Radius*s, 0), half))
	gles.Uniform1f(p.inset, min(max(post.Inset*s, 0), half))
	regions := post.Regions[:min(len(post.Regions), MaxRegions)]
	gles.Uniform1i(p.count, len(regions))
	for i, r := range regions {
		gles.Uniform4f(p.region[i], r.Rect[0]*s, r.Rect[1]*s, max(r.Rect[2], 0)*s, max(r.Rect[3], 0)*s)
		gles.Uniform1f(p.regionRadius[i], max(r.Radius, 0)*s)
	}
	for i, k := range effects.Controls {
		v := min(max(k.Value(post.CRT), k.Min), k.Max)
		if k.Words != nil {
			gles.Uniform1i(p.controls[i], int(v))
		} else {
			gles.Uniform1f(p.controls[i], v)
		}
	}
	p.vao.Bind()
	gles.DrawTriangle()
	gles.VertexArray(0).Bind()
}

func (p *crtPass) close() {
	p.vao.Delete()
	p.program.Delete()
}
