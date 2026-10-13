package scene_test

import (
	"path/filepath"
	"strings"
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
// time 0), fonts are pinned. Columns are measured with the renderer's
// fonts at each scale, afresh per theme and scale, as Rust's check_with
// does.

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
			l := l
			(&scene.Columns{}).Fit(&l, &th, measureAt(h, s))
			img, err := h.Offscreen(make(h, s, &l, logical, scene.Style{Theme: &th}), logical, s)
			if err != nil {
				t.Fatal(err)
			}
			g.Assert(t, "themes/"+th.Name, name+"@"+rendertest.ScaleSuffix(s), img, rendertest.Offscreen)
		}
	}
}

// measureAt measures with h's fonts at s (Rust Renderer::measure).
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
// launcher open; the rows of the design canvas. Its Go golden stands in for
// Rust's because of two pinned divergences (row order, emphasis face; see
// TestGoldenTheWindowDiffersFromRustsOnlyByItsPinnedDivergences).
func TestGoldenTheThemesWindowWithTheRitesOfTheCanvas(t *testing.T) {
	o := list.DefaultOptions()
	o.Counter = true
	v := list.NewView(o)
	v.Push(windowRows())
	v.Apply(input.Insert("o"))
	checkThemes(t, "window", v, o, nil, plainScene)
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

// The window case differs from Rust's golden by two pinned divergences
// only (feat/logis-go G20), not by its measured columns: drawn with
// Rust's row order and cosmic-text's word-wide emphasis face imitated, it
// equals Rust's golden but for anti-aliasing.
//   - Tie order: nucleo scores "power-profile\t·\t…" 16 for "o" where its
//     ASCII twin scores 36: its non-ASCII scan carries the class of the
//     last match, not of the character before (internal/fuzzy divergence
//     (a), testdata/divergences.json). logis scores it 36, so it keeps its
//     input place among the rows tied at 36; Rust ranks it below them.
//   - Emphasis face: cosmic-text 0.19 shapes a simple ASCII word in the
//     attributes of its first byte when every span set on it agrees with
//     them, and spans equal to the line's defaults are never set; so a
//     match at a word's start sets the whole word in the match face ("of",
//     "off", "on" semibold in forge; the colour stays per letter). logis
//     sets only the matched letters apart, as Rust's rows.rs describes
//     (internal/text divergences).
func TestGoldenTheWindowDiffersFromRustsOnlyByItsPinnedDivergences(t *testing.T) {
	h := rendertest.Headless(t)
	g := rendertest.CheckedIn(t)
	g.Alpha = rendertest.Straight
	o := list.DefaultOptions()
	o.Counter = true
	for _, themeName := range theme.Bundled() {
		th, err := theme.Themes{}.Load(themeName)
		if err != nil {
			t.Fatal(err)
		}
		logical, rows := scene.Fit(o, &th)
		v := list.NewView(o)
		v.SetLines(rows)
		v.Push(windowRows())
		v.Apply(input.Insert("o"))
		l := v.Look()
		var order []string
		for _, r := range l.Rows {
			order = append(order, strings.SplitN(r.Text, "\t", 2)[0])
		}
		if want := "do-not-disturb keyboard-layout mouse-autohide-toggle power-profile night-light theme vox"; strings.Join(order, " ") != want {
			t.Fatalf("rows %v, want %s", order, want)
		}
		// Rust's order: the power-profile row below night-light and theme.
		l.Rows = append(l.Rows[:3:3], l.Rows[4], l.Rows[5], l.Rows[3], l.Rows[6])
		h.SetFonts(rendertest.PinnedFonts(t))
		if err := h.SetFaces(th.Faces); err != nil {
			t.Fatal(err)
		}
		for _, s := range goldenScales {
			l := l
			(&scene.Columns{}).Fit(&l, &th, measureAt(h, s))
			sc := scene.Draw(&l, logical, scene.Style{Theme: &th})
			for i := range sc.Texts {
				if !sc.Texts[i].UnderlineEmphasis {
					wordWide(&sc.Texts[i])
				}
			}
			img, err := h.Offscreen(sc, logical, s)
			if err != nil {
				t.Fatal(err)
			}
			rust, err := rendertest.ReadPNG(g.RustPath("themes/"+th.Name, "window@"+rendertest.ScaleSuffix(s)))
			if err != nil {
				t.Fatal(err)
			}
			rust = rendertest.Premultiply(rust)
			c, err := rendertest.Compare(img, rust, rendertest.Offscreen.Channel)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s @%s: %v", th.Name, rendertest.ScaleSuffix(s), c)
			if float64(c.Outliers) > 0.003*float64(c.Total) {
				t.Errorf("%s @%s: beyond anti-aliasing: %v", th.Name, rendertest.ScaleSuffix(s), c)
			}
		}
	}
}

// wordWide imitates cosmic-text's fast path: an emphasis that starts an
// ASCII word takes the face to the word's end (its colour spans stay).
func wordWide(t *render.Text) {
	for i, e := range t.Emphasis {
		if e.Start > 0 && t.Text[e.Start-1] != ' ' {
			continue
		}
		end := e.End
		for end < len(t.Text) && t.Text[end] != ' ' {
			end++
		}
		t.Emphasis[i].End = end
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
// first is as wide as the widest name in view (21 characters of a
// monospace face, about 0.6 em each), the second as "auto", and every
// row's columns start at the same place (Rust
// columns_are_measured_with_the_row_font).
func TestGoldenColumnsAreMeasuredWithTheRowFont(t *testing.T) {
	h := rendertest.Headless(t)
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
