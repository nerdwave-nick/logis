package cogitator

import (
	"slices"

	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The theme the window wears (Rust src/daemon/window/wear.rs put_on and
// ui.rs Command::Theme): worn from now on, the visible panel included,
// which is re-fitted to the theme's size (in the terms its session asked
// for) and drawn again. Fontconfig is asked again only when the faces
// change. With the test trace on it reports `theme <name>`. Motion timings
// and blur behind the body are the motion ticket's.

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
	if sh := u.shown; sh != nil {
		size, rows := scene.Fit(sh.asked, th)
		sh.view.SetLines(rows)
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
