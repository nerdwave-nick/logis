package cogitator

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The window's session logic (Rust src/daemon/window/ui.rs), driven
// without a compositor: a fake surface, a recording host and trace.

type fakeSurface struct {
	shown    bool
	size     scale.Size
	paint    window.Painter
	redraws  int
	fail     error
	shows    int
	released bool
	// unconfigured: the compositor has not sized the panel yet.
	unconfigured bool
	pastes       []input.Selection
	// asked are the apps tokens were asked for; token answers them.
	asked []string
	token string
}

func (s *fakeSurface) Show(size scale.Size, paint window.Painter) error {
	if s.fail != nil {
		return s.fail
	}
	s.shown, s.size, s.paint, s.released = true, size, paint, false
	s.shows++
	return nil
}
func (s *fakeSurface) Hide()            { s.shown = false }
func (s *fakeSurface) ReleaseKeyboard() { s.released = true }
func (s *fakeSurface) Visible() bool    { return s.shown }
func (s *fakeSurface) RequestRedraw()   { s.redraws++ }
func (s *fakeSurface) Size() (scale.Size, bool) {
	return s.size, s.shown && !s.unconfigured
}
func (s *fakeSurface) Paste(sel input.Selection) { s.pastes = append(s.pastes, sel) }
func (s *fakeSurface) RequestToken(appID string, done func(string)) {
	s.asked = append(s.asked, appID)
	done(s.token)
}

type answered struct {
	id     daemon.SessionID
	answer daemon.Answer
}

type fakeHost struct {
	answers []answered
	fails   []string
	tokens  []string
}

func (h *fakeHost) Answer(id daemon.SessionID, a daemon.Answer) {
	h.answers = append(h.answers, answered{id, a})
}
func (h *fakeHost) Fail(id daemon.SessionID, message string) { h.fails = append(h.fails, message) }
func (h *fakeHost) Exit(int, string)                         {}
func (h *fakeHost) Activated(id daemon.SessionID, token string) {
	h.tokens = append(h.tokens, fmt.Sprintf("%d %s", id, token))
}

type fakeKeyboard struct{ policy config.FocusLoss }

func (k *fakeKeyboard) SetFocusLoss(p config.FocusLoss) { k.policy = p }

type rig struct {
	ui     *ui
	surf   *fakeSurface
	host   *fakeHost
	kb     *fakeKeyboard
	events []string
}

func newRig() *rig {
	r := &rig{surf: &fakeSurface{}, host: &fakeHost{}, kb: &fakeKeyboard{}}
	th := theme.Default()
	r.ui = newUI(r.host, r.surf, &th, r.kb)
	r.ui.emit = func(format string, args ...any) { r.events = append(r.events, fmt.Sprintf(format, args...)) }
	return r
}

func entries(texts ...string) []list.Entry {
	out := make([]list.Entry, len(texts))
	for i, t := range texts {
		out[i] = list.Entry{Text: t}
	}
	return out
}

func (r *rig) show(id daemon.SessionID, options list.Options, texts ...string) {
	r.ui.show(daemon.Session{ID: id, Mode: protocol.ModeDmenu, Options: options, Entries: entries(texts...)})
}

func (r *rig) appendItems(id daemon.SessionID, texts ...string) {
	r.ui.step(id, provider.Step{Kind: provider.StepAppend, Entries: entries(texts...)})
}

func (r *rig) act(actions ...input.Action) {
	for _, a := range actions {
		r.ui.act(a)
	}
}

func (r *rig) saw(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(r.events, w) {
			t.Fatalf("no %q in %q", w, r.events)
		}
	}
}

func TestAShownSessionIsSizedByItsOptionsAndReportsItsItems(t *testing.T) {
	r := newRig()
	o := list.DefaultOptions()
	lines, width := uint32(4), uint32(40)
	o.Lines, o.Width = &lines, &width
	r.show(7, o, "alpha", "beta")
	th := theme.Default()
	want, _ := scene.Fit(o, &th)
	if !r.surf.shown || r.surf.size != want {
		t.Fatalf("shown %v at %v, want %v", r.surf.shown, r.surf.size, want)
	}
	r.saw(t, "show 7", "items 2")
	r.appendItems(7, "gamma")
	r.saw(t, "items 3")
	r.appendItems(8, "not mine")
	if r.events[len(r.events)-1] != "items 3" {
		t.Fatalf("%q", r.events)
	}
}

func TestWithoutASizeTheWindowIsTheThemesAndShowsItsRows(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "a")
	th := theme.Default()
	want, rows := scene.Fit(list.DefaultOptions(), &th)
	if r.surf.size != want || r.ui.shown.view.Look().Lines != rows {
		t.Fatalf("%v, %d rows", r.surf.size, r.ui.shown.view.Look().Lines)
	}
}

func TestTypingSelectsAndReturnAnswersWithTheItemAndClosesTheWindow(t *testing.T) {
	r := newRig()
	r.show(3, list.DefaultOptions(), "alpha", "beta", "gamma")
	r.act(input.Insert("gam"))
	r.saw(t, `query "gam"`, "select 2")
	r.act(input.ActAccept)
	r.saw(t, "accept 2")
	if len(r.host.answers) != 1 || r.host.answers[0] != (answered{3, daemon.Answer{Kind: daemon.AnswerItem, Index: 2}}) {
		t.Fatalf("%+v", r.host.answers)
	}
	if r.surf.shown || r.ui.shown != nil {
		t.Fatal("the window stays after a dmenu answer")
	}
	r.act(input.ActAccept) // nothing shown: nothing answered
	if len(r.host.answers) != 1 {
		t.Fatalf("%+v", r.host.answers)
	}
}

func TestReturnWithoutAMatchAnswersWithTheTypedText(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "alpha")
	r.act(input.Insert("Free Rite"))
	r.saw(t, "select none")
	r.act(input.ActAccept)
	r.saw(t, `typed "Free Rite"`)
	if r.host.answers[0].answer != (daemon.Answer{Kind: daemon.AnswerTyped, Text: "Free Rite"}) {
		t.Fatalf("%+v", r.host.answers)
	}
}

func TestEscapeDismisses(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "alpha")
	r.act(input.ActDismiss)
	r.saw(t, "dismiss")
	if r.host.answers[0].answer.Kind != daemon.AnswerDismissed || r.surf.shown {
		t.Fatalf("%+v", r.host.answers)
	}
}

func TestOnlyMatchRefusesFreeText(t *testing.T) {
	r := newRig()
	o := list.DefaultOptions()
	o.OnlyMatch = true
	r.show(1, o, "alpha")
	r.act(input.Insert("zz"), input.ActAccept)
	if len(r.host.answers) != 0 || !r.surf.shown {
		t.Fatalf("%+v", r.host.answers)
	}
	r.act(input.EditAction(input.Edit{Kind: input.Clear}), input.ActAccept)
	r.saw(t, `query ""`, "accept 0")
}

func TestAChangeAsksForAFrameAndNothingElseDoes(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "a", "b")
	before := r.surf.redraws
	r.act(input.ActPrevious) // already at the top
	if r.surf.redraws != before {
		t.Fatal("a frame for nothing")
	}
	r.act(input.ActNext)
	if r.surf.redraws != before+1 {
		t.Fatalf("%d redraws", r.surf.redraws-before)
	}
	r.saw(t, "select 1")
}

func TestStreamedItemsAreDrawnAsTheyCome(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "first")
	before := r.surf.redraws
	r.appendItems(1, "second", "third")
	if r.surf.redraws == before {
		t.Fatal("no frame for the new items")
	}
	r.act(input.ActNext, input.ActNext, input.ActAccept)
	if r.host.answers[0].answer.Index != 2 {
		t.Fatalf("%+v", r.host.answers)
	}
}

func TestHideTakesTheSessionAway(t *testing.T) {
	r := newRig()
	r.show(4, list.DefaultOptions(), "a")
	r.ui.hide(5) // another session's
	if !r.surf.shown {
		t.Fatal("hidden for another session")
	}
	r.ui.hide(4)
	r.saw(t, "hide 4")
	if r.surf.shown || r.ui.shown != nil {
		t.Fatal("still shown")
	}
	r.act(input.ActDismiss)
	if len(r.host.answers) != 0 {
		t.Fatal("answered a hidden session")
	}
}

func TestASurfaceTheCompositorClosedDismissesItsSession(t *testing.T) {
	r := newRig()
	r.show(2, list.DefaultOptions(), "a")
	r.surf.shown = false // the compositor sent closed
	r.ui.check()
	if len(r.host.answers) != 1 || r.host.answers[0] != (answered{2, daemon.Answer{Kind: daemon.AnswerDismissed}}) {
		t.Fatalf("%+v", r.host.answers)
	}
	r.ui.check()
	if len(r.host.answers) != 1 {
		t.Fatal("dismissed twice")
	}
}

func TestASessionThatCannotBeShownFails(t *testing.T) {
	r := newRig()
	r.surf.fail = &window.SurfaceError{Reason: "no glass"}
	r.show(2, list.DefaultOptions())
	if len(r.host.fails) != 1 || !strings.Contains(r.host.fails[0], "no glass") || r.ui.shown != nil {
		t.Fatalf("%+v", r.host.fails)
	}
}

func TestAToastShowsOverTheVisibleListForAWhile(t *testing.T) {
	r := newRig()
	now := time.Unix(1000, 0)
	r.ui.now = func() time.Time { return now }
	r.ui.toast(daemon.Toast{Kind: daemon.ToastConfigRejected, Error: "/c:1:2: why"})
	if len(r.events) != 0 {
		t.Fatalf("a toast without a window: %q", r.events)
	}
	r.show(1, list.DefaultOptions(), "a")
	r.ui.toast(daemon.Toast{Kind: daemon.ToastConfigRejected, Error: "/c:1:2: why"})
	r.saw(t, `toast "the config is heretical: /c:1:2: why"`)
	if l := r.ui.look(); l.Toast == nil || l.Toast.Level != list.Error {
		t.Fatalf("%+v", l.Toast)
	}
	until, ok := r.ui.deadline()
	if !ok || until != now.Add(noticeFor) {
		t.Fatalf("%v %v", until, ok)
	}
	r.ui.expire()
	if r.ui.look().Toast == nil {
		t.Fatal("expired early")
	}
	now = now.Add(noticeFor)
	r.ui.expire()
	r.saw(t, "untoast")
	if r.ui.look().Toast != nil {
		t.Fatal("still toasting")
	}
	if _, ok := r.ui.deadline(); ok {
		t.Fatal("a deadline without a toast")
	}
}

// A daemon whose binary was replaced reminds the user in its own words, as
// a notice, not an error (feat/logis-go 750bed66).
func TestTheStaleDaemonsReminderIsToastedAsANotice(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "a")
	const words = "logis updated — restart the daemon: kill 7"
	r.ui.toast(daemon.Toast{Kind: daemon.ToastStale, Text: words})
	r.saw(t, `toast "`+words+`"`)
	if l := r.ui.look(); l.Toast == nil || *l.Toast != (list.Notice{Text: words, Level: list.Info}) {
		t.Fatalf("%+v", l.Toast)
	}
}

func TestTheConfigsFocusLossReachesTheKeyboard(t *testing.T) {
	r := newRig()
	c := config.Default()
	c.FocusLoss = config.FocusKeep
	r.ui.configure(c)
	if r.kb.policy != config.FocusKeep {
		t.Fatalf("%v", r.kb.policy)
	}
}

func TestDmenuProviderAndAppSessionsAreServed(t *testing.T) {
	for _, m := range []protocol.Mode{protocol.ModeDmenu, protocol.ModeProvider, protocol.ModeApps} {
		if err := accepts(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := accepts(protocol.ModeServitor); err == nil {
		t.Fatal("servitor accepted")
	}
	if err := accepts(protocol.ModeServitor); !strings.Contains(err.Error(), "logis --servitor") {
		t.Fatal(err)
	}
}
