package render

import (
	_ "embed" // glitch.frag
	"fmt"
	"strconv"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/gles"
)

// glitchFragment is the glitch pass's fragment shader: the clean-room
// glitch.frag (MIT, the logis project; see its header), integrated with
// logis's bottom-up frames (load flips; nothing else changed).
//
//go:embed glitch.frag
var glitchFragment string

// glitchPass is the glitch pass (feat/logis-go 33cfcf44): one full-screen
// triangle over the finished frame, before the CRT pass, with the
// controls of effects.Glitch and the moment's time and burst; the same
// screens as the CRT pass (the body's glass and the regions).
type glitchPass struct {
	program                                                        *gles.Program
	vao                                                            gles.VertexArray
	size, scale, time, body, radius, inset, frameLoc, count, event int32
	region, regionRadius                                           [MaxRegions]int32
	// controls are the uniforms of effects.GlitchControls, in its order.
	controls []int32
}

func newGlitchPass() (*glitchPass, error) { return glitchPassOf(glitchFragment) }

// glitchPassOf is the pass with fragment as its shader.
func glitchPassOf(fragment string) (*glitchPass, error) {
	prog, err := gles.NewProgram(postVertex, fragment)
	if err != nil {
		return nil, fmt.Errorf("glitch: %w", err)
	}
	p := &glitchPass{
		program: prog, vao: gles.NewVertexArray(),
		size: prog.Uniform("u_size"), scale: prog.Uniform("u_scale"), time: prog.Uniform("u_time"),
		body: prog.Uniform("u_body"), radius: prog.Uniform("u_radius"), inset: prog.Uniform("u_inset"),
		frameLoc: prog.Uniform("u_frame"), count: prog.Uniform("u_region_count"), event: prog.Uniform("u_event"),
	}
	for i := range MaxRegions {
		at := "[" + strconv.Itoa(i) + "]"
		p.region[i], p.regionRadius[i] = prog.Uniform("u_region"+at), prog.Uniform("u_region_radius"+at)
	}
	for _, k := range effects.GlitchControls {
		p.controls = append(p.controls, prog.Uniform(k.Uniform))
	}
	return p, nil
}

// wanted: a strength of 0 is no pass at all.
func (p *glitchPass) wanted(post Post) bool { return post.Glitch.On() }

// draw draws frame into t with post's glitch at its moment. The geometry
// goes in buffer pixels as the CRT pass's does, the controls (held to
// their ranges) in logical pixels.
func (p *glitchPass) draw(frame gles.Texture, t Target, post Post) {
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
	gles.Uniform1f(p.time, post.Moment.Time)
	gles.Uniform1f(p.event, min(max(post.Moment.Event, 0), 1))
	gles.Uniform4f(p.body, x, y, w, h)
	gles.Uniform1f(p.radius, min(max(post.Radius*s, 0), half))
	gles.Uniform1f(p.inset, min(max(post.Inset*s, 0), half))
	regions := post.Regions[:min(len(post.Regions), MaxRegions)]
	gles.Uniform1i(p.count, len(regions))
	for i, r := range regions {
		gles.Uniform4f(p.region[i], r.Rect[0]*s, r.Rect[1]*s, max(r.Rect[2], 0)*s, max(r.Rect[3], 0)*s)
		gles.Uniform1f(p.regionRadius[i], max(r.Radius, 0)*s)
	}
	for i, k := range effects.GlitchControls {
		gles.Uniform1f(p.controls[i], min(max(k.Value(post.Glitch), k.Min), k.Max))
	}
	p.vao.Bind()
	gles.DrawTriangle()
	gles.VertexArray(0).Bind()
}

func (p *glitchPass) close() {
	p.vao.Delete()
	p.program.Delete()
}
