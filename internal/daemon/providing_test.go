package daemon

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Provider sessions through the daemon's core with a fake process, on a
// clock the tests move.

// fakeRunning records what a provider process was sent and how it was
// treated: sent lines, `eof`, `signal N`.
type fakeRunning struct{ seen []string }

func (f *fakeRunning) Send(m provider.ToProvider) error {
	f.seen = append(f.seen, strings.TrimSuffix(string(provider.Encode(m)), "\n"))
	return nil
}
func (f *fakeRunning) CloseInput() { f.seen = append(f.seen, "eof") }
func (f *fakeRunning) Signal(sig syscall.Signal) {
	f.seen = append(f.seen, fmt.Sprintf("signal %d", sig))
}

func (f *fakeRunning) take() []string {
	s := f.seen
	f.seen = nil
	return s
}

type providerRig struct {
	core  *core
	seen  *recorder
	clock time.Time
	fakes map[SessionID]*fakeRunning
	logs  []string
}

func newProviderRig() *providerRig {
	r := &providerRig{seen: &recorder{}, clock: time.Unix(1_000_000, 0), fakes: map[SessionID]*fakeRunning{}}
	r.core = newCore(r.seen, pid)
	r.core.now = func() time.Time { return r.clock }
	r.core.logf = func(m string) { r.logs = append(r.logs, m) }
	r.core.providers.spawn = func(id SessionID, l protocol.Launch) (provider.Running, error) {
		if string(l.Argv[0]) == "/nowhere" {
			return nil, errors.New("'/nowhere' cannot be started: no such file or directory")
		}
		f := &fakeRunning{}
		r.fakes[id] = f
		return f, nil
	}
	return r
}

// open opens provider session id running program; its wire.
func (r *providerRig) open(t *testing.T, id SessionID, program string) *wire {
	t.Helper()
	w := greet(t, r.core, id)
	open := protocol.Open(protocol.ModeProvider, nil)
	open.Provider = &protocol.Launch{Argv: [][]byte{[]byte(program)}, Cwd: []byte("/")}
	r.core.receive(id, open, nil)
	return w
}

func (r *providerRig) say(id SessionID, m provider.Message) {
	r.core.heard(id, provider.Heard{Message: m})
}

// staged is a provider session past its greeting with one stage of ids.
func (r *providerRig) staged(t *testing.T, id SessionID, ids ...string) (*wire, *fakeRunning) {
	t.Helper()
	w := r.open(t, id, "rite")
	wantReplies(t, w, protocol.Visible())
	f := r.fakes[id]
	wantSeenLines(t, f, `{"type":"hello","protocol":1}`)
	r.say(id, provider.Hello{Protocol: 1})
	var items []provider.Item
	for _, i := range ids {
		items = append(items, wireItem(i))
	}
	r.say(id, provider.Stage{ID: "rite", Items: items})
	return w, f
}

func wireItem(id string) provider.Item {
	return provider.Item{ID: id, Columns: []string{strings.ToUpper(id)}}
}

func wantSeenLines(t *testing.T, f *fakeRunning, want ...string) {
	t.Helper()
	if got := f.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Fatalf("the provider saw %q, want %q", got, want)
	}
}

func outcome(t *testing.T, w *wire) protocol.OutcomeKind {
	t.Helper()
	replies := w.take()
	if len(replies) != 1 || replies[0].Type != protocol.TypeResult {
		t.Fatalf("%+v", replies)
	}
	return replies[0].Outcome.Kind
}

func errorOf(t *testing.T, w *wire) string {
	t.Helper()
	replies := w.take()
	if len(replies) != 1 || replies[0].Type != protocol.TypeError {
		t.Fatalf("%+v", replies)
	}
	return replies[0].Message
}

func (r *providerRig) deadline(t *testing.T) time.Time {
	t.Helper()
	at, ok := r.core.deadline()
	if !ok {
		t.Fatal("no timer is due")
	}
	return at
}

func TestAProviderStartsWhenItsSessionIsShownAndItsFirstStageShowsTheWindow(t *testing.T) {
	r := newProviderRig()
	w, _ := r.staged(t, 1, "oil", "fire")
	wantLog(t, r.seen, "show 1 provider [OIL,FIRE]")
	wantReplies(t, w) // the session stays open
	r.say(1, provider.Stage{ID: "aspect", Items: []provider.Item{wireItem("on")}})
	wantLog(t, r.seen, "stage 1 [ON]")
}

func TestAQueuedProviderSessionStartsOnlyOnceShown(t *testing.T) {
	r := newProviderRig()
	dmenu := open(t, r.core, 1, protocol.ModeDmenu)
	w := r.open(t, 2, "rite")
	wantReplies(t, dmenu, protocol.Visible(), result(protocol.Replaced))
	wantReplies(t, w, protocol.Visible())
	if r.fakes[2] == nil {
		t.Fatal("the provider did not start when its session replaced the visible one")
	}
}

func TestAProviderThatCannotStartEndsItsSessionWithTheReason(t *testing.T) {
	r := newProviderRig()
	w := r.open(t, 1, "/nowhere")
	replies := w.take()
	if len(replies) != 2 || replies[0] != protocol.Visible() || replies[1].Type != protocol.TypeError ||
		!strings.Contains(replies[1].Message, "'/nowhere' cannot be started") {
		t.Fatalf("%+v", replies)
	}
	if !r.core.idle() {
		t.Fatal("not idle")
	}
}

func TestAnAnswerActivatesAndTheProvidersCloseEndsTheSessionWithClosed(t *testing.T) {
	r := newProviderRig()
	w, f := r.staged(t, 1, "oil")
	r.core.answered(1, Answer{Kind: AnswerItem, Index: 0})
	wantSeenLines(t, f, `{"type":"activate","stage":"rite","id":"oil"}`)
	r.say(1, provider.Close{})
	if got := outcome(t, w); got != protocol.Closed {
		t.Fatal(got)
	}
	wantSeenLines(t, f, "eof") // no cancel after its own close
	if log := r.seen.take(); log[len(log)-1] != "hide 1" {
		t.Fatalf("%q", log)
	}
	if _, ok := r.core.deadline(); ok {
		t.Fatal("an acting provider is to be signalled")
	}
}

func TestADismissedProviderThatNeverActedIsCancelledThenEndedIfItLingers(t *testing.T) {
	r := newProviderRig()
	w, f := r.staged(t, 1, "oil")
	r.core.answered(1, Answer{Kind: AnswerDismissed})
	if got := outcome(t, w); got != protocol.Dismissed {
		t.Fatal(got)
	}
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
	if r.core.idle() {
		t.Fatal("idle while it is still to be ended")
	}
	term := r.deadline(t)
	r.core.tick(term.Add(-time.Millisecond))
	wantSeenLines(t, f)
	r.core.tick(term)
	wantSeenLines(t, f, "signal 15")
	kill := r.deadline(t)
	if kill.Sub(term) != grace {
		t.Fatalf("SIGKILL %v after SIGTERM", kill.Sub(term))
	}
	r.core.tick(kill)
	wantSeenLines(t, f, "signal 9")
	if _, ok := r.core.deadline(); ok {
		t.Fatal("a timer after SIGKILL")
	}
	r.core.tick(kill.Add(time.Hour))
	wantSeenLines(t, f) // killed once
	if r.core.idle() {
		t.Fatal("idle before the killed provider's exit is heard")
	}
	r.core.heard(1, provider.Heard{Exited: true, Status: "signal: 9 (SIGKILL)"})
	if !r.core.idle() {
		t.Fatal("not idle once it is gone")
	}
}

func TestARetiredProviderThatExitsIsForgotten(t *testing.T) {
	r := newProviderRig()
	r.staged(t, 1, "oil")
	f := r.fakes[1]
	r.core.disconnect(1)
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
	r.core.heard(1, provider.Heard{Exited: true, Status: "exit status: 0"})
	if !r.core.idle() {
		t.Fatal("not idle")
	}
	if _, ok := r.core.deadline(); ok {
		t.Fatal("a timer for a gone provider")
	}
}

func TestDismissalNeverSignalsAProviderThatWasSentAnAnswer(t *testing.T) {
	r := newProviderRig()
	w, f := r.staged(t, 1, "oil")
	r.core.answered(1, Answer{Kind: AnswerItem, Index: 0})
	f.take()
	r.core.answered(1, Answer{Kind: AnswerDismissed})
	if got := outcome(t, w); got != protocol.Dismissed {
		t.Fatal(got)
	}
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
	if _, ok := r.core.deadline(); ok || !r.core.idle() {
		t.Fatal("an acting provider is to be signalled")
	}
}

func TestAReplacedProviderSessionIsCancelled(t *testing.T) {
	r := newProviderRig()
	first, f := r.staged(t, 1, "oil")
	r.open(t, 2, "rite")
	if got := outcome(t, first); got != protocol.Replaced {
		t.Fatal(got)
	}
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
}

// A provider's toast whose words take longer to read than it asked to
// show stays until they are read (list.Notice.ReadFor).
func TestALongToastShowsLongEnoughToRead(t *testing.T) {
	r := newProviderRig()
	r.staged(t, 1, "oil")
	r.seen.take()
	long := strings.Repeat("the litany goes on ", 8)
	r.say(1, provider.Toast{Text: long, Level: list.Error, Seconds: new(uint32(1))})
	if over := r.deadline(t); over != r.clock.Add(10*time.Second) {
		t.Fatalf("over after %v", over.Sub(r.clock))
	}
}

func TestTheFirstStageIsDueWithinThePatience(t *testing.T) {
	r := newProviderRig()
	w := r.open(t, 1, "rite")
	w.take()
	r.say(1, provider.Hello{Protocol: 1})
	due := r.deadline(t)
	if due != r.clock.Add(5*time.Second) {
		t.Fatalf("due after %v", due.Sub(r.clock))
	}
	r.core.tick(due.Add(-time.Millisecond))
	wantReplies(t, w)
	r.core.tick(due)
	if m := errorOf(t, w); !strings.Contains(m, "showed nothing within 5 s") {
		t.Fatal(m)
	}
}

func TestAToastShowsForItsSpanAndAfterCloseTheWindowLingersOnIt(t *testing.T) {
	r := newProviderRig()
	w, _ := r.staged(t, 1, "oil")
	r.seen.take()
	toast := provider.Toast{Text: "the rite is begun", Level: list.Success, Seconds: new(uint32(8))}
	r.say(1, toast)
	wantLog(t, r.seen, "toast 1 the rite is begun")
	over := r.deadline(t)
	if over != r.clock.Add(8*time.Second) {
		t.Fatalf("over after %v", over.Sub(r.clock))
	}
	r.clock = over
	r.core.tick(over)
	wantLog(t, r.seen, "untoast 1")
	r.say(1, toast)
	r.say(1, provider.Close{})
	if got := outcome(t, w); got != protocol.Closed {
		t.Fatalf("the caller is not answered at once: %v", got)
	}
	wantLog(t, r.seen, "toast 1 the rite is begun", "linger 1")
	r.core.tick(r.clock.Add(8*time.Second - time.Millisecond))
	wantLog(t, r.seen)
	r.core.tick(r.clock.Add(8 * time.Second))
	wantLog(t, r.seen, "hide 1")
}

func TestWithoutALingererTheWindowHidesAtOnce(t *testing.T) {
	r := newProviderRig()
	r.core.resolver = plainResolver{r.seen}
	w, _ := r.staged(t, 1, "oil")
	r.say(1, provider.Toast{Text: "done"})
	r.say(1, provider.Close{})
	if got := outcome(t, w); got != protocol.Closed {
		t.Fatal(got)
	}
	if log := r.seen.take(); log[len(log)-1] != "hide 1" {
		t.Fatalf("%q", log)
	}
}

// plainResolver is a recorder that is no Lingerer.
type plainResolver struct{ r *recorder }

func (p plainResolver) Accepts(m protocol.Mode) error         { return p.r.Accepts(m) }
func (p plainResolver) Show(s Session)                        { p.r.Show(s) }
func (p plainResolver) Step(id SessionID, step provider.Step) { p.r.Step(id, step) }
func (p plainResolver) End(id SessionID)                      { p.r.End(id) }
func (p plainResolver) Hide(id SessionID)                     { p.r.Hide(id) }

func TestAProviderThatDepartsOrBreaksTheProtocolFailsItsSession(t *testing.T) {
	r := newProviderRig()
	w, _ := r.staged(t, 1, "oil")
	r.core.heard(1, provider.Heard{Exited: true, Status: "exit status: 3"})
	if m := errorOf(t, w); !strings.Contains(m, "exit status: 3") || !strings.Contains(m, "departed") {
		t.Fatal(m)
	}
	w, f := r.staged(t, 2, "oil")
	r.core.heard(2, provider.Heard{Err: errors.New("not a JSON line")})
	if m := errorOf(t, w); !strings.Contains(m, "not a JSON line") {
		t.Fatal(m)
	}
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
}

func TestUpdatesReachTheWindowAndToastsBeforeItAreOnlyLogged(t *testing.T) {
	r := newProviderRig()
	w := r.open(t, 1, "rite")
	w.take()
	r.say(1, provider.Hello{Protocol: 1})
	r.say(1, provider.Toast{Text: "early"})
	wantLog(t, r.seen)
	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "early") {
		t.Fatalf("logged %q", r.logs)
	}
	// Items before any stage break the protocol.
	r.say(1, provider.Items{Items: []provider.Item{wireItem("early")}})
	if m := errorOf(t, w); !strings.Contains(m, "'items' came before any 'stage'") {
		t.Fatal(m)
	}
	r = newProviderRig()
	r.open(t, 1, "rite")
	r.say(1, provider.Hello{Protocol: 1})
	pending := wireItem("oil")
	pending.Pending = true
	r.say(1, provider.Stage{ID: "s", Items: []provider.Item{pending}})
	r.say(1, provider.Items{Items: []provider.Item{wireItem("oil")}})
	wantLog(t, r.seen, "show 1 provider [OIL]", "update 1 0")
	r.say(1, provider.Unknown{Type: "omen"})
	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "'omen'") {
		t.Fatalf("logged %q", r.logs)
	}
}

func TestDmenuAnswersEndTheSessionWithTheChosenLine(t *testing.T) {
	r := newProviderRig()
	w := open(t, r.core, 1, protocol.ModeDmenu)
	lines := []protocol.Item{{Text: "a", Raw: []byte("a")}, {Text: "�b", Raw: []byte("\xffb")}}
	r.core.receive(1, protocol.Items(lines), nil)
	w.take()
	r.core.answered(1, Answer{Kind: AnswerDescend, Index: 1})
	want := protocol.Result(protocol.Outcome{Kind: protocol.Selected, Index: 1, Item: lines[1]})
	wantReplies(t, w, want)
}

func TestTheDaemonsExitRetiresItsProviders(t *testing.T) {
	r := newProviderRig()
	_, f := r.staged(t, 1, "oil")
	r.core.closeAll()
	wantSeenLines(t, f, `{"type":"cancel"}`, "eof")
}
