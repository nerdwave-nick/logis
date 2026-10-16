package motion

import "github.com/nerdwave-nick/logis/internal/render"

// Apply moves and fades sc into this pose (Rust src/motion/apply.rs): every
// position moved by the offset and every colour's alpha multiplied by the
// opacity, images, the effects' areas (body and plates) and the caret included, so any scene
// opens and closes the same way. The rest pose changes nothing.
func (p Pose) Apply(sc *render.Scene) {
	if p.IsRest() {
		return
	}
	opacity := min(max(p.Opacity, 0), 1)
	fade := func(c render.Color) render.Color { return c.WithAlpha(c.A * opacity) }
	shift := func(at [2]float32) [2]float32 { return [2]float32{at[0] + p.Offset[0], at[1] + p.Offset[1]} }
	sc.Clear = fade(sc.Clear)
	for i := range sc.Shapes {
		s := &sc.Shapes[i]
		s.Rect = p.Place(s.Rect)
		s.Fill = s.Fill.Map(fade, shift)
		s.Border.Paint = s.Border.Paint.Map(fade, shift)
		s.Border.Highlight.Color = fade(s.Border.Highlight.Color)
		s.Glow.Color = fade(s.Glow.Color)
	}
	sc.Post.Rect = p.Place(sc.Post.Rect)
	if len(sc.Post.Regions) > 0 {
		regions := make([]render.Region, len(sc.Post.Regions))
		for i, r := range sc.Post.Regions {
			regions[i] = render.Region{Rect: p.Place(r.Rect), Radius: r.Radius}
		}
		sc.Post.Regions = regions
	}
	for i := range sc.Images {
		sc.Images[i].Origin = shift(sc.Images[i].Origin)
		sc.Images[i].Opacity *= opacity
	}
	for i := range sc.Texts {
		t := &sc.Texts[i]
		t.Origin = shift(t.Origin)
		t.Color = fade(t.Color)
		spans := make([]render.Span, len(t.Spans))
		for j, s := range t.Spans {
			s.Color = fade(s.Color)
			spans[j] = s
		}
		t.Spans = spans
		if t.Caret != nil {
			caret := *t.Caret
			caret.Color, caret.Glow = fade(caret.Color), fade(caret.Glow)
			t.Caret = &caret
		}
	}
}
