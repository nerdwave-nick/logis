package provider

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/list"
)

// The app launcher as a provider: its stages, answers and launches.

const appsNow = 1_760_000_000

func appFixtures() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "apps")
}

func appIndex() *apps.Index {
	f := appFixtures()
	cx := apps.Context{Desktops: []string{"niri"}, Path: []string{filepath.Join(f, "bin")}}
	return apps.Scan(apps.Dirs{filepath.Join(f, "system/applications")}, cx)
}

func opened(t *testing.T, f *apps.Frecency) (*Apps, Step, *apps.Index) {
	t.Helper()
	ix := appIndex()
	a, step := OpenApps(ix, f, appsNow)
	if step.Kind != StepStage {
		t.Fatalf("%+v", step)
	}
	return a, step, ix
}

func texts(entries []list.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Text
	}
	return out
}

func TestTheFirstStageListsAppsByNameWithIconsAndMatchTexts(t *testing.T) {
	_, step, _ := opened(t, apps.NewFrecency())
	if !step.Options.OnlyMatch || step.Options.Placeholder != "summon an application" || step.Options.Prompt != "> " {
		t.Errorf("an app list takes no typed text: %+v", step.Options)
	}
	want := []string{"Anoint", "Broken in home", "Bus Spirit", "Cogitator shell",
		"Deleted in home", "Oil (system)", "Only in niri", "Present TryExec"}
	if got := texts(step.Entries); !slices.Equal(got, want) {
		t.Errorf("%q\nwant %q", got, want)
	}
	anoint := step.Entries[0]
	if !anoint.HasMatchText || anoint.MatchText != "Anoint\tHoly Oil\toil unguent blessing\tanoint" {
		t.Errorf("match text %q", anoint.MatchText)
	}
	if d := anoint.Decor; d == nil || d.Icon != "forge-cog" || !d.HasIcon || d.Boost != 0 {
		t.Errorf("decor %+v", d)
	}
	// An app without icon claims no icon column.
	if d := step.Entries[1].Decor; d == nil || d.HasIcon || d.Icon != "" {
		t.Errorf("decor %+v", d)
	}
}

func TestFrecentAppsComeFirstAndAreBoosted(t *testing.T) {
	f := apps.NewFrecency()
	f.Record("forge-oil.desktop", appsNow)
	f.Record("forge-oil.desktop", appsNow)
	f.Record("forge-terminal.desktop", appsNow-100*24*3600)
	_, step, _ := opened(t, f)
	if got := texts(step.Entries)[:3]; !slices.Equal(got, []string{"Oil (system)", "Cogitator shell", "Anoint"}) {
		t.Errorf("%q", got)
	}
	if step.Entries[0].Decor.Boost <= step.Entries[1].Decor.Boost {
		t.Errorf("boosts %d, %d", step.Entries[0].Decor.Boost, step.Entries[1].Decor.Boost)
	}
}

func row(t *testing.T, entries []list.Entry, text string) int {
	t.Helper()
	i := slices.IndexFunc(entries, func(e list.Entry) bool { return e.Text == text })
	if i < 0 {
		t.Fatalf("no row %q", text)
	}
	return i
}

func TestReturnLaunchesTheApp(t *testing.T) {
	a, step, ix := opened(t, apps.NewFrecency())
	said := a.Answer(Answer{Kind: AnswerItem, Index: row(t, step.Entries, "Anoint")}, ix, nil)
	if said.Launch == nil || said.ID != "forge-anoint.desktop" || said.Launch.Argv[0] != "anoint" || said.Launch.Action != "" {
		t.Errorf("%+v", said)
	}
}

func TestShiftReturnOpensTheActionsAndReturnThereLaunchesOne(t *testing.T) {
	a, step, ix := opened(t, apps.NewFrecency())
	said := a.Answer(Answer{Kind: AnswerDescend, Index: row(t, step.Entries, "Anoint")}, ix, nil)
	if said.Launch != nil || len(said.Steps) != 1 || said.Steps[0].Kind != StepStage {
		t.Fatalf("%+v", said)
	}
	stage := said.Steps[0]
	if stage.Options.Prompt != "Anoint: " || stage.Options.Placeholder != "" || !stage.Options.OnlyMatch {
		t.Errorf("%+v", stage.Options)
	}
	if got := texts(stage.Entries); !slices.Equal(got, []string{"Anoint", "Purge", "Bless"}) {
		t.Errorf("%q", got)
	}
	var icons []string
	for _, e := range stage.Entries {
		icons = append(icons, e.Decor.Icon)
	}
	if !slices.Equal(icons, []string{"forge-cog", "forge-purge", "forge-cog"}) {
		t.Errorf("icons %q", icons)
	}
	said = a.Answer(Answer{Kind: AnswerItem, Index: 1}, ix, nil)
	if said.Launch == nil || said.ID != "forge-anoint.desktop" || said.Launch.Action != "purge" {
		t.Fatalf("%+v", said)
	}
	said = a.Answer(Answer{Kind: AnswerDescend, Index: 0}, ix, nil)
	if said.Launch == nil || said.Launch.Action != "" {
		t.Errorf("the app itself: %+v", said)
	}
}

func TestShiftReturnOnAnAppWithoutActionsLaunchesIt(t *testing.T) {
	a, step, ix := opened(t, apps.NewFrecency())
	if said := a.Answer(Answer{Kind: AnswerDescend, Index: row(t, step.Entries, "Oil (system)")}, ix, nil); said.Launch == nil {
		t.Errorf("%+v", said)
	}
}

func TestAnAppThatCannotBePlannedSaysWhyAndTakesReturnAgain(t *testing.T) {
	a, step, ix := opened(t, apps.NewFrecency())
	said := a.Answer(Answer{Kind: AnswerItem, Index: row(t, step.Entries, "Cogitator shell")}, ix, nil)
	if said.Launch != nil || len(said.Steps) != 2 || said.Steps[0].Kind != StepToast || said.Steps[1].Kind != StepReady {
		t.Fatalf("%+v", said)
	}
	if n := said.Steps[0].Notice; n.Level != list.Error || !strings.Contains(n.Text, "terminal") ||
		!strings.HasPrefix(n.Text, "Cogitator shell cannot be summoned: ") {
		t.Errorf("%+v", n)
	}
	if said.Failure != said.Steps[0].Notice.Text {
		t.Errorf("the log is told %q", said.Failure)
	}
	if len(AppsFailed("x")) != 2 {
		t.Error("a failure is a toast and ready")
	}
}

func TestAnAppGoneFromTheIndexMeanwhileIsAnErrorNotAPanic(t *testing.T) {
	a, step, _ := opened(t, apps.NewFrecency())
	said := a.Answer(Answer{Kind: AnswerItem, Index: row(t, step.Entries, "Anoint")}, &apps.Index{}, nil)
	if len(said.Steps) != 2 || said.Steps[0].Kind != StepToast || said.Steps[0].Notice.Text != "forge-anoint.desktop is gone from the census" ||
		said.Failure != said.Steps[0].Notice.Text {
		t.Errorf("%+v", said)
	}
}

func TestTypedTextDismissalAndRowsOutOfRangeSayNothing(t *testing.T) {
	a, _, ix := opened(t, apps.NewFrecency())
	for _, answer := range []Answer{{Kind: AnswerTyped, Text: "x"}, {Kind: AnswerDismissed}, {Kind: AnswerItem, Index: 99}} {
		if said := a.Answer(answer, ix, nil); said.Launch != nil || len(said.Steps) != 0 {
			t.Errorf("%+v: %+v", answer, said)
		}
	}
}
