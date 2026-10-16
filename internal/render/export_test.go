package render

import (
	_ "embed" // the clean room's glitch.frag

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// LegacyPostOver runs Rust v1's post shader (legacyPostFragment) over
// frame, an image as Offscreen returns it (top-down), at scale s with v1's
// seven strengths taken from post.CRT, and reads the result back as
// Offscreen does. For the compatibility test of the CRT pass.
func (h *Headless) LegacyPostOver(frame *Image, s scale.Scale, post Post) (*Image, error) {
	prog, err := gles.NewProgram(postVertex, legacyPostFragment)
	if err != nil {
		return nil, err
	}
	defer prog.Delete()
	w, ht := frame.Width, frame.Height
	row := w * 4
	// GL's frames are bottom-up: upload the image's last row first.
	bottomUp := make([]byte, len(frame.Pixels))
	for y := range ht {
		copy(bottomUp[y*row:(y+1)*row], frame.Pixels[(ht-1-y)*row:(ht-y)*row])
	}
	tex := gles.NewTexture(w, ht)
	defer tex.Delete()
	tex.Upload(0, 0, w, ht, bottomUp)
	target := gles.NewTarget(w, ht)
	defer target.Delete()
	t := Target{FBO: target.FBO, Size: scale.Size{W: uint32(w), H: uint32(ht)}, Scale: s}
	sc := t.Scale.Factor()
	x, y, bw, bh := post.Rect[0]*sc, post.Rect[1]*sc, post.Rect[2]*sc, post.Rect[3]*sc
	half := max(min(bw, bh), 0) / 2
	gles.BindFramebuffer(t.FBO)
	gles.Viewport(0, 0, w, ht)
	gles.NoScissor()
	gles.NoBlend()
	prog.Use()
	tex.Bind()
	c := post.CRT
	gles.Uniform1i(prog.Uniform("u_frame"), 0)
	gles.Uniform2f(prog.Uniform("u_size"), float32(w), float32(ht))
	gles.Uniform1f(prog.Uniform("u_scale"), sc)
	gles.Uniform1f(prog.Uniform("u_time"), post.Time)
	gles.Uniform4f(prog.Uniform("u_body"), x, y, bw, bh)
	gles.Uniform1f(prog.Uniform("u_radius"), min(max(post.Radius*sc, 0), half))
	gles.Uniform1f(prog.Uniform("u_inset"), min(max(post.Inset*sc, 0), half))
	gles.Uniform4f(prog.Uniform("u_a"), clamp01(c.Scanlines.Strength), clamp01(c.Grain.Strength),
		clamp01(c.Flicker.Strength), clamp01(c.Roundness.Vignette))
	gles.Uniform4f(prog.Uniform("u_b"), clamp01(c.Glow.Strength), clamp01(c.Roundness.Curvature),
		clamp01(c.Aberration.Amount), 0)
	vao := gles.NewVertexArray()
	defer vao.Delete()
	vao.Bind()
	gles.DrawTriangle()
	gles.VertexArray(0).Bind()
	read := gles.ReadPixels(0, 0, w, ht)
	gles.BindFramebuffer(0)
	if err := gles.Err(); err != nil {
		return nil, err
	}
	out := &Image{Width: w, Height: ht, Pixels: make([]byte, len(read))}
	for y := range ht {
		copy(out.Pixels[y*row:(y+1)*row], read[(ht-1-y)*row:(ht-y)*row])
	}
	return out, nil
}

// cleanRoomGlitch is the clean room's glitch.frag as delivered (row 0 of
// its frame is the top), for the glitch pass's compatibility test.
//
//go:embed testdata/glitch-cleanroom.frag
var cleanRoomGlitch string

// CleanRoomGlitchOver runs the delivered glitch shader over frame, an
// image as Offscreen returns it (top-down), uploaded top-down as the
// clean room's stills were, with post's geometry, glitch and moment, and
// reads the result back as Offscreen does.
func (h *Headless) CleanRoomGlitchOver(frame *Image, s scale.Scale, post Post) (*Image, error) {
	pass, err := glitchPassOf(cleanRoomGlitch)
	if err != nil {
		return nil, err
	}
	defer pass.close()
	w, ht := frame.Width, frame.Height
	tex := gles.NewTexture(w, ht)
	defer tex.Delete()
	tex.Upload(0, 0, w, ht, frame.Pixels)
	target := gles.NewTarget(w, ht)
	defer target.Delete()
	pass.draw(gles.Texture(tex), Target{FBO: target.FBO, Size: scale.Size{W: uint32(w), H: uint32(ht)}, Scale: s}, post)
	gles.BindFramebuffer(target.FBO)
	read := gles.ReadPixels(0, 0, w, ht)
	gles.BindFramebuffer(0)
	if err := gles.Err(); err != nil {
		return nil, err
	}
	row := w * 4
	out := &Image{Width: w, Height: ht, Pixels: make([]byte, len(read))}
	for y := range ht {
		copy(out.Pixels[y*row:(y+1)*row], read[(ht-1-y)*row:(ht-y)*row])
	}
	return out, nil
}
