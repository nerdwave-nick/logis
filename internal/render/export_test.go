package render

import (
	_ "embed" // the clean room's glitch.frag
	"fmt"
	"strings"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// LegacyPostOver runs the legacy post shader (legacyPostFragment) over
// frame, an image as Offscreen returns it (top-down), at scale s with its
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

// wholePixelGrids are logis's edits to the delivered glitch shader besides
// the flip, old text to new: its hashed grids (the noise band's cells, the
// slices, the blocks' cells) counted in whole device pixels from the
// glass's rounded top left (ad8913d4). Nothing else differs.
var wholePixelGrids = [][2]string{
	{"float g_lo, g_hi;\n", "float g_lo, g_hi;\nvec2 g_grid;\n"},
	{"uint cell(float x) { return uint(int(floor(x)) + 0x100000); }\n",
		"uint cell(float x) { return uint(int(floor(x)) + 0x100000); }\n" +
			"float whole_px(float px) { return max(floor(px + 0.5), 1.0); }\n"},
	{"float slice = floor(y / max(u_slice_px * u_scale, 1.0));", "float slice = floor(y / whole_px(u_slice_px * u_scale));"},
	{"uint gx = cell(q.x / (wx * 1.7)), gy = cell(q.y / (hy * 3.3));",
		"uint gx = cell(g_grid.x / whole_px(wx * 1.7)), gy = cell(g_grid.y / whole_px(hy * 3.3));"},
	{"float r = rnd(cell(q.y / (hy * 1.3)), gx ^ s, 56u);", "float r = rnd(cell(g_grid.y / whole_px(hy * 1.3)), gx ^ s, 56u);"},
	{"vec2 c = q / max(u_band_cell_px * u_scale, 1.0);", "vec2 c = g_grid / whole_px(u_band_cell_px * u_scale);"},
	{"vec2 q = p - (g_centre - g_half);\n", "vec2 q = p - (g_centre - g_half);\n\tg_grid = p - floor(g_centre - g_half + 0.5);\n"},
	{"slice_tear(q.y, ", "slice_tear(g_grid.y, "},
	{"float slice = floor(q.y / max(u_slice_px * u_scale, 1.0));", "float slice = floor(g_grid.y / whole_px(u_slice_px * u_scale));"},
}

// cleanRoomOnWholePixels is the delivered shader with wholePixelGrids, each
// edit made exactly once.
func cleanRoomOnWholePixels() (string, error) {
	src := cleanRoomGlitch
	for _, e := range wholePixelGrids {
		if n := strings.Count(src, e[0]); n != 1 {
			return "", fmt.Errorf("the delivered glitch shader has %q %d times, not once", e[0], n)
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	return src, nil
}

// CleanRoomGlitchOver runs the delivered glitch shader, its grids on whole
// device pixels (wholePixelGrids), over frame, an image as Offscreen
// returns it (top-down), uploaded top-down as the clean room's stills
// were, with post's geometry, glitch and moment, and reads the result back
// as Offscreen does.
func (h *Headless) CleanRoomGlitchOver(frame *Image, s scale.Scale, post Post) (*Image, error) {
	src, err := cleanRoomOnWholePixels()
	if err != nil {
		return nil, err
	}
	pass, err := glitchPassOf(src)
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
