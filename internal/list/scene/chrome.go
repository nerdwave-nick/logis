package scene

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The panel itself (Rust src/list/scene/chrome.rs): its body with the glow
// (pulsing) and the border (its gradient and travelling highlight), the
// corner rivets, the rule under the prompt line, and the gear sigil. The
// post-processing effects over the body come with the theme engine
// (feat/logis-go G18).

// panel draws the body, its glow and border, then the rivets.
func panel(sc *render.Scene, body [4]float32, s Style) {
	t := s.Theme
	x, y, w, h := body[0], body[1], body[2], body[3]
	vertical := func(stops []render.Color) render.Paint {
		return render.Ramp([2]float32{0, y}, [2]float32{0, y + h}, stops...)
	}
	g := t.Glow
	// The pulse dims the glow and brings it back, never above its colour.
	strength := 1 - g.Pulse*(1-s.wave(int64(g.Period)))
	border := render.Border{Width: t.Border.Width, Paint: vertical(t.Border.Paint)}
	if hl := t.Border.Highlight; t.Border.HasHighlight {
		// From the top, clockwise; at rest (no period) it stays there.
		border.Highlight = render.Highlight{
			Angle: -math.Pi/2 + 2*math.Pi*s.phase(int64(hl.Period)),
			Reach: hl.Length, Color: hl.Color,
		}
	}
	sc.Shapes = append(sc.Shapes, render.RoundedRect{
		Rect: body, Radius: t.Panel.Radius, Fill: vertical(t.Panel.Fill), Border: border,
		Glow: render.Glow{Radius: g.Radius, Color: g.Color.WithAlpha(g.Color.A * strength)},
	})
	if !t.HasRivets {
		return
	}
	r := t.Rivets
	d := r.Size
	left, right := x+r.Inset, x+w-r.Inset-d
	top, bottom := y+r.Inset, y+h-r.Inset-d
	for _, c := range []struct {
		x, y  float32
		stops []render.Color
	}{{left, top, r.Top}, {right, top, r.Top}, {left, bottom, r.Bottom}, {right, bottom, r.Bottom}} {
		sc.Shapes = append(sc.Shapes, disc([4]float32{c.x, c.y, d, d}, c.stops))
	}
}

// disc is a round boss lit from its top left.
func disc(rect [4]float32, stops []render.Color) render.RoundedRect {
	x, y, w, h := rect[0], rect[1], rect[2], rect[3]
	return render.RoundedRect{
		Rect: rect, Radius: min(w, h) / 2,
		Fill: render.Ramp([2]float32{x + w*0.3, y + h*0.3}, [2]float32{x + w, y + h}, stops...),
	}
}

// rule draws the rule under the prompt line: its gradient across, dashed
// or not, with its ornament at the middle.
func rule(sc *render.Scene, body [4]float32, t *theme.Theme) {
	r := t.Rule
	left, right := body[0]+r.Inset, body[0]+body[2]-r.Inset
	top := body[1] + t.Prompt.Height
	if right <= left || r.Thickness <= 0 {
		return
	}
	paint := render.Ramp([2]float32{left, 0}, [2]float32{right, 0}, r.Paint...)
	bar := func(from, to float32) render.RoundedRect {
		return render.RoundedRect{Rect: [4]float32{from, top, to - from, r.Thickness}, Fill: paint}
	}
	if !r.Dashed {
		sc.Shapes = append(sc.Shapes, bar(left, right))
	} else {
		for at := left; at < right; at += r.Dash[0] + r.Dash[1] {
			sc.Shapes = append(sc.Shapes, bar(at, min(at+r.Dash[0], right)))
		}
	}
	if !r.HasOrnament {
		return
	}
	o := r.Ornament
	centre, mid, half := (left+right)/2, top+r.Thickness/2, o.Size/2
	sc.Shapes = append(sc.Shapes, render.RoundedRect{
		Rect: [4]float32{centre - half, mid - half, o.Size, o.Size}, Radius: half,
		Fill: render.Solid(o.Fill), Border: render.Border{Width: 1, Paint: render.Solid(o.Edge)},
	})
}

// gear draws the gear sigil: a ring, a hub and eight teeth around it, size
// square with its centre at (cx, cy). A generic motif.
func gear(sc *render.Scene, cx, cy, size float32, c render.Color) {
	unit := size / 24
	ring, stroke := 5*unit, 2*unit
	sc.Shapes = append(sc.Shapes, render.RoundedRect{
		Rect:   [4]float32{cx - ring - stroke/2, cy - ring - stroke/2, 2*ring + stroke, 2*ring + stroke},
		Radius: ring + stroke/2, Fill: render.Solid(render.Transparent),
		Border: render.Border{Width: stroke, Paint: render.Solid(c)},
	})
	sc.Shapes = append(sc.Shapes, dot(cx, cy, 1.6*unit, c))
	for i := range 8 {
		angle := float64(i) * 2 * math.Pi / 8
		reach := 8.6 * unit
		sc.Shapes = append(sc.Shapes, dot(cx+reach*float32(math.Cos(angle)), cy+reach*float32(math.Sin(angle)), 1.4*unit, c))
	}
}

func dot(cx, cy, radius float32, c render.Color) render.RoundedRect {
	return render.RoundedRect{Rect: [4]float32{cx - radius, cy - radius, 2 * radius, 2 * radius}, Radius: radius, Fill: render.Solid(c)}
}
