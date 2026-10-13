package scene_test

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/icons"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The list as drawn in each bundled theme (Rust tests/list.rs): the prompt
// line with query, caret and counter, tab-separated columns, the selection
// band and matches set apart; a provider's stage with glyphs, badges, a
// pending row and a toast; the app launcher's icons; and the theme's own
// window with its effects. Compared with goldens per theme at 1.0, 1.3 and
// 2.0 in tests/goldens/themes/<theme>/, stored with straight alpha
// (rendertest.Straight). Deterministic: the frame's time is frozen (the
// glow at full strength, the highlight at the top, grain and flicker at
// time 0), fonts are pinned. Columns are estimated from the rows in view
// until measured columns arrive (feat/logis-go G20); Rust's goldens have
// measured ones, so where a case has tab columns the Go golden stands in.

var goldenScales = []scale.Scale{scale.One, scale.From120ths(156), scale.FromInteger(2)}

// makeScene builds a case's scene for a renderer at a scale (icons, if
// any, go into its atlas).
type makeScene func(h *render.Headless, s scale.Scale, l *list.Look, logical scale.Size, st scene.Style) *render.Scene

func plainScene(_ *render.Headless, _ scale.Scale, l *list.Look, logical scale.Size, st scene.Style) *render.Scene {
	return scene.Draw(l, logical, st)
}

// checkThemes renders view's list (sized by o) in every bundled theme at
// every scale and compares each with themes/<theme>/<name>@<scale>.
func checkThemes(t *testing.T, name string, view *list.View, o list.Options, toast *list.Notice, make makeScene) {
	t.Helper()
	h := rendertest.Headless(t)
	g := rendertest.CheckedIn(t)
	g.Alpha = rendertest.Straight
	for _, themeName := range theme.Bundled() {
		th, err := theme.Themes{}.Load(themeName)
		if err != nil {
			t.Fatal(err)
		}
		logical, rows := scene.Fit(o, &th)
		view.SetLines(rows)
		l := view.Look()
		l.Toast = toast
		h.SetFonts(rendertest.PinnedFonts(t))
		if err := h.SetFaces(th.Faces); err != nil {
			t.Fatal(err)
		}
		for _, s := range goldenScales {
			img, err := h.Offscreen(make(h, s, &l, logical, scene.Style{Theme: &th}), logical, s)
			if err != nil {
				t.Fatal(err)
			}
			g.Assert(t, "themes/"+th.Name, name+"@"+rendertest.ScaleSuffix(s), img, rendertest.Offscreen)
		}
	}
}

func u32(n uint32) *uint32 { return &n }

func entries(texts ...string) []list.Entry {
	out := make([]list.Entry, len(texts))
	for i, t := range texts {
		out[i] = list.Entry{Text: t}
	}
	return out
}

func decorated(text, glyph string, badges []string, pending bool) list.Entry {
	return list.Entry{Text: text, Decor: &list.Decor{Glyph: glyph, Badges: badges, Pending: pending}}
}

func TestGoldenAFilteredListWithColumnsHighlightsAndACounter(t *testing.T) {
	o := list.DefaultOptions()
	o.Lines, o.Width, o.Counter = u32(5), u32(50), true
	v := list.NewView(o)
	v.Push(entries("rite of oil\tunguent", "rite of ignition\tspark", "café noir\tdrink", "機械の精霊\tspirit",
		"Ἐν ἀρχῇ ἦν ὁ λόγος", "the rite of the long litany that never ends and is cut short\tlong"))
	v.Apply(input.Insert("rite"))
	v.Apply(input.ActNext)
	checkThemes(t, "filtered", v, o, nil, plainScene)
}

func TestGoldenAPromptOnlyPasswordWithItsMask(t *testing.T) {
	o := list.DefaultOptions()
	o.Prompt, o.Lines, o.Width, o.Password = "pin: ", u32(0), u32(40), '*'
	v := list.NewView(o)
	v.Apply(input.Insert("1234"))
	checkThemes(t, "password", v, o, nil, plainScene)
}

func TestGoldenAnEmptyQueryShowsThePlaceholderAndEveryItem(t *testing.T) {
	o := list.DefaultOptions()
	o.Lines, o.Width, o.Placeholder = u32(3), u32(40), "seek the rite"
	v := list.NewView(o)
	v.Push(entries("alpha", "beta", "gamma", "delta"))
	checkThemes(t, "placeholder", v, o, nil, plainScene)
}

func TestGoldenAProviderStageWithGlyphsBadgesAPendingRowAndAToast(t *testing.T) {
	o := list.DefaultOptions()
	o.Prompt, o.Lines, o.Width = "rite: ", u32(5), u32(52)
	v := list.NewView(o)
	v.Push([]list.Entry{
		decorated("rite of oil\tunguent", "⚙", []string{"current"}, false),
		decorated("rite of ignition\tspark", "†", nil, true),
		decorated("litany of the forge", "", []string{"edict", "heresy"}, false),
		decorated("機械の精霊", "§", nil, false),
	})
	v.Apply(input.ActNext)
	checkThemes(t, "provider", v, o, &list.Notice{Text: "the rite of oil is begun", Level: list.Success}, plainScene)
}

// The theme's own window: no size asked, as servitor mode and the app
// launcher open; the rows of the design canvas.
func TestGoldenTheThemesWindowWithTheRitesOfTheCanvas(t *testing.T) {
	o := list.DefaultOptions()
	o.Counter = true
	v := list.NewView(o)
	v.Push([]list.Entry{
		decorated("do-not-disturb\ton\tSilence the vox of the desktop", "", nil, false),
		decorated("keyboard-layout\tus\tThe tongue of the keys", "", nil, false),
		decorated("mouse-autohide-toggle\toff\tVeil the cursor when idle", "", nil, false),
		decorated("power-profile\t·\tThe hunger of the machine spirit", "", nil, true),
		decorated("vox\tauto\tWho hears the machine", "", []string{"edict"}, false),
		decorated("night-light\toff\tDim the light of the screens", "", nil, false),
		decorated("theme\tforge\tThe visage of the machine", "", nil, false),
	})
	v.Apply(input.Insert("o"))
	checkThemes(t, "window", v, o, nil, plainScene)
}

// The app launcher's list: icons from the fixture icon theme in the icon
// column, a glyph where an icon is missing, nothing where neither is.
func TestGoldenAnAppListDrawsItsIcons(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "icons"))
	if err != nil {
		t.Fatal(err)
	}
	paths := icons.SearchPaths{
		IconDirs:   []string{filepath.Join(root, "home-icons"), filepath.Join(root, "share", "icons")},
		PixmapDirs: []string{filepath.Join(root, "pixmaps")},
	}
	o := list.DefaultOptions()
	o.Lines, o.Width = u32(5), u32(40)
	v := list.NewView(o)
	withIcon := func(text, icon, glyph string) list.Entry {
		return list.Entry{Text: text, Decor: &list.Decor{Icon: icon, Glyph: glyph}}
	}
	v.Push([]list.Entry{
		withIcon("Anoint", "forge-cog", ""),
		withIcon("Banner of the forge", "forge-banner", ""),
		withIcon("Cogitator skull", "cogitator-skull", ""),
		withIcon("Missing, with a glyph", "forge-absent", "⚙"),
		withIcon("Missing, without", "forge-absent", ""),
	})
	v.Apply(input.ActNext)
	checkThemes(t, "apps", v, o, nil, func(h *render.Headless, s scale.Scale, l *list.Look, logical scale.Size, st scene.Style) *render.Scene {
		cache := icons.NewCache(icons.LoadThemes(paths, "logis-forge"))
		sc := scene.DrawWith(l, logical, st, &icons.FrameIcons{Cache: cache, Atlas: h.Atlas(), Scale: s})
		if len(sc.Images) != 3 {
			t.Errorf("%d icons found, want 3", len(sc.Images))
		}
		return sc
	})
}
