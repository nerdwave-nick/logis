package motion

import (
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ported from Rust src/motion/apply.rs; the caret fading with its line is
// Go's own (Rust leaves it opaque while the panel fades).

func applyScene() *render.Scene {
	shape := render.RoundedRect{
		Rect:   [4]float32{10, 20, 100, 40},
		Radius: 8,
		Fill: render.Linear([2]float32{0, 20}, [2]float32{0, 60},
			render.RGBA(1, 0, 0, 0.8), render.RGBA(0, 0, 1, 0.4)),
		Border: render.Border{Width: 2, Paint: render.Solid(render.Hex(0xd4a017)),
			Highlight: render.Highlight{Angle: 1, Reach: 20, Color: render.RGBA(1, 1, 1, 0.6)}},
		Glow: render.Glow{Radius: 12, Color: render.RGBA(0.7, 0, 0, 0.5)},
	}
	text := render.NewText("rite", [2]float32{14, 24}, 15, render.Hex(0xeadfc4))
	text.Spans = []render.Span{{Start: 0, End: 2, Color: render.Hex(0xf2c14e)}}
	text.Caret = &render.Caret{At: 4, Color: render.Hex(0xf2c14e), Glow: render.RGBA(1, 0.5, 0, 0.8)}
	return &render.Scene{
		Clear:  render.RGBA(0, 0, 0, 0.5),
		Shapes: []render.RoundedRect{shape},
		Images: []render.ImageQuad{{Image: 0, Origin: [2]float32{30, 24}, Opacity: 0.8}},
		Texts:  []render.Text{text},
	}
}

func TestTheEffectsMoveWithThePanel(t *testing.T) {
	s := applyScene()
	s.Post = render.Post{Rect: [4]float32{10, 20, 100, 40}, Scanlines: 0.5}
	Pose{Opacity: 0.5, Offset: [2]float32{0, 8}}.Apply(s)
	if s.Post.Rect != [4]float32{10, 28, 100, 40} {
		t.Errorf("%v", s.Post.Rect)
	}
	if s.Post.Scanlines != 0.5 {
		t.Error("the strengths changed")
	}
}

func TestTheRestPoseChangesNothing(t *testing.T) {
	s := applyScene()
	Rest.Apply(s)
	if !reflect.DeepEqual(s, applyScene()) {
		t.Errorf("%+v", s)
	}
}

func TestAPoseFadesEveryColourAndKeepsTheHue(t *testing.T) {
	s := applyScene()
	Pose{Opacity: 0.5}.Apply(s)
	if s.Clear != render.RGBA(0, 0, 0, 0.25) {
		t.Errorf("clear %v", s.Clear)
	}
	shape := s.Shapes[0]
	fill := shape.Fill.Stops()
	if len(fill) != 2 || fill[0].A != 0.4 || fill[1].A != 0.2 || fill[0].R != 1 {
		t.Errorf("fill %v", fill)
	}
	if border := shape.Border.Paint.Stops(); len(border) != 1 || border[0] != render.Hex(0xd4a017).WithAlpha(0.5) {
		t.Errorf("border %v", border)
	}
	if shape.Border.Highlight.Color.A != 0.3 || shape.Glow.Color.A != 0.25 {
		t.Errorf("highlight %v, glow %v", shape.Border.Highlight.Color, shape.Glow.Color)
	}
	text := s.Texts[0]
	if text.Color.A != 0.5 || text.Spans[0].Color.A != 0.5 {
		t.Errorf("text %v, span %v", text.Color, text.Spans[0].Color)
	}
	if text.Caret.Color.A != 0.5 || text.Caret.Glow.A != 0.4 {
		t.Errorf("caret %+v", text.Caret)
	}
	if applyScene().Texts[0].Caret.Color.A != 1 {
		t.Error("the pose changed the scene it was not applied to")
	}
	if s.Images[0].Opacity != 0.4 {
		t.Errorf("images do not fade: %v", s.Images[0].Opacity)
	}
}

func TestAPoseMovesShapesGradientsAndTextTogether(t *testing.T) {
	s := applyScene()
	Pose{Opacity: 1, Offset: [2]float32{2, 10}}.Apply(s)
	if s.Shapes[0].Rect != [4]float32{12, 30, 100, 40} {
		t.Errorf("%v", s.Shapes[0].Rect)
	}
	if f := s.Shapes[0].Fill; f.Start != [2]float32{2, 30} || f.End != [2]float32{2, 70} {
		t.Errorf("gradient %v → %v", f.Start, f.End)
	}
	if s.Texts[0].Origin != [2]float32{16, 34} || s.Texts[0].Color != applyScene().Texts[0].Color {
		t.Errorf("text %+v", s.Texts[0])
	}
	if s.Images[0].Origin != [2]float32{32, 34} || s.Images[0].Opacity != 0.8 {
		t.Errorf("image %+v", s.Images[0])
	}
}

func TestOpacityIsClamped(t *testing.T) {
	s := applyScene()
	Pose{Opacity: 3}.Apply(s)
	if s.Texts[0].Color.A != 1 {
		t.Errorf("%v", s.Texts[0].Color)
	}
}

func TestPlaceMovesARectangleByTheOffset(t *testing.T) {
	if got := (Pose{Opacity: 1, Offset: [2]float32{1, -2}}).Place([4]float32{3, 4, 5, 6}); got != [4]float32{4, 2, 5, 6} {
		t.Errorf("%v", got)
	}
}
