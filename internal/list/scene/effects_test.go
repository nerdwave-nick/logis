package scene

import (
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The post-processing effects over the body, the phosphor theme's look, and
// a personal theme's sigil image (feat/logis-go G18).

func phosphorTheme(t *testing.T) *theme.Theme {
	t.Helper()
	p, err := theme.Themes{}.Load("cogitator-phosphor")
	if err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestTheEffectsCoverTheBody(t *testing.T) {
	th := forge()
	logical := sized(th)
	rect, radius := Body(logical, th)
	post := Draw(look(3, 0), logical, still(th)).Post
	if post.Rect != rect || post.Radius != radius || post.CRT != th.Effects || post.Inset != th.Border.Width {
		t.Fatalf("%+v", post)
	}
	plain := *th
	plain.Effects, plain.Glitch = theme.Effects{}, effects.Glitch{}
	if p := Draw(look(3, 0), logical, still(&plain)).Post; p.Any() {
		t.Fatalf("a theme without effects asks for the pass: %+v", p)
	}
}

// The effects' time is the frame's, in seconds within the hour, while the
// theme moves on its own, and frozen at 0 otherwise (the grain stands
// still under reduce-motion).
func TestTheGrainMovesWithTheFrameOnlyWhileTheThemeMoves(t *testing.T) {
	th := forge()
	grain := func(th *theme.Theme, at uint64) float32 {
		return Draw(look(1, 0), sized(th), Style{Theme: th, At: at}).Post.Time
	}
	if got := grain(th, 1_500_000_000); got != 1.5 {
		t.Errorf("%v", got)
	}
	if got := grain(th, 3_600_000_000_000+2_000_000_000); got != 2 {
		t.Errorf("within the hour: %v", got)
	}
	s := th.Stilled()
	if got := grain(&s, 1_500_000_000); got != 0 {
		t.Errorf("still grain: %v", got)
	}
}

// Phosphor's label is "SUMMON>"; the default prompt yields to it too.
func TestPhosphorsLabelStandsBeforeTheQuery(t *testing.T) {
	p := phosphorTheme(t)
	s := Draw(look(1, 0), sized(p), still(p))
	find(t, s, "SUMMON>")
	find(t, s, "rite")
}

func TestPhosphorInvertsTheSelectionAndUnderlinesMatches(t *testing.T) {
	p := phosphorTheme(t)
	s := Draw(look(3, 1), sized(p), still(p))
	selected := find(t, s, "row 1")
	if selected.Color != p.Selection.Columns[0] || !selected.UnderlineEmphasis {
		t.Fatalf("%+v", selected)
	}
	// The band is solid phosphor with a glow and no edge bar.
	i := slices.IndexFunc(s.Shapes, func(r render.RoundedRect) bool {
		return r.Fill == render.Solid(p.Selection.Band[0])
	})
	if i < 0 || s.Shapes[i].Glow.Radius == 0 {
		t.Fatalf("the band: %d", i)
	}
	// A dashed rule: many short bars.
	ruleY := p.Window.Margin + p.Prompt.Height
	dashes := 0
	for _, r := range s.Shapes {
		if r.Rect[1] == ruleY {
			dashes++
		}
	}
	if dashes <= 20 {
		t.Fatalf("%d dashes", dashes)
	}
}

// Phosphor brackets its badges instead of drawing pills.
func TestPhosphorBracketsItsBadges(t *testing.T) {
	p := phosphorTheme(t)
	s := Draw(decorated(), sized(p), still(p))
	find(t, s, "[EDICT]")
	find(t, s, "[▒▒▒ AUGURING]")
}

// asked is an icon source that records what it is asked for and finds all
// of it.
type asked []struct {
	name string
	side uint32
}

func (a *asked) Image(name string, side uint32) (render.ImageID, bool) {
	*a = append(*a, struct {
		name string
		side uint32
	}{name, side})
	return render.ImageID(len(*a)), true
}

// A personal theme's sigil is an image beside the theme file; a bundled
// theme has no directory, so only an absolute path is found.
func TestAPersonalSigilIsAnImageBesideTheTheme(t *testing.T) {
	th := forge()
	th.Dir = "/home/u/.config/logis/themes"
	th.Prompt.Sigil.Image, th.Prompt.Sigil.Size = "sigils/skull.svg", 22
	logical := sized(th)
	var a asked
	s := DrawWith(look(1, 0), logical, still(th), &a)
	if len(a) != 1 || a[0].name != "/home/u/.config/logis/themes/sigils/skull.svg" || a[0].side != 22 {
		t.Fatalf("asked %v", a)
	}
	if len(s.Images) != 1 || s.Images[0].Origin[0]+22 >= find(t, s, "INVOKE").Origin[0] {
		t.Fatalf("the sigil before the label: %+v", s.Images)
	}
	th.Dir = ""
	a = nil
	DrawWith(look(1, 0), logical, still(th), &a)
	if len(a) != 0 {
		t.Fatalf("a bundled theme's relative sigil was looked for: %v", a)
	}
	th.Prompt.Sigil.Image = "/usr/share/logis/eye.png"
	DrawWith(look(1, 0), logical, still(th), &a)
	if len(a) != 1 || a[0].name != "/usr/share/logis/eye.png" {
		t.Fatalf("asked %v", a)
	}
}

// The glitch covers the body with the theme's controls at the style's
// moment (the window's glitch clock); off (reduce-motion), the scene asks
// for nothing of it, whatever the moment.
func TestTheGlitchTakesTheStylesMoment(t *testing.T) {
	th := forge()
	m := effects.Moment{Time: 2.5, Event: 0.75}
	post := Draw(look(1, 0), sized(th), Style{Theme: th, Glitch: m}).Post
	if post.Glitch != th.Glitch || post.Moment != m {
		t.Fatalf("%+v %+v", post.Glitch, post.Moment)
	}
	s := th.Stilled()
	still := Draw(look(1, 0), sized(&s), Style{Theme: &s, Glitch: m}).Post
	if still.Glitch.On() || still.Moment != (effects.Moment{}) {
		t.Errorf("stilled: %+v %+v", still.Glitch, still.Moment)
	}
}
