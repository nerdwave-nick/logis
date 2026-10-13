package servitor

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Ported from Rust src/servitor/flow/tests.rs. The recorded vigil
// (tests/fixtures/servitor/vigil.jsonl), line by line: broken, dnd
// (awaited), hush (awaited), screenshot, theme, hush (mute), dnd (heeded
// "on"), concluded.

// after is a flow that has begun and heard the first n lines of the vigil.
func after(t *testing.T, n int) *Flow {
	t.Helper()
	f := &Flow{}
	f.Begin()
	for _, h := range recorded(t, "vigil.jsonl")[:n] {
		f.Told(Told{Herald: h})
	}
	return f
}

func said(t *testing.T, acts []Act) []provider.Message {
	t.Helper()
	var out []provider.Message
	for _, a := range acts {
		s, ok := a.(Say)
		if !ok {
			t.Fatalf("not a line: %#v", a)
		}
		out = append(out, s.Message)
	}
	return out
}

func oneStage(t *testing.T, acts []Act) provider.Stage {
	t.Helper()
	m := said(t, acts)
	if len(m) != 1 {
		t.Fatalf("not one stage: %#v", m)
	}
	s, ok := m[0].(provider.Stage)
	if !ok {
		t.Fatalf("not one stage: %#v", m)
	}
	return s
}

func items(t *testing.T, acts []Act) []provider.Item {
	t.Helper()
	m := said(t, acts)
	if len(m) != 1 {
		t.Fatalf("not items: %#v", m)
	}
	it, ok := m[0].(provider.Items)
	if !ok || it.Select != nil {
		t.Fatalf("not items: %#v", m)
	}
	return it.Items
}

func activate(f *Flow, stage, id string) []Act {
	return f.Heard(provider.Activate{Stage: stage, ID: id})
}

func submit(f *Flow, stage, text string) []Act {
	return f.Heard(provider.Submit{Stage: stage, Text: text})
}

func invoked(t *testing.T, acts []Act) []string {
	t.Helper()
	if len(acts) != 1 {
		t.Fatalf("no invocation: %#v", acts)
	}
	i, ok := acts[0].(Invoke)
	if !ok {
		t.Fatalf("no invocation: %#v", acts)
	}
	return i.Args
}

func isReady(acts []Act) bool {
	return len(acts) == 1 && reflect.DeepEqual(acts[0], Say{provider.Ready{}})
}

func errorToastThenReady(t *testing.T, acts []Act) string {
	t.Helper()
	m := said(t, acts)
	if len(m) == 2 {
		if toast, ok := m[0].(provider.Toast); ok && toast.Level == list.Error && m[1] == (provider.Ready{}) {
			return toast.Text
		}
	}
	t.Fatalf("not an error toast and ready: %#v", m)
	return ""
}

func want[T comparable](t *testing.T, got, want T, why string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: %v, want %v", why, got, want)
	}
}

func TestItGreetsAndShowsTheRiteStageAtOnceBeforeTheCensusSpeaks(t *testing.T) {
	first := said(t, (&Flow{}).Begin())
	if len(first) != 2 || first[0] != (provider.Hello{Protocol: 1}) {
		t.Fatalf("%#v", first)
	}
	stage, ok := first[1].(provider.Stage)
	if !ok || stage.ID != "rite" || stage.Input != provider.InputNone || len(stage.Items) != 0 || stage.Select != nil || stage.Placeholder == "" {
		t.Fatalf("%#v", first[1])
	}
}

func TestEveryRiteAppearsAsItIsToldAndAnAwaitedOneIsPending(t *testing.T) {
	f := after(t, 0)
	var told []provider.Item
	for _, h := range recorded(t, "vigil.jsonl")[:5] {
		told = append(told, items(t, f.Told(Told{Herald: h}))...)
	}
	var ids []string
	var pending []bool
	for _, i := range told {
		ids, pending = append(ids, i.ID), append(pending, i.Pending)
	}
	if !reflect.DeepEqual(ids, []string{"broken", "dnd", "hush", "screenshot", "theme"}) ||
		!reflect.DeepEqual(pending, []bool{false, true, true, false, false}) {
		t.Fatalf("%q %v", ids, pending)
	}
	if !reflect.DeepEqual(told[1].Columns, []string{"dnd", "·", "Silence the vox of the desktop"}) {
		t.Fatalf("%q", told[1].Columns)
	}
	if !reflect.DeepEqual(told[0].Badges, []string{"heresy"}) || !reflect.DeepEqual(told[3].Badges, []string{"edict"}) || len(told[4].Badges) != 0 {
		t.Fatalf("%q %q %q", told[0].Badges, told[3].Badges, told[4].Badges)
	}
	if *told[0].Glyph != "⚙" || *told[3].Glyph != "†" {
		t.Fatalf("%q %q", *told[0].Glyph, *told[3].Glyph)
	}
}

func TestASettlingLineReplacesItsRiteSettledWithItsAspect(t *testing.T) {
	f := after(t, 6)
	settled := items(t, f.Told(Told{Herald: recorded(t, "vigil.jsonl")[6]}))
	if len(settled) != 1 || settled[0].ID != "dnd" || settled[0].Pending || settled[0].Columns[1] != "on" {
		t.Fatalf("%#v", settled)
	}
}

func TestAPendingRiteIsNeverTaken(t *testing.T) {
	f := after(t, 5)
	for _, r := range []string{"dnd", "hush"} {
		if acts := activate(f, "rite", r); !isReady(acts) {
			t.Fatalf("%s: %#v", r, acts)
		}
	}
}

func TestASettledRiteOffersItsAspectsWithTheCurrentMarkedAndSelected(t *testing.T) {
	f := after(t, 7)
	stage := oneStage(t, activate(f, "rite", "dnd"))
	if stage.ID != "aspect" || stage.Input != provider.InputNone || len(stage.Items) != 2 ||
		stage.Items[0].ID != "on" || stage.Items[1].ID != "off" {
		t.Fatalf("%#v", stage)
	}
	if !reflect.DeepEqual(stage.Items[0].Badges, []string{"current"}) || len(stage.Items[1].Badges) != 0 {
		t.Fatalf("%#v", stage.Items)
	}
	if stage.Select == nil || *stage.Select != "on" {
		t.Fatalf("select %v", stage.Select)
	}
}

func TestAStaleOrUnknownAspectIsNeverMarkedOrSelected(t *testing.T) {
	f := after(t, 0)
	// A census whose auspex was forgone: its aspect is the data-slate's word.
	f.Told(Told{Herald: mustParse(t, `{"herald":"rite","rite":"dnd","edict":false,"aspect":"off","standing":"performed","auspex":"forgone","aspects":["on","off"],"canon":[]}`)})
	unmarked := func(stage provider.Stage) {
		t.Helper()
		if stage.Select != nil {
			t.Fatalf("select %q", *stage.Select)
		}
		for _, i := range stage.Items {
			if len(i.Badges) != 0 {
				t.Fatalf("%#v", i)
			}
		}
	}
	unmarked(oneStage(t, activate(f, "rite", "dnd")))
	// hush settled mute and stands in no aspect: nothing is marked either.
	unmarked(oneStage(t, activate(after(t, 7), "rite", "hush")))
}

func TestInscriptionsAreAskedInWrittenOrderFromTheCanon(t *testing.T) {
	f := after(t, 7)
	oneStage(t, activate(f, "rite", "dnd"))
	reason := oneStage(t, activate(f, "aspect", "off"))
	want(t, reason.ID, "inscription:reason", "id")
	want(t, reason.Placeholder, "why the silence was imposed", "placeholder")
	want(t, reason.Query, "", "no decree to prefill")
	want(t, reason.Input, provider.InputRequired, "mandatory without a decree")
	if !strings.Contains(*reason.Prompt, "reason") {
		t.Fatalf("%q", *reason.Prompt)
	}
	origin := oneStage(t, submit(f, "inscription:reason", "the forge demands focus"))
	want(t, origin.ID, "inscription:origin", "id")
	want(t, origin.Query, "the cogitator", "the decree of the chosen aspect")
	want(t, origin.Input, provider.InputOptional, "input")
	if origin.Placeholder == "" {
		t.Fatal("a purpose of its own when unwritten")
	}
	argv := invoked(t, submit(f, "inscription:origin", "the cogitator"))
	if w := []string{"invoke", "dnd", "off", "--reason", "the forge demands focus", "--origin", "the cogitator"}; !reflect.DeepEqual(argv, w) {
		t.Fatalf("%q", argv)
	}
}

func TestTheDecreePrefilledFollowsTheAspectChosen(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "dnd")
	activate(f, "aspect", "on")
	want(t, oneStage(t, submit(f, "inscription:reason", "r")).Query, "the hotkey", "query")
}

func TestAMandatoryInscriptionMustBeFilledBeforeInvoking(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "dnd")
	activate(f, "aspect", "on")
	if text := errorToastThenReady(t, submit(f, "inscription:reason", "")); !strings.Contains(text, "reason") {
		t.Fatalf("%q", text)
	}
	// Still asked: a word now leads on.
	want(t, oneStage(t, submit(f, "inscription:reason", "now")).ID, "inscription:origin", "next")
}

func TestAClearedDecreeIsPassedAndAnEmptyWordWithoutOneIsLeftOut(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "dnd")
	activate(f, "aspect", "on")
	submit(f, "inscription:reason", "r")
	if argv := invoked(t, submit(f, "inscription:origin", "")); !reflect.DeepEqual(argv, []string{"invoke", "dnd", "on", "--reason", "r", "--origin", ""}) {
		t.Fatalf("%q", argv)
	}
	f = after(t, 7)
	activate(f, "rite", "theme")
	want(t, oneStage(t, activate(f, "aspect", "porpl")).Input, provider.InputOptional, "input")
	if argv := invoked(t, submit(f, "inscription:reason", "")); !reflect.DeepEqual(argv, []string{"invoke", "theme", "porpl"}) {
		t.Fatalf("%q", argv)
	}
}

func TestAnEdictNeedsNoAspectAndItsDecreeIsPrefilled(t *testing.T) {
	f := after(t, 7)
	region := oneStage(t, activate(f, "rite", "screenshot"))
	want(t, region.ID, "inscription:region", "id")
	want(t, region.Query, "chosen", "query")
	if argv := invoked(t, submit(f, "inscription:region", "whole")); !reflect.DeepEqual(argv, []string{"invoke", "screenshot", "--region", "whole"}) {
		t.Fatalf("%q", argv)
	}
}

func TestARiteWithoutInscriptionsIsInvokedOnceItsAspectIsChosen(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "hush")
	if argv := invoked(t, activate(f, "aspect", "quiet")); !reflect.DeepEqual(argv, []string{"invoke", "hush", "quiet"}) {
		t.Fatalf("%q", argv)
	}
}

func TestOnceInvokeIsLaunchedTheFlowClosesWithoutAToast(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "hush")
	invoked(t, activate(f, "aspect", "quiet"))
	if acts := f.Launched(nil); !reflect.DeepEqual(acts, []Act{Say{provider.Close{}}, Exit{0}}) {
		t.Fatalf("%#v", acts)
	}
}

func TestALaunchThatFailsIsToldAndTheLastStageAnswersAgain(t *testing.T) {
	f := after(t, 7)
	activate(f, "rite", "screenshot")
	argv := invoked(t, submit(f, "inscription:region", "whole"))
	if text := errorToastThenReady(t, f.Launched(errors.New("no such file or directory"))); !strings.Contains(text, "no such file") {
		t.Fatalf("%q", text)
	}
	if again := invoked(t, submit(f, "inscription:region", "whole")); !reflect.DeepEqual(again, argv) {
		t.Fatalf("%q", again)
	}
}

func TestAHereticalRiteIsRefusedInPlace(t *testing.T) {
	if text := errorToastThenReady(t, activate(after(t, 7), "rite", "broken")); !strings.Contains(text, "broken") {
		t.Fatalf("%q", text)
	}
}

func TestAnswersOutOfTurnOnlyFreeTheStage(t *testing.T) {
	f := after(t, 7)
	for i, acts := range [][]Act{activate(f, "rite", "nosuch"), activate(f, "aspect", "on"), submit(f, "rite", "dnd")} {
		if !isReady(acts) {
			t.Fatalf("%d: %#v", i, acts)
		}
	}
	activate(f, "rite", "hush")
	for i, acts := range [][]Act{activate(f, "aspect", "sideways"), activate(f, "rite", "hush")} {
		if !isReady(acts) {
			t.Fatalf("%d: %#v", i, acts)
		}
	}
}

func TestLaterCensusLinesLeaveALaterStageAloneUnlessTheyTellItsRite(t *testing.T) {
	f := after(t, 1)
	lines := recorded(t, "vigil.jsonl")
	f.Told(Told{Herald: lines[2]}) // hush, awaited
	f.Told(Told{Herald: lines[3]}) // screenshot
	// dnd as the forgone census would tell it: chosen while not yet heard.
	f.Told(Told{Herald: mustParse(t, `{"herald":"rite","rite":"dnd","edict":false,"aspect":"","standing":"dormant","auspex":"","aspects":["on","off"],"canon":[]}`)})
	oneStage(t, activate(f, "rite", "dnd"))
	if acts := f.Told(Told{Herald: lines[4]}); len(acts) != 0 {
		t.Fatalf("theme: %#v", acts)
	}
	if acts := f.Told(Told{Herald: lines[5]}); len(acts) != 0 {
		t.Fatalf("hush: %#v", acts)
	}
	// dnd itself is told again, settled in "on": it is marked and selected.
	m := said(t, f.Told(Told{Herald: lines[6]}))
	if len(m) != 1 {
		t.Fatalf("%#v", m)
	}
	it, ok := m[0].(provider.Items)
	if !ok || it.Select == nil || *it.Select != "on" || !reflect.DeepEqual(it.Items[0].Badges, []string{"current"}) {
		t.Fatalf("%#v", m)
	}
}

func TestTheCensussOwnTroublesAreToldInTheWindow(t *testing.T) {
	errorToast := func(acts []Act, part string) {
		t.Helper()
		m := said(t, acts)
		if len(m) != 1 {
			t.Fatalf("%#v", m)
		}
		if toast, ok := m[0].(provider.Toast); !ok || toast.Level != list.Error || !strings.Contains(toast.Text, part) {
			t.Fatalf("%#v", m)
		}
	}
	errorToast(after(t, 0).Unheard(errors.New("no such file or directory")), "no such file")
	errorToast(after(t, 3).Told(Told{Ended: true, Status: "exit status: 2"}), "exit status: 2")
	f := after(t, 0)
	f.Told(Told{Herald: Concluded{}})
	m := said(t, f.Told(Told{Ended: true, Status: "exit status: 0"}))
	if len(m) != 1 {
		t.Fatalf("an empty Librarium is told: %#v", m)
	}
	if tt, ok := m[0].(provider.Toast); !ok || tt.Level != list.Info || tt.Text != noRites || *tt.Seconds != 6 {
		t.Fatalf("an empty Librarium is told: %#v", m)
	}
	if acts := after(t, 8).Told(Told{Ended: true, Status: "exit status: 0"}); len(acts) != 0 {
		t.Fatalf("%#v", acts)
	}
}

func TestLamentsAndUnknownHeraldsChangeNothingShown(t *testing.T) {
	var log strings.Builder
	f := after(t, 2)
	f.Log = &log
	if acts := f.Told(Told{Herald: Lament{Lament: "the data-slate is garbled", Rite: "dnd"}}); len(acts) != 0 {
		t.Fatalf("%#v", acts)
	}
	if acts := f.Told(Told{Herald: UnknownHerald{}}); len(acts) != 0 {
		t.Fatalf("%#v", acts)
	}
	want(t, log.String(), "logis servitor: the census laments (dnd): the data-slate is garbled\n", "log")
}

func TestCancelEndsTheProviderAndAForeignProtocolIsAnError(t *testing.T) {
	if acts := after(t, 7).Heard(provider.Cancel{}); !reflect.DeepEqual(acts, []Act{Exit{0}}) {
		t.Fatalf("%#v", acts)
	}
	f := after(t, 0)
	if acts := f.Heard(provider.Hello{Protocol: 1}); len(acts) != 0 {
		t.Fatalf("%#v", acts)
	}
	if acts := f.Heard(provider.Hello{Protocol: 2}); !reflect.DeepEqual(acts, []Act{Exit{2}}) {
		t.Fatalf("%#v", acts)
	}
}

func TestNothingTheFlowSaysIsAnOutcomeToast(t *testing.T) {
	// Through a whole flow to its close, no toast is ever said: servitor
	// announces the verdict itself.
	f := &Flow{}
	all := f.Begin()
	for _, h := range recorded(t, "vigil.jsonl") {
		all = append(all, f.Told(Told{Herald: h})...)
	}
	all = append(all, activate(f, "rite", "dnd")...)
	all = append(all, activate(f, "aspect", "on")...)
	all = append(all, submit(f, "inscription:reason", "r")...)
	all = append(all, submit(f, "inscription:origin", "the hotkey")...)
	all = append(all, f.Launched(nil)...)
	for _, a := range all {
		if s, ok := a.(Say); ok {
			if _, toast := s.Message.(provider.Toast); toast {
				t.Fatalf("%#v", all)
			}
		}
	}
	if len(all) == 0 || all[len(all)-1] != (Exit{0}) {
		t.Fatalf("the flow never closed: %#v", all)
	}
}
