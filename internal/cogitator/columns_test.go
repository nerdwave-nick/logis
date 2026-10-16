package cogitator

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The window measures the columns of the rows it draws: as wide as the
// widest text shown since the list opened, never narrower while typing; a
// provider's next stage starts afresh.

// tenPerChar measures ten pixels a character, at the frame's scale it was
// asked for.
func tenPerChar(scales *[]scale.Scale) func(g window.Geometry) scene.Measure {
	return func(g window.Geometry) scene.Measure {
		*scales = append(*scales, g.Scale)
		return scene.MeasureFunc(func(t render.Text) (float32, bool) {
			return 10 * float32(utf8.RuneCountInString(t.Text)), true
		})
	}
}

// columnStart is where the drawn text words starts, less where first
// starts: how far a second column stands from the first.
func columnStart(t *testing.T, sc *render.Scene, first, words string) float32 {
	t.Helper()
	at := map[string]float32{}
	for _, x := range sc.Texts {
		at[x.Text] = x.Origin[0]
	}
	a, ok1 := at[first]
	b, ok2 := at[words]
	if !ok1 || !ok2 {
		t.Fatalf("%q or %q not drawn", first, words)
	}
	return b - a
}

func (r *rig) paintOnce(t *testing.T) *render.Scene {
	t.Helper()
	var drawn *render.Scene
	r.ui.draw = func(sc *render.Scene, _ window.Geometry) { drawn = sc }
	r.surf.paint(window.Geometry{Logical: r.surf.size, Physical: r.surf.size, Scale: scale.From120ths(156)})
	if drawn == nil {
		t.Fatal("nothing drawn")
	}
	return drawn
}

func TestTheWindowMeasuresTheColumnsAtTheFramesScale(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.show(1, list.DefaultOptions(), "abcdefghij\tx", "ab\ty")
	sc := r.paintOnce(t)
	gap := r.ui.theme.Row.ColumnGap
	if got := columnStart(t, sc, "abcdefghij", "x"); got != 100+gap {
		t.Errorf("second column at %v, want %v", got, 100+gap)
	}
	if len(scales) != 1 || scales[0] != scale.From120ths(156) {
		t.Errorf("measured at %v, want the frame's 1.3", scales)
	}
}

func TestColumnsKeepTheirWidthsWhileTheQueryNarrowsTheList(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.show(1, list.DefaultOptions(), "abcdefghij\tx", "ab\ty")
	r.paintOnce(t)
	r.act(input.Insert("y"))
	sc := r.paintOnce(t)
	if got, want := columnStart(t, sc, "ab", "y"), 100+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("second column at %v after typing, want %v", got, want)
	}
}

func TestAProvidersNextStageMeasuresItsColumnsAfresh(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.showProvider(1, list.DefaultOptions(), entries("abcdefghij\tx")...)
	r.paintOnce(t)
	r.ui.step(1, provider.Step{Kind: provider.StepStage, Options: list.DefaultOptions(), Entries: entries("abc\ty")})
	sc := r.paintOnce(t)
	if got, want := columnStart(t, sc, "abc", "y"), 30+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("second column at %v in the next stage, want %v", got, want)
	}
}

// The app launcher's actions stage (Shift+Return on an app) is a next
// stage of the apps session: its columns start afresh, not at the widths
// of the app list before it. Rows show an app's Name, which may hold a tab
// (desktop entries unescape \t), so its parts line up as columns.
func TestAnAppsActionsStageMeasuresItsColumnsAfresh(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktop := map[string]string{
		"anvil.desktop": "[Desktop Entry]\nType=Application\nName=Anvil of the very long forge\\tx\nExec=true\n",
		"bolt.desktop": "[Desktop Entry]\nType=Application\nName=Bolt\\tb\nExec=true\nActions=fire;\n\n" +
			"[Desktop Action fire]\nName=Fire\\tf\nExec=true\n",
	}
	for name, text := range desktop {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix := apps.Scan(apps.Dirs{dir}, apps.Context{Path: filepath.SplitList(os.Getenv("PATH"))})
	feed, first := provider.OpenApps(ix, apps.NewFrecency(), 1_760_000_000)
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.ui.show(daemon.Session{ID: 1, Mode: protocol.ModeApps, Options: first.Options, Entries: first.Entries})
	if got, want := columnStart(t, r.paintOnce(t), "Anvil of the very long forge", "x"), 280+r.ui.theme.Row.ColumnGap; got != want {
		t.Fatalf("app list's second column at %v, want %v", got, want)
	}
	bolt := slices.IndexFunc(first.Entries, func(e list.Entry) bool { return e.Text == "Bolt\tb" })
	said := feed.Answer(provider.Answer{Kind: provider.AnswerDescend, Index: bolt}, ix, nil)
	if len(said.Steps) != 1 || said.Steps[0].Kind != provider.StepStage {
		t.Fatalf("Shift+Return said %+v", said)
	}
	r.ui.step(1, said.Steps[0])
	if got, want := columnStart(t, r.paintOnce(t), "Fire", "f"), 40+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("actions stage's second column at %v, want %v", got, want)
	}
}

// Servitor mode is a provider session: each of its stages (rites, a
// rite's aspects, the rites again after an invocation, its census read
// anew) is a next stage whose columns start afresh, so a rite gone from
// the census leaves no wide column behind.
func TestAServitorsRitesStageAgainMeasuresItsColumnsAfresh(t *testing.T) {
	prompt := "rite: "
	rites := func(names ...string) provider.Stage {
		s := provider.Stage{ID: "rite", Prompt: &prompt, Placeholder: "choose a rite"}
		for _, n := range names {
			s.Items = append(s.Items, provider.Item{ID: n, Columns: []string{n, "on", "a purpose"}})
		}
		return s
	}
	var stages provider.Stages
	apply := func(m provider.Message) provider.Step {
		t.Helper()
		steps, err := stages.Apply(m)
		if err != nil || len(steps) != 1 {
			t.Fatalf("%v: %+v", err, steps)
		}
		return steps[0]
	}
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	first := apply(rites("mouse-autohide-toggle", "vox"))
	r.showProvider(1, first.Options, first.Entries...)
	if got, want := columnStart(t, r.paintOnce(t), "vox", "on"), 210+r.ui.theme.Row.ColumnGap; got != want {
		t.Fatalf("rites' second column at %v, want %v", got, want)
	}
	aspect := "aspect: "
	r.ui.step(1, apply(provider.Stage{ID: "aspect", Prompt: &aspect,
		Items: []provider.Item{{ID: "on", Columns: []string{"on"}}, {ID: "off", Columns: []string{"off"}}}}))
	r.paintOnce(t)
	r.ui.step(1, apply(rites("vox", "theme")))
	if got, want := columnStart(t, r.paintOnce(t), "vox", "on"), 50+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("rites again: second column at %v, want %v", got, want)
	}
}
