package scene_test

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/icons"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The list as drawn in each bundled theme: the prompt line with query,
// caret and counter, tab-separated columns, the selection band and matches
// set apart; a provider's stage with glyphs, badges, a pending row and a
// toast; the app launcher's icons; and the theme's own window with its
// effects. Compared with goldens per theme at 1.0, 1.3 and 2.0 in
// tests/goldens/themes/<theme>/, stored with straight alpha
// (rendertest.Straight). Deterministic: the frame's time is frozen (the
// glow at full strength, the highlight at the top, grain and flicker at
// time 0), fonts are pinned. Columns are measured with the renderer's fonts
// at each scale, afresh per theme and scale.

var goldenScales = []scale.Scale{scale.One, scale.From120ths(156), scale.FromInteger(2)}

// bentGrain is the offscreen tolerance with a few outliers: both bundled
// themes bend their panel (cd816303), and the CRT pass takes each pixel's
// grain from the cell under its bent position, floor(q). Where q lies on a
// cell's edge, a driver that rounds the bend's last bit otherwise takes
// the neighbouring cell's noise: llvmpipe against Intel Arc, 1 to 38
// pixels per golden (at most 0.002 %), none with the grain off.
var bentGrain = rendertest.Tolerance{Channel: rendertest.Offscreen.Channel, Outliers: 0.0001}

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
			l := l
			(&scene.Columns{}).Fit(&l, &th, measureAt(h, s))
			img, err := h.Offscreen(make(h, s, &l, logical, scene.Style{Theme: &th}), logical, s)
			if err != nil {
				t.Fatal(err)
			}
			g.Assert(t, "themes/"+th.Name, name+"@"+rendertest.ScaleSuffix(s), img, bentGrain)
		}
	}
}

// measureAt measures with h's fonts at s.
func measureAt(h *render.Headless, s scale.Scale) scene.Measure {
	return scene.MeasureFunc(func(t render.Text) (float32, bool) { return h.MeasureText(t, s) })
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
	v.Push(windowRows())
	v.Apply(input.Insert("o"))
	checkThemes(t, "window", v, o, nil, plainScene)
}

// The theme's window frozen mid-episode of its glitch (33cfcf44): a burst
// at its peak, 5 s and half a 30 Hz step after the opening, so torn rows,
// blocks, the noise band and the split colours show at the theme's
// strength. The other goldens show the glitch at rest (time 0, no burst).
func TestGoldenTheThemesWindowMidGlitch(t *testing.T) {
	o := list.DefaultOptions()
	o.Counter = true
	v := list.NewView(o)
	v.Push(windowRows())
	v.Apply(input.Insert("o"))
	midGlitch := func(_ *render.Headless, _ scale.Scale, l *list.Look, logical scale.Size, st scene.Style) *render.Scene {
		st.Glitch = effects.Moment{Time: 5 + 0.5/30, Event: 1}
		return scene.Draw(l, logical, st)
	}
	checkThemes(t, "glitch", v, o, nil, midGlitch)
}

// windowRows are the rows of the theme's window case.
func windowRows() []list.Entry {
	return []list.Entry{
		decorated("do-not-disturb\ton\tSilence the vox of the desktop", "", nil, false),
		decorated("keyboard-layout\tus\tThe tongue of the keys", "", nil, false),
		decorated("mouse-autohide-toggle\toff\tVeil the cursor when idle", "", nil, false),
		decorated("power-profile\t·\tThe hunger of the machine spirit", "", nil, true),
		decorated("vox\tauto\tWho hears the machine", "", []string{"edict"}, false),
		decorated("night-light\toff\tDim the light of the screens", "", nil, false),
		decorated("theme\tforge\tThe visage of the machine", "", nil, false),
	}
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

// The columns are measured with each theme's row font at each scale: the
// first is as wide as the widest name in view (21 characters of a monospace
// face, about 0.6 em each), the second as "auto", and every row's columns
// start at the same place.
func TestGoldenColumnsAreMeasuredWithTheRowFont(t *testing.T) {
	h := rendertest.HeadlessForStructure(t)
	for _, themeName := range theme.Bundled() {
		th, err := theme.Themes{}.Load(themeName)
		if err != nil {
			t.Fatal(err)
		}
		h.SetFonts(rendertest.PinnedFonts(t))
		if err := h.SetFaces(th.Faces); err != nil {
			t.Fatal(err)
		}
		for _, s := range goldenScales {
			v := list.NewView(list.DefaultOptions())
			v.Push(entries("mouse-autohide-toggle\toff\tVeil the cursor when idle", "vox\tauto\tWho hears the machine"))
			l := v.Look()
			(&scene.Columns{}).Fit(&l, &th, measureAt(h, s))
			em := th.Row.Font.Size
			if len(l.Columns) != 2 {
				t.Fatalf("%s: two measured columns: %v", th.Name, l.Columns)
			}
			name, aspect := l.Columns[0], l.Columns[1]
			if name < 21*0.5*em || name >= 21*0.7*em {
				t.Errorf("%s @%v: name column %v", th.Name, s, name)
			}
			if d := aspect - name*4/21; d <= -2 || d >= 2 {
				t.Errorf("%s @%v: aspect %v, name %v", th.Name, s, aspect, name)
			}
			logical, _ := scene.Fit(list.DefaultOptions(), &th)
			sc := scene.Draw(&l, logical, scene.Style{Theme: &th})
			x := func(words string) float32 {
				for _, t := range sc.Texts {
					if t.Text == words {
						return t.Origin[0]
					}
				}
				t.Fatalf("%q not drawn", words)
				return 0
			}
			if x("off") != x("auto") || x("Veil the cursor when idle") != x("Who hears the machine") {
				t.Errorf("%s @%v: columns do not line up", th.Name, s)
			}
			if x("off") < x("mouse-autohide-toggle")+name {
				t.Errorf("%s @%v: off at %v, before the name's end %v", th.Name, s, x("off"), x("mouse-autohide-toggle")+name)
			}
		}
	}
}

// Plain logis's tab strip (8cba2fc9) in each bundled theme, each tab lit in
// turn: the plates with their counts and the key hint above the theme's own
// window, as the owner's references show it (lab v6: Rites lit with its 7
// rites, Applications with 22 apps).
func TestGoldenTheTabStripWithEachTabLit(t *testing.T) {
	apps := entries("kitty", "Abyssus", "Alacritty", "Alina of the Arena", "ALSA Scarlett Control Panel",
		"Audacity", "Avahi SSH Server Browser", "Avahi VNC Server Browser", "Blender", "btop++", "Discord")
	for lit, name := range []string{"tabs-applications", "tabs-rites"} {
		o := list.DefaultOptions()
		o.Placeholder = []string{"summon an application", "seek a rite"}[lit]
		v := list.NewView(o)
		v.Push([][]list.Entry{apps, windowRows()}[lit])
		strip := scene.Strip{Tabs: [2]scene.Tab{{Label: "applications", Count: 22, Icon: scene.AppsIcon},
			{Label: "rites", Count: 7, Icon: scene.RitesIcon}}, Active: lit}
		checkTabbed(t, name, v, o, strip)
	}
}

// Silent rites (2aaad4b9) in each bundled theme: plain logis whose caller
// has no servitor on its PATH, on its Rites tab; the plate counts "—" and
// the tab says why.
func TestGoldenSilentRites(t *testing.T) {
	o := list.DefaultOptions()
	o.Placeholder = "seek a rite"
	strip := scene.Strip{Tabs: [2]scene.Tab{{Label: "applications", Count: 22, Icon: scene.AppsIcon},
		{Label: "rites", Icon: scene.RitesIcon, Silent: true}}, Active: 1}
	checkTabbed(t, "tabs-rites-silent", list.NewView(o), o, strip)
}

// checkTabbed is checkThemes for a panel under the tab strip st.
func checkTabbed(t *testing.T, name string, view *list.View, o list.Options, st scene.Strip) {
	t.Helper()
	h := rendertest.Headless(t)
	g := rendertest.CheckedIn(t)
	g.Alpha = rendertest.Straight
	for _, themeName := range theme.Bundled() {
		th, err := theme.Themes{}.Load(themeName)
		if err != nil {
			t.Fatal(err)
		}
		logical, rows := scene.FitTabbed(o, &th, 0)
		view.SetLines(rows)
		h.SetFonts(rendertest.PinnedFonts(t))
		if err := h.SetFaces(th.Faces); err != nil {
			t.Fatal(err)
		}
		for _, s := range goldenScales {
			l := view.Look()
			(&scene.Columns{}).Fit(&l, &th, measureAt(h, s))
			band, banded := scene.TabbedBandTop(&l, logical, &th)
			st.Measure = measureAt(h, s)
			sc := scene.DrawTabbed(&l, st, logical, band, banded, scene.Style{Theme: &th}, nil)
			img, err := h.Offscreen(sc, logical, s)
			if err != nil {
				t.Fatal(err)
			}
			g.Assert(t, "themes/"+th.Name, name+"@"+rendertest.ScaleSuffix(s), img, bentGrain)
		}
	}
}
