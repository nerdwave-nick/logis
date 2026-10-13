package cogitator

import (
	"slices"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
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

// Provider sessions in the window (Rust src/daemon/window/ui.rs): after an
// answer the window stays and Return waits for the provider's next stage or
// ready; a next stage keeps the window's size; toasts come and go; a
// session over lingers on its toast without the keyboard.

func (r *rig) showProvider(id daemon.SessionID, o list.Options, e ...list.Entry) {
	r.ui.show(daemon.Session{ID: id, Mode: protocol.ModeProvider, Options: o, Entries: e})
}

func (r *rig) wantAnswers(t *testing.T, want ...daemon.Answer) {
	t.Helper()
	var got []daemon.Answer
	for _, a := range r.host.answers {
		got = append(got, a.answer)
	}
	r.host.answers = nil
	if !slices.Equal(got, want) {
		t.Fatalf("answers %+v, want %+v", got, want)
	}
}

func lines(n uint32) *uint32 { return &n }

// Ported from Rust a_later_stage_keeps_the_rows_the_window_has_room_for.
func TestALaterStageKeepsTheRowsTheWindowHasRoomFor(t *testing.T) {
	r := newRig()
	first := list.DefaultOptions()
	first.Lines, first.Width = lines(6), lines(44)
	r.showProvider(1, first, list.Entry{Text: "oil"})
	r.act(input.ActAccept)
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerItem})
	next := list.DefaultOptions()
	next.Lines, next.Prompt = lines(15), "aspect: "
	r.events = nil
	r.ui.step(1, provider.Step{Kind: provider.StepStage, Options: next, Entries: entries("on", "off")})
	look := r.ui.look()
	if look.Lines != 6 || look.Prompt != "aspect: " {
		t.Fatalf("%d lines, prompt %q", look.Lines, look.Prompt)
	}
	if r.surf.shows != 1 {
		t.Fatal("a next stage opened the window anew")
	}
	r.saw(t, "stage 1", "items 2")
	r.act(input.ActAccept) // a new stage takes Return again
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerItem})
}

func TestAfterAnAnswerReturnWaitsForTheProviderAndEscapeStillDismisses(t *testing.T) {
	r := newRig()
	r.showProvider(2, list.DefaultOptions(), entries("a", "b")...)
	r.act(input.ActNext, input.ActAccept)
	r.saw(t, "accept 1")
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerItem, Index: 1})
	if !r.surf.shown || r.ui.shown == nil {
		t.Fatal("the window went after a provider's answer")
	}
	r.act(input.ActAccept, input.ActDescend)
	r.wantAnswers(t)
	r.ui.step(2, provider.Step{Kind: provider.StepReady})
	r.saw(t, "ready")
	r.act(input.ActDescend)
	r.saw(t, "descend 1")
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerDescend, Index: 1})
	r.act(input.ActDismiss)
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerDismissed})
	if r.surf.shown || r.ui.shown != nil {
		t.Fatal("dismissal left the window")
	}
}

func TestUpdatesRemovalsAndPreselectionReachTheList(t *testing.T) {
	r := newRig()
	pending := list.Entry{Text: "oil", Decor: &list.Decor{Pending: true}}
	r.showProvider(1, list.DefaultOptions(), pending, list.Entry{Text: "fire"})
	r.act(input.ActAccept)
	r.wantAnswers(t) // a pending row is never accepted
	before := r.surf.redraws
	r.ui.step(1, provider.Step{Kind: provider.StepUpdate, Updates: []provider.Update{{Index: 0, Entry: list.Entry{Text: "oil"}}}})
	r.saw(t, "update 0")
	if r.surf.redraws == before {
		t.Fatal("no frame for the update")
	}
	r.ui.step(1, provider.Step{Kind: provider.StepPreselect, Index: 1})
	if item, _ := r.ui.shown.view.Selected(); item != 1 {
		t.Fatalf("selected %d", item)
	}
	r.ui.step(1, provider.Step{Kind: provider.StepRemove, Removed: []uint32{1}})
	r.saw(t, "remove 1")
	r.act(input.ActAccept)
	r.wantAnswers(t, daemon.Answer{Kind: daemon.AnswerItem, Index: 0})
}

func TestAProvidersToastShowsUntilItsTimeIsUp(t *testing.T) {
	r := newRig()
	r.showProvider(1, list.DefaultOptions(), entries("a")...)
	notice := list.Notice{Text: "the rite is begun", Level: list.Success}
	r.ui.step(1, provider.Step{Kind: provider.StepToast, Notice: notice, Span: 8 * time.Second})
	r.saw(t, `toast "the rite is begun"`)
	if l := r.ui.look(); l.Toast == nil || *l.Toast != notice {
		t.Fatalf("%+v", l.Toast)
	}
	if _, ok := r.ui.deadline(); ok {
		t.Fatal("the window times a provider's toast: the daemon does")
	}
	r.ui.untoast(2) // another session's
	if r.ui.look().Toast == nil {
		t.Fatal("untoasted for another session")
	}
	r.ui.untoast(1)
	r.saw(t, "untoast")
	if r.ui.look().Toast != nil {
		t.Fatal("still toasting")
	}
}

func TestALingeringSessionKeepsItsToastWithoutTheKeyboard(t *testing.T) {
	r := newRig()
	r.showProvider(1, list.DefaultOptions(), entries("a")...)
	r.ui.step(1, provider.Step{Kind: provider.StepToast, Notice: list.Notice{Text: "done"}, Span: time.Second})
	r.ui.linger(2) // another session's
	if r.surf.released {
		t.Fatal("released for another session")
	}
	r.ui.linger(1)
	r.saw(t, "linger 1")
	if !r.surf.released || !r.surf.shown || r.ui.look().Toast == nil {
		t.Fatalf("released %v, shown %v", r.surf.released, r.surf.shown)
	}
	r.act(input.ActDismiss, input.ActAccept)
	r.ui.step(1, provider.Step{Kind: provider.StepAppend, Entries: entries("late")})
	r.ui.toast(daemon.Toast{Kind: daemon.ToastConfigRejected, Error: "/c:1:1: why"})
	r.surf.shown = false // the compositor closed it meanwhile
	r.ui.check()
	r.wantAnswers(t)
	if slices.Contains(r.events, "items 2") || slices.ContainsFunc(r.events, func(e string) bool { return e == `toast "the config is heretical: /c:1:1: why"` }) {
		t.Fatalf("a lingering session took steps: %q", r.events)
	}
	r.surf.shown = true
	r.showProvider(3, list.DefaultOptions(), entries("next")...)
	r.ui.hide(1) // the linger's end, after the next session came
	if r.ui.shown == nil || r.ui.shown.id != 3 {
		t.Fatal("the linger's end took the next session away")
	}
}

func TestALingeringSessionGoesAtItsHide(t *testing.T) {
	r := newRig()
	r.showProvider(1, list.DefaultOptions(), entries("a")...)
	r.ui.linger(1)
	r.ui.hide(1)
	r.saw(t, "hide 1")
	if r.surf.shown || r.ui.shown != nil {
		t.Fatal("still shown")
	}
}

// fakeIcons find the icons they name.
type fakeIcons map[string]render.ImageID

func (f fakeIcons) Image(name string, _ uint32) (render.ImageID, bool) {
	id, ok := f[name]
	return id, ok
}

func TestARowsIconIsDrawnInPlaceOfItsGlyphAndAMissingOneShowsTheGlyph(t *testing.T) {
	r := newRig()
	var drawn *render.Scene
	r.ui.draw = func(sc *render.Scene, _ window.Geometry) { drawn = sc }
	r.ui.icons = func(window.Geometry) scene.Icons { return fakeIcons{"forge-cog": 7} }
	found := list.Entry{Text: "cog", Decor: &list.Decor{Icon: "forge-cog", HasIcon: true, Glyph: "⚙", HasGlyph: true}}
	lost := list.Entry{Text: "lost", Decor: &list.Decor{Icon: "nowhere", HasIcon: true, Glyph: "†", HasGlyph: true}}
	r.showProvider(1, list.DefaultOptions(), found, lost)
	r.surf.paint(window.Geometry{Logical: r.surf.size, Physical: r.surf.size, Scale: scale.From120ths(120)})
	if drawn == nil || len(drawn.Images) != 1 || drawn.Images[0].Image != 7 {
		t.Fatalf("images %+v", drawn.Images)
	}
	var texts []string
	for _, x := range drawn.Texts {
		texts = append(texts, x.Text)
	}
	if !slices.Contains(texts, "†") || slices.Contains(texts, "⚙") {
		t.Fatalf("%q", texts)
	}
}

func TestTheConfigsIconThemeIsLoadedWhenItChanges(t *testing.T) {
	r := newRig()
	var loaded []string
	r.ui.iconTheme = func(name string) { loaded = append(loaded, name) }
	c := config.Default()
	r.ui.configure(c)
	r.ui.configure(c)
	c.IconTheme = "Papirus"
	r.ui.configure(c)
	if !slices.Equal(loaded, []string{"hicolor", "Papirus"}) {
		t.Fatalf("%q", loaded)
	}
}
