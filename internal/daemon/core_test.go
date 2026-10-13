package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// wire is a peer that records what it was sent, and whether it was closed.
type wire struct {
	replies []protocol.Reply
	closed  bool
}

func (w *wire) send(r protocol.Reply) { w.replies = append(w.replies, r) }
func (w *wire) close()                { w.closed = true }

func (w *wire) take() []protocol.Reply {
	r := w.replies
	w.replies = nil
	return r
}

// recorder records resolver calls as text.
type recorder struct {
	log    []string
	refuse string
}

func (r *recorder) take() []string {
	l := r.log
	r.log = nil
	return l
}

func texts(entries []list.Entry) string {
	var t []string
	for _, e := range entries {
		t = append(t, e.Text)
	}
	return strings.Join(t, ",")
}

func (r *recorder) Accepts(protocol.Mode) error {
	if r.refuse != "" {
		return errors.New(r.refuse)
	}
	return nil
}

func (r *recorder) note(format string, args ...any) {
	r.log = append(r.log, fmt.Sprintf(format, args...))
}

func (r *recorder) Show(s Session) {
	prompt := ""
	if s.Options.Prompt != list.DefaultOptions().Prompt {
		prompt = " '" + s.Options.Prompt + "'"
	}
	r.note("show %d %s%s [%s]", s.ID, s.Mode, prompt, texts(s.Entries))
}

func (r *recorder) Step(id SessionID, step provider.Step) {
	switch step.Kind {
	case provider.StepAppend:
		r.note("append %d [%s]", id, texts(step.Entries))
	case provider.StepStage:
		r.note("stage %d [%s]", id, texts(step.Entries))
	case provider.StepToast:
		r.note("toast %d %s", id, step.Notice.Text)
	case provider.StepUpdate:
		r.note("update %d %d", id, step.Updates[0].Index)
	default:
		r.note("step %d %d", id, step.Kind)
	}
}

func (r *recorder) End(id SessionID)     { r.note("end %d", id) }
func (r *recorder) Hide(id SessionID)    { r.note("hide %d", id) }
func (r *recorder) Linger(id SessionID)  { r.note("linger %d", id) }
func (r *recorder) Untoast(id SessionID) { r.note("untoast %d", id) }

const pid = 4242

func newTestCore() (*core, *recorder) {
	r := &recorder{}
	return newCore(r, pid), r
}

func itemsOf(texts ...string) protocol.Request {
	var items []protocol.Item
	for _, t := range texts {
		items = append(items, protocol.Item{Text: t, Raw: []byte(t)})
	}
	return protocol.Items(items)
}

// greet connects id and greets it; it returns its wire with the welcome taken.
func greet(t *testing.T, c *core, id SessionID) *wire {
	t.Helper()
	w := &wire{}
	c.connect(id, w)
	c.receive(id, protocol.Hello(), nil)
	if got := w.take(); !reflect.DeepEqual(got, []protocol.Reply{protocol.Welcome(pid)}) {
		t.Fatalf("greeting %d: %+v", id, got)
	}
	return w
}

func open(t *testing.T, c *core, id SessionID, mode protocol.Mode) *wire {
	t.Helper()
	w := greet(t, c, id)
	c.receive(id, protocol.Open(mode, nil), nil)
	return w
}

func result(kind protocol.OutcomeKind) protocol.Reply {
	return protocol.Result(protocol.Outcome{Kind: kind})
}

func wantReplies(t *testing.T, w *wire, want ...protocol.Reply) {
	t.Helper()
	if got := w.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("replies %+v, want %+v", got, want)
	}
}

func wantLog(t *testing.T, r *recorder, want ...string) {
	t.Helper()
	if got := r.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("resolver saw %q, want %q", got, want)
	}
}

// isError: the replies are exactly one error containing needle.
func isError(replies []protocol.Reply, needle string) bool {
	return len(replies) == 1 && replies[0].Type == protocol.TypeError &&
		strings.Contains(replies[0].Message, needle)
}

// Ported from Rust src/daemon/core.rs a_session_the_resolver_could_not_show_ends_with_its_error.
func TestASessionTheResolverCouldNotShowEndsWithItsError(t *testing.T) {
	c, r := newTestCore()
	failing := open(t, c, 1, protocol.ModeDmenu)
	waiting := open(t, c, 2, protocol.ModeDmenu)
	c.failed(2, "not visible yet")
	wantReplies(t, failing, protocol.Visible())
	c.failed(1, "the glass cracked")
	wantReplies(t, failing, protocol.Error("the glass cracked"))
	if !failing.closed {
		t.Error("the failed connection stays open")
	}
	wantReplies(t, waiting, protocol.Queued(), protocol.Visible())
	wantLog(t, r, "show 1 dmenu []", "hide 1", "show 2 dmenu []")
}

// Ported from the_options_a_client_opened_with_reach_the_resolver_when_shown.
func TestTheOptionsAClientOpenedWithReachTheResolverWhenShown(t *testing.T) {
	c, r := newTestCore()
	open(t, c, 1, protocol.ModeDmenu)
	greet(t, c, 2)
	c.receive(2, protocol.Open(protocol.ModeDmenu, json.RawMessage(`{"prompt":"rite? "}`)), nil)
	c.receive(2, itemsOf("b"), nil)
	wantLog(t, r, "show 1 dmenu []")
	c.receive(1, protocol.Cancel(), nil)
	wantLog(t, r, "hide 1", "show 2 dmenu 'rite? ' [b]")
}

// The list options are read when the session opens; malformed ones end it.
func TestADmenuSessionWithMalformedOptionsIsRefusedAtOpen(t *testing.T) {
	c, r := newTestCore()
	w := greet(t, c, 1)
	c.receive(1, protocol.Open(protocol.ModeDmenu, json.RawMessage(`{"password": "ab"}`)), nil)
	if replies := w.take(); !isError(replies, "password 'ab' is not one character") || !w.closed {
		t.Errorf("replies %+v, closed %v", replies, w.closed)
	}
	wantLog(t, r)
}

// Ported from a_greeting_of_another_version_is_refused.
func TestAGreetingOfAnotherVersionIsRefused(t *testing.T) {
	c, _ := newTestCore()
	w := &wire{}
	c.connect(1, w)
	c.receive(1, protocol.Request{Type: protocol.TypeHello, Version: 99}, nil)
	replies := w.take()
	if !isError(replies, "99") || !strings.Contains(replies[0].Message, "restart the daemon") {
		t.Errorf("replies %+v", replies)
	}
	if !w.closed || !c.idle() {
		t.Errorf("closed %v, idle %v", w.closed, c.idle())
	}
}

// Ported from the_first_session_is_shown_and_its_items_streamed_to_the_resolver.
func TestTheFirstSessionIsShownAndItsItemsStreamedToTheResolver(t *testing.T) {
	c, r := newTestCore()
	w := open(t, c, 1, protocol.ModeDmenu)
	c.receive(1, itemsOf("a", "b"), nil)
	c.receive(1, protocol.End(), nil)
	wantReplies(t, w, protocol.Visible())
	wantLog(t, r, "show 1 dmenu []", "append 1 [a,b]", "end 1")
}

// Ported from a_resolved_session_gets_its_result_and_the_next_queued_one_is_shown.
func TestAResolvedSessionGetsItsResultAndTheNextQueuedOneIsShown(t *testing.T) {
	c, r := newTestCore()
	first := open(t, c, 1, protocol.ModeDmenu)
	second := open(t, c, 2, protocol.ModeDmenu)
	c.receive(2, itemsOf("x"), nil)
	c.receive(2, protocol.End(), nil)
	wantReplies(t, second, protocol.Queued())
	wantLog(t, r, "show 1 dmenu []")

	c.answered(1, Answer{Kind: AnswerDismissed})
	wantReplies(t, first, protocol.Visible(), result(protocol.Dismissed))
	if !first.closed {
		t.Error("the answered connection stays open")
	}
	wantReplies(t, second, protocol.Visible())
	wantLog(t, r, "hide 1", "show 2 dmenu [x]", "end 2")
	if second.closed {
		t.Error("the shown connection was closed")
	}
}

// Ported from an_interactive_session_replaces_the_visible_one.
func TestAnInteractiveSessionReplacesTheVisibleOne(t *testing.T) {
	c, r := newTestCore()
	first := open(t, c, 1, protocol.ModeDmenu)
	second := open(t, c, 2, protocol.ModeApps)
	wantReplies(t, first, protocol.Visible(), result(protocol.Replaced))
	if !first.closed {
		t.Error("the replaced connection stays open")
	}
	wantReplies(t, second, protocol.Visible())
	wantLog(t, r, "show 1 dmenu []", "hide 1", "show 2 apps []")
}

// Ported from a_disconnect_cancels_the_session_and_shows_the_next.
func TestADisconnectCancelsTheSessionAndShowsTheNext(t *testing.T) {
	c, r := newTestCore()
	open(t, c, 1, protocol.ModeDmenu)
	second := open(t, c, 2, protocol.ModeDmenu)
	c.disconnect(1)
	wantReplies(t, second, protocol.Queued(), protocol.Visible())
	wantLog(t, r, "show 1 dmenu []", "hide 1", "show 2 dmenu []")
	c.answered(1, Answer{Kind: AnswerDismissed})
	wantLog(t, r) // a late answer for a gone session is ignored
}

// Ported from a_cancel_ends_the_session_with_cancelled.
func TestACancelEndsTheSessionWithCancelled(t *testing.T) {
	c, r := newTestCore()
	w := open(t, c, 1, protocol.ModeServitor)
	c.receive(1, protocol.Cancel(), nil)
	wantReplies(t, w, protocol.Visible(), result(protocol.Cancelled))
	if !w.closed || !c.idle() {
		t.Errorf("closed %v, idle %v", w.closed, c.idle())
	}
	wantLog(t, r, "show 1 servitor []", "hide 1")
}

// Ported from the_result_goes_only_to_the_session_that_was_answered.
func TestTheResultGoesOnlyToTheSessionThatWasAnswered(t *testing.T) {
	c, _ := newTestCore()
	first := open(t, c, 1, protocol.ModeDmenu)
	second := open(t, c, 2, protocol.ModeDmenu)
	c.receive(1, itemsOf("a"), nil)
	c.receive(1, itemsOf("b"), nil)
	c.answered(1, Answer{Kind: AnswerItem, Index: 1})
	chosen := protocol.Outcome{Kind: protocol.Selected, Index: 1,
		Item: protocol.Item{Text: "b", Raw: []byte("b")}}
	wantReplies(t, first, protocol.Visible(), protocol.Result(chosen))
	wantReplies(t, second, protocol.Queued(), protocol.Visible())
}

// Typed text goes back as typed; an item the session never had is no answer.
func TestTypedTextIsTheOutcomeAndAnUnknownItemIsIgnored(t *testing.T) {
	c, _ := newTestCore()
	w := open(t, c, 1, protocol.ModeDmenu)
	c.receive(1, itemsOf("a"), nil)
	c.answered(1, Answer{Kind: AnswerItem, Index: 1})
	c.answered(1, Answer{Kind: AnswerItem, Index: -1})
	wantReplies(t, w, protocol.Visible())
	c.answered(1, Answer{Kind: AnswerTyped, Text: "free"})
	wantReplies(t, w, protocol.Result(protocol.Outcome{Kind: protocol.Typed, Text: "free"}))
}

// Ported from protocol_violations_end_the_connection_with_an_error, plus the
// provider rules of Rust's Feed::new.
func TestProtocolViolationsEndTheConnectionWithAnError(t *testing.T) {
	rite := protocol.Open(protocol.ModeProvider, nil)
	rite.Provider = &protocol.Launch{Argv: [][]byte{[]byte("rite")}}
	dmenuWithProvider := protocol.Open(protocol.ModeDmenu, nil)
	dmenuWithProvider.Provider = rite.Provider
	for _, tc := range []struct {
		requests []protocol.Request
		needle   string
	}{
		{[]protocol.Request{itemsOf("a")}, "'items' is out of place: a session begins with 'open'"},
		{[]protocol.Request{protocol.Open(protocol.ModeApps, nil), itemsOf("a")}, "only dmenu sessions take items"},
		{[]protocol.Request{protocol.Open(protocol.ModeDmenu, nil), protocol.End(), itemsOf("a")}, "items arrived after end"},
		{[]protocol.Request{protocol.Open(protocol.ModeDmenu, nil), protocol.End(), protocol.End()}, "'end' is out of place"},
		{[]protocol.Request{protocol.Open(protocol.ModeApps, nil), protocol.End()}, "'end' is out of place"},
		{[]protocol.Request{protocol.Open(protocol.ModeDmenu, nil), protocol.Hello()}, "'hello' is out of place: the session is already open"},
		{[]protocol.Request{protocol.Cancel()}, "'cancel' is out of place"},
		{[]protocol.Request{protocol.Open(protocol.ModeProvider, nil)}, "a provider session must name its provider"},
		{[]protocol.Request{dmenuWithProvider}, "only a provider session names a provider"},
	} {
		t.Run(tc.needle, func(t *testing.T) {
			c, _ := newTestCore()
			w := greet(t, c, 1)
			for _, r := range tc.requests {
				c.receive(1, r, nil)
			}
			var replies []protocol.Reply
			for _, r := range w.take() {
				if r.Type != protocol.TypeVisible {
					replies = append(replies, r)
				}
			}
			if !isError(replies, tc.needle) {
				t.Errorf("replies %+v", replies)
			}
			if !w.closed || !c.idle() {
				t.Errorf("closed %v, idle %v", w.closed, c.idle())
			}
		})
	}
	// A provider session is shown; this core starts no providers.
	c, _ := newTestCore()
	w := greet(t, c, 1)
	c.receive(1, rite, nil)
	wantReplies(t, w, protocol.Visible(), protocol.Error("the provider would not wake: this machine spirit starts no providers"))
}

// The first word must be hello.
func TestAnythingButHelloFirstIsRefused(t *testing.T) {
	c, _ := newTestCore()
	w := &wire{}
	c.connect(1, w)
	c.receive(1, protocol.Open(protocol.ModeDmenu, nil), nil)
	if replies := w.take(); !isError(replies, "'open' is out of place: the first word must be 'hello'") {
		t.Errorf("replies %+v", replies)
	}
}

// Ported from an_undecodable_frame_ends_the_connection_with_an_error.
func TestAnUndecodableFrameEndsTheConnectionWithAnError(t *testing.T) {
	c, _ := newTestCore()
	w := greet(t, c, 1)
	c.receive(1, protocol.Request{}, errors.New("malformed message: oops"))
	if replies := w.take(); !isError(replies, "oops") || !w.closed {
		t.Errorf("replies %+v, closed %v", replies, w.closed)
	}
}

// Ported from a_mode_the_resolver_refuses_is_an_error.
func TestAModeTheResolverRefusesIsAnError(t *testing.T) {
	r := &recorder{refuse: "no window"}
	c := newCore(r, pid)
	w := open(t, c, 1, protocol.ModeDmenu)
	if replies := w.take(); !isError(replies, "no window") || !w.closed {
		t.Errorf("replies %+v, closed %v", replies, w.closed)
	}
	wantLog(t, r)
}

// Ported from the_daemon_is_idle_only_without_connections.
func TestTheDaemonIsIdleOnlyWithoutConnections(t *testing.T) {
	c, _ := newTestCore()
	if !c.idle() {
		t.Error("a new core is busy")
	}
	c.connect(1, &wire{})
	if c.idle() {
		t.Error("idle with a connection")
	}
	c.disconnect(1)
	if !c.idle() {
		t.Error("busy after the disconnect")
	}
}
