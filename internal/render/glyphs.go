package render

import (
	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// glyphFloats is floats per glyph instance: rect, atlas texel and kind,
// premultiplied colour.
const glyphFloats = 12

// glyphSlot is a glyph image's place in the glyph atlas and its offset
// from the glyph origin (left, and top above the baseline).
type glyphSlot struct {
	x, y, w, h uint32
	left, top  int
	color      bool
}

// glyphPass draws texts (Rust text_pass.rs over glyphon): lines shaped by
// the fonts, glyph images rasterised once into their own atlas texture
// (coverage in alpha for outlines, straight RGBA for colour emoji) and
// drawn texel for texel as instanced quads, blended as glyphon does on a
// non-sRGB target: sRGB-encoded colours, alpha blending.
type glyphPass struct {
	fonts   *text.Fonts
	shaped  []shapedLine
	cache   map[text.GlyphKey]glyphSlot
	tex     gles.Texture
	packer  *shelfPacker
	maxSide int
	program *gles.Program
	size    int32
	sample  int32
	vao     gles.VertexArray
	buf     gles.Buffer
	packed  []float32
}

func newGlyphPass() (*glyphPass, error) {
	program, err := gles.NewProgram(glyphVertex, glyphFragment)
	if err != nil {
		return nil, err
	}
	p := &glyphPass{
		program: program,
		size:    program.Uniform("u_size"),
		sample:  program.Uniform("u_atlas"),
		vao:     gles.NewVertexArray(),
		buf:     gles.NewBuffer(),
		maxSide: min(maxSide, gles.MaxTextureSize()),
	}
	p.vao.Bind()
	p.buf.Bind()
	for i := range glyphFloats / 4 {
		gles.InstanceAttrib(i, 4, glyphFloats, i*4)
	}
	gles.VertexArray(0).Bind()
	p.reset(min(initialSide, p.maxSide))
	return p, nil
}

// reset empties the atlas at side × side texels.
func (p *glyphPass) reset(side int) {
	if p.tex != 0 {
		p.tex.Delete()
	}
	p.tex = gles.NewTexture(side, side)
	p.packer = newShelfPacker(uint32(side), uint32(side))
	p.cache = map[text.GlyphKey]glyphSlot{}
}

func (p *glyphPass) setFonts(f *text.Fonts) {
	if p.fonts != nil {
		p.fonts.Close()
	}
	p.fonts, p.shaped = f, p.shaped[:0]
	side, _ := p.packer.size()
	p.reset(int(side)) // FontIDs name other fonts now
}

// prepare shapes the frame's texts (before the shapes are drawn: their
// underlines are drawn with them).
func (p *glyphPass) prepare(texts []Text, s scale.Scale) {
	if p.fonts == nil {
		p.shaped = p.shaped[:0]
		return
	}
	p.shape(texts, s)
}

// draw rasterises missing glyphs and draws the prepared lines into t.
// The blend state and target are already set.
func (p *glyphPass) draw(t Target) {
	if p.fonts == nil || len(p.shaped) == 0 {
		return
	}
	if !p.pack(t.Size) {
		// The atlas is full at its largest: start it afresh, once.
		side, _ := p.packer.size()
		p.reset(int(side))
		p.pack(t.Size)
	}
	if len(p.packed) == 0 {
		return
	}
	p.program.Use()
	gles.Uniform2f(p.size, float32(t.Size.W), float32(t.Size.H))
	gles.Uniform1i(p.sample, 0)
	p.tex.Bind()
	p.vao.Bind()
	p.buf.Stream(p.packed)
	gles.DrawQuads(len(p.packed) / glyphFloats)
}

// bounds is the area a line's glyphs are clipped to, in buffer pixels:
// its column (or the target's width) and the target's height, as glyphon
// clips them.
type bounds struct{ x0, x1, y0, y1 int }

func lineBounds(l *shapedLine, size scale.Size) bounds {
	left, right, ok := l.text.column(l.scale)
	if !ok {
		left, right = 0, int(size.W)
	}
	b := bounds{x0: max(left, 0), y1: int(size.H)}
	b.x1 = max(min(right, int(size.W)), b.x0)
	return b
}

// pack makes this frame's glyph instances; false if the atlas is full.
func (p *glyphPass) pack(size scale.Size) bool {
	p.packed = p.packed[:0]
	for i := range p.shaped {
		l := &p.shaped[i]
		at := place(l)
		clip := lineBounds(l, size)
		for _, g := range l.line.Glyphs {
			x, bin := subpixelBin(at.left + (g.X + g.XOffset))
			slot, ok := p.glyph(text.GlyphKey{Font: g.Font, ID: g.ID, Size: at.size, XBin: bin})
			if !ok {
				return false
			}
			if slot.w == 0 {
				continue
			}
			q := quad{x: x + slot.left, y: at.baseline + int(at.top+g.Y) - slot.top,
				w: int(slot.w), h: int(slot.h), ax: int(slot.x), ay: int(slot.y)}
			if !q.clip(clip) {
				continue
			}
			kind := float32(0)
			if slot.color {
				kind = 1
			}
			cluster := g.Cluster
			if g.Ellipsis {
				cluster = 0 // the ellipsis takes the first piece's colour
			}
			ink := l.text.colorAt(l.str, cluster).Premultiplied()
			p.packed = append(p.packed,
				float32(q.x), float32(q.y), float32(q.w), float32(q.h),
				float32(q.ax), float32(q.ay), kind, 0,
				ink[0], ink[1], ink[2], ink[3])
		}
	}
	return true
}

// quad is a glyph image's place on the target and in the atlas.
type quad struct{ x, y, w, h, ax, ay int }

// clip cuts q to b like glyphon: false if it lies wholly outside.
func (q *quad) clip(b bounds) bool {
	if q.x > b.x1 || q.x+q.w < b.x0 || q.y > b.y1 || q.y+q.h < b.y0 {
		return false
	}
	if q.x < b.x0 {
		d := b.x0 - q.x
		q.x, q.w, q.ax = b.x0, q.w-d, q.ax+d
	}
	q.w = min(q.w, b.x1-q.x)
	if q.y < b.y0 {
		d := b.y0 - q.y
		q.y, q.h, q.ay = b.y0, q.h-d, q.ay+d
	}
	q.h = min(q.h, b.y1-q.y)
	return true
}

// glyph is k's slot, rasterised and uploaded on first use; a glyph that
// cannot be rendered is an empty slot. !ok when the atlas is full.
func (p *glyphPass) glyph(k text.GlyphKey) (glyphSlot, bool) {
	if s, ok := p.cache[k]; ok {
		return s, true
	}
	img, err := p.fonts.Glyph(k)
	if err != nil || img.Width == 0 || img.Height == 0 || max(img.Width, img.Height) >= p.maxSide {
		p.cache[k] = glyphSlot{}
		return glyphSlot{}, true
	}
	x, y, ok := p.packer.allocate(uint32(img.Width), uint32(img.Height))
	for !ok {
		side, _ := p.packer.size()
		if int(side)*2 > p.maxSide {
			return glyphSlot{}, false
		}
		p.grow()
		x, y, ok = p.packer.allocate(uint32(img.Width), uint32(img.Height))
	}
	p.tex.Upload(int(x), int(y), img.Width, img.Height, rgba(img))
	s := glyphSlot{x: x, y: y, w: uint32(img.Width), h: uint32(img.Height), left: img.Left, top: img.Top, color: img.Color}
	p.cache[k] = s
	return s, true
}

// grow doubles the atlas on the GPU; placed glyphs keep their texels.
func (p *glyphPass) grow() {
	old, _ := p.packer.size()
	p.packer.grow()
	side, _ := p.packer.size()
	tex := gles.NewTexture(int(side), int(side))
	gles.CopyTexture(p.tex, tex, int(old), int(old))
	p.tex.Delete()
	p.tex = tex
}

// rgba is a glyph image as atlas texels: colour as it is, coverage in
// alpha.
func rgba(img *text.GlyphImage) []byte {
	if img.Color {
		return img.Pixels
	}
	out := make([]byte, len(img.Pixels)*4)
	for i, c := range img.Pixels {
		out[4*i], out[4*i+1], out[4*i+2], out[4*i+3] = 255, 255, 255, c
	}
	return out
}

func (p *glyphPass) close() {
	if p.fonts != nil {
		p.fonts.Close()
		p.fonts = nil
	}
	p.tex.Delete()
	p.program.Delete()
	p.vao.Delete()
	p.buf.Delete()
}

// glyphVertex: one instance per glyph, its rect in whole buffer pixels.
const glyphVertex = `#version 300 es
precision highp float;
uniform vec2 u_size;
layout(location = 0) in vec4 a_rect;  // x, y, w, h in whole buffer pixels
layout(location = 1) in vec4 a_texel; // top-left atlas texel, kind (0 coverage, 1 colour), -
layout(location = 2) in vec4 a_color; // premultiplied ink
flat out vec2 v_offset; // atlas texel minus buffer pixel
flat out float v_kind;
flat out vec4 v_color;

void main() {
	vec2 corner = vec2(float(gl_VertexID & 1), float((gl_VertexID >> 1) & 1));
	vec2 p = a_rect.xy + corner * a_rect.zw;
	gl_Position = vec4(p.x / u_size.x * 2.0 - 1.0, 1.0 - p.y / u_size.y * 2.0, 0.0, 1.0);
	v_offset = a_texel.xy - a_rect.xy;
	v_kind = a_texel.z;
	v_color = a_color;
}
`

// glyphFragment loads exactly one texel per pixel: coverage scales the
// ink; a colour texel (straight alpha) is premultiplied.
const glyphFragment = `#version 300 es
precision highp float;
uniform vec2 u_size;
uniform sampler2D u_atlas;
flat in vec2 v_offset;
flat in float v_kind;
flat in vec4 v_color;
out vec4 o_color;

void main() {
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	vec4 t = texelFetch(u_atlas, ivec2(floor(p + v_offset)), 0);
	o_color = v_kind > 0.5 ? vec4(t.rgb * t.a, t.a) : v_color * t.a;
}
`
