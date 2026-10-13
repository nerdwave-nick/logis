package cogitator

import (
	"slices"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The theme the window wears (Rust src/daemon/window/wear.rs put_on and
// ui.rs Command::Theme): worn from now on, the visible panel included,
// which is re-fitted to the theme's size (in the terms its session asked
// for) and drawn again. Fontconfig is asked again only when the faces
// change. With the test trace on it reports `theme <name>`. Its motion
// timings move the panels shown from then on (unless LOGIS_MOTION
// outranks them), and the body is blurred behind where both the config's
// and the theme's blur want it.

// resizer is a surface whose visible panel can ask for another size.
type resizer interface{ Resize(scale.Size) }

// retheme wears th from now on; th is never changed after.
func (u *ui) retheme(th *theme.Theme) {
	u.emit("theme %s", th.Name)
	if u.setFaces != nil && !sameFaces(u.theme.Faces, th.Faces) {
		if err := u.setFaces(th.Faces); err != nil && u.logf != nil {
			u.logf("the theme's fonts fall back: " + err.Error())
		}
	}
	u.theme = th
	u.wearMotion()
	u.wearBlur()
	if sh := u.shown; sh != nil {
		asked := list.DefaultOptions()
		asked.Lines, asked.Width = sh.asked[0], sh.asked[1]
		size, rows := scene.Fit(asked, th)
		sh.view.SetLines(rows)
		sh.rows = rows
		if r, ok := u.surf.(resizer); ok {
			r.Resize(size)
		}
	}
	u.surf.RequestRedraw()
}

func sameFaces(a, b []text.Face) bool {
	return slices.EqualFunc(a, b, func(x, y text.Face) bool {
		return x.Weight == y.Weight && slices.Equal(x.Families, y.Families)
	})
}

// mover is a surface that moves and blurs its panels.
type mover interface {
	SetMotion(motion.Motion)
	SetBlur(on bool)
}

// motion is the motion of panels shown in the theme worn now.
func (u *ui) motion() motion.Motion {
	if u.envMotion != nil {
		return *u.envMotion
	}
	m := u.theme.Motion
	return motion.Timed(m.Open, m.Close, m.Select)
}

// wearMotion moves the panels shown from now on in the theme's motion.
func (u *ui) wearMotion() {
	if m, ok := u.surf.(mover); ok {
		m.SetMotion(u.motion())
	}
}

// wearBlur blurs behind the body where the config and the theme want it.
func (u *ui) wearBlur() {
	if m, ok := u.surf.(mover); ok {
		m.SetBlur(u.blur && u.theme.Panel.Blur)
	}
}

// framedSurface is a surface that draws each frame at its moment and in
// its transition's pose.
type framedSurface interface {
	ShowFrames(size scale.Size, paint window.FramePainter) error
}

// present shows sh's panel; a surface without frame timing draws it at
// rest, at the clock's time.
func (u *ui) present(size scale.Size, sh *shown) error {
	paint := u.painter(sh)
	if s, ok := u.surf.(framedSurface); ok {
		return s.ShowFrames(size, paint)
	}
	return u.surf.Show(size, func(g window.Geometry) bool {
		var at uint64
		if u.clock != nil {
			at = u.clock()
		}
		return paint(window.Frame{Geometry: g, FrameTime: window.FrameTime{At: at}, Pose: motion.Rest}).Moving
	})
}

// painter draws sh's list each frame in the theme worn then (Rust
// wear.rs drawing), at the frame's moment and in its pose: the selection
// band glides to the selected row, the theme's continuous effects (glow
// pulse, travelling highlight, flicker, a pending row's shimmer) move, and
// either keeps the window drawing on every frame callback (scene.Moving);
// the body is blurred behind.
func (u *ui) painter(sh *shown) window.FramePainter {
	return func(f window.Frame) window.Drawing {
		look := u.lookOf(sh)
		measure := scene.NoMeasure
		if u.measure != nil {
			measure = u.measure(f.Geometry)
		}
		sh.columns.Fit(&look, u.theme, measure)
		target, selected := scene.BandTop(&look, f.Logical, u.theme)
		band, banded, gliding := sh.glide.Toward(target, selected, f.At, f.Interval)
		if u.draw != nil {
			var icons scene.Icons
			if u.icons != nil {
				icons = u.icons(f.Geometry)
			}
			sc := scene.DrawAt(&look, f.Logical, band, banded, scene.Style{Theme: u.theme, At: f.At}, icons)
			f.Pose.Apply(sc)
			u.draw(sc, f.Geometry)
		}
		body, radius := scene.Body(f.Logical, u.theme)
		return window.Drawing{
			Moving:   gliding || scene.Moving(&look, u.theme),
			Backdrop: &window.Backdrop{Rect: body, Radius: radius},
		}
	}
}
