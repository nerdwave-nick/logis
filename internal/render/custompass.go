package render

import (
	"errors"
	"runtime"

	"github.com/nerdwave-nick/logis/internal/custom"
	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/gles"
)

// customPass is the custom pass (feat/logis-go a5e5f876): the theme's or
// the config's shader (internal/custom) over the whole frame after the
// built-in effects, read through a smooth sampler. It compiles a shader
// the first time a frame asks for it and keeps it while frames ask for
// the same one; a shader that cannot be read, compiled or linked is said
// once in the log and skipped until another one is asked for.
type customPass struct {
	vao     gles.VertexArray
	sampler gles.Sampler
	logf    func(string)
	// tried is the shader compiled last (whether it compiled); program is
	// nil when it did not.
	tried                                       custom.Shader
	program                                     *gles.Program
	channel, resolution, time, frameNo, glitchI int32
}

func newCustomPass() *customPass {
	return &customPass{vao: gles.NewVertexArray(), sampler: gles.NewSmoothSampler()}
}

// wanted: a shader is asked for and compiles (compiled here on first
// sight).
func (p *customPass) wanted(post Post) bool {
	s := post.Custom
	if !s.Set() {
		return false
	}
	if s.Path != p.tried.Path || s.Source != p.tried.Source || s.Problem != p.tried.Problem {
		p.compile(s)
	}
	return p.program != nil
}

func (p *customPass) compile(s custom.Shader) {
	p.forget()
	p.tried = s
	prog, err := compileCustom(s)
	if err != nil {
		if p.logf != nil {
			p.logf(custom.Heresy(err) + "; it is skipped, the built-in effects still draw")
		}
		return
	}
	p.program = prog
	p.channel, p.resolution = prog.Uniform("iChannel0"), prog.Uniform("iResolution")
	p.time, p.frameNo, p.glitchI = prog.Uniform("iTime"), prog.Uniform("iFrame"), prog.Uniform("iGlitch")
}

// compileCustom is s's program on the current context, or why there is
// none (a *custom.Error in the user's lines).
func compileCustom(s custom.Shader) (*gles.Program, error) {
	if err := s.Readable(); err != nil {
		return nil, err
	}
	fragment, err := custom.Fragment(s.Path, s.Source)
	if err != nil {
		return nil, err
	}
	prog, err := gles.NewProgram(postVertex, fragment)
	if refused := (*gles.ShaderError)(nil); errors.As(err, &refused) {
		return nil, custom.Explain(s.Path, s.Source, refused.Log)
	}
	return prog, err
}

// CheckCustom compiles s on an offscreen context of its own (--check-config):
// nil when it compiles and links, else why not in the user's lines.
func CheckCustom(s custom.Shader) error {
	if err := s.Readable(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := egl.NewSurfaceless()
	if err != nil {
		return errors.New("it cannot be tried here: " + err.Error())
	}
	defer ctx.Close()
	prog, err := compileCustom(s)
	if err != nil {
		return err
	}
	prog.Delete()
	return nil
}

// draw draws frame into t through the shader: iResolution in buffer
// pixels, iTime the seconds since the panel opened (0 when frozen),
// iFrame and iGlitch.
func (p *customPass) draw(frame gles.Texture, t Target, post Post) {
	gles.BindFramebuffer(t.FBO)
	gles.Viewport(0, 0, int(t.Size.W), int(t.Size.H))
	gles.NoScissor()
	gles.NoBlend()
	p.program.Use()
	frame.Bind()
	p.sampler.Bind()
	time := post.Moment.Time
	if post.Custom.Frozen {
		time = 0
	}
	gles.Uniform1i(p.channel, 0)
	gles.Uniform3f(p.resolution, float32(t.Size.W), float32(t.Size.H), 1)
	gles.Uniform1f(p.time, time)
	gles.Uniform1i(p.frameNo, post.Frame)
	gles.Uniform1f(p.glitchI, post.Glitch.Intensity(post.Moment))
	p.vao.Bind()
	gles.DrawTriangle()
	gles.VertexArray(0).Bind()
	gles.Sampler(0).Bind()
}

func (p *customPass) forget() {
	if p.program != nil {
		p.program.Delete()
		p.program = nil
	}
}

func (p *customPass) close() {
	p.forget()
	p.sampler.Delete()
	p.vao.Delete()
}
