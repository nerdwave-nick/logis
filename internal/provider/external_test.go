package provider

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// An external provider's protocol rules, on a fake process.

// fake records what a provider process was sent and how it was treated,
// one string each: sent lines (without newline), `eof`, `signal N`.
type fake struct {
	seen []string
	// broken: sends fail, as to a provider that no longer reads.
	broken bool
}

func (f *fake) take() []string {
	s := f.seen
	f.seen = nil
	return s
}

func (f *fake) Send(m ToProvider) error {
	if f.broken {
		return errors.New("the provider does not take its input")
	}
	f.seen = append(f.seen, strings.TrimSuffix(string(Encode(m)), "\n"))
	return nil
}
func (f *fake) CloseInput()               { f.seen = append(f.seen, "eof") }
func (f *fake) Signal(sig syscall.Signal) { f.seen = append(f.seen, fmt.Sprintf("signal %d", sig)) }

func wantSeen(t *testing.T, f *fake, want ...string) {
	t.Helper()
	if got := f.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Fatalf("seen %q, want %q", got, want)
	}
}

func greeted(t *testing.T) (*External, *fake) {
	t.Helper()
	f := &fake{}
	e, err := NewExternal(f)
	if err != nil {
		t.Fatal(err)
	}
	wantSeen(t, f, `{"type":"hello","protocol":1}`)
	if _, err := e.Heard(Hello{Protocol: 1}, nil); err != nil {
		t.Fatal(err)
	}
	return e, f
}

func stageOf(t *testing.T, e *External) {
	t.Helper()
	said, err := e.Heard(Stage{ID: "rite", Items: []Item{item("oil", "oil")}}, nil)
	if err != nil || said.Close || len(said.Steps) != 1 || said.Steps[0].Kind != StepStage {
		t.Fatalf("%+v, %v", said, err)
	}
}

func TestTheProviderMustGreetFirstInThisProtocol(t *testing.T) {
	e, _ := NewExternal(&fake{})
	if _, err := e.Heard(Ready{}, nil); err == nil || !strings.Contains(err.Error(), "hello") {
		t.Fatalf("%v", err)
	}
	e, _ = NewExternal(&fake{})
	if _, err := e.Heard(Hello{Protocol: 2}, nil); err == nil || !strings.Contains(err.Error(), "protocol 2") {
		t.Fatalf("%v", err)
	}
	e, _ = greeted(t)
	if _, err := e.Heard(Hello{Protocol: 1}, nil); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("%v", err)
	}
	if _, err := NewExternal(&fake{broken: true}); err == nil {
		t.Fatal("a provider that takes no greeting started")
	}
}

func TestUnknownLinesAreSkippedAndErrorsEndTheSession(t *testing.T) {
	e, _ := greeted(t)
	var logged []string
	e.Log = func(m string) { logged = append(logged, m) }
	if said, err := e.Heard(Unknown{Type: "omen"}, nil); err != nil || said.Close || len(said.Steps) != 0 {
		t.Fatalf("%+v, %v", said, err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "'omen'") {
		t.Fatalf("logged %q", logged)
	}
	if _, err := e.Heard(nil, errors.New("not a JSON line")); err == nil {
		t.Fatal("garbage was taken")
	}
}

func TestAnAnswerIsSentAsActivate(t *testing.T) {
	e, f := greeted(t)
	stageOf(t, e)
	if err := e.Answer(Answer{Kind: AnswerItem}); err != nil {
		t.Fatal(err)
	}
	wantSeen(t, f, `{"type":"activate","stage":"rite","id":"oil"}`)
}

func TestAnAnswerNoItemHasSendsNothingAndAFailedSendIsAnError(t *testing.T) {
	e, f := greeted(t)
	stageOf(t, e)
	if err := e.Answer(Answer{Kind: AnswerItem, Index: 5}); err != nil {
		t.Fatal(err)
	}
	wantSeen(t, f)
	if r, ok := e.Retire(); !ok || r == nil {
		t.Fatal("it never acted: it may be ended")
	}
	e, f = greeted(t)
	stageOf(t, e)
	f.broken = true
	if err := e.Answer(Answer{Kind: AnswerItem}); err == nil {
		t.Fatal("a lost provider took its answer")
	}
}

func TestAProviderThatNeverActedIsCancelledAndMayBeEnded(t *testing.T) {
	e, f := greeted(t)
	stageOf(t, e)
	rest, ok := e.Retire()
	wantSeen(t, f, `{"type":"cancel"}`, "eof")
	if !ok || rest != Running(f) {
		t.Fatal("it may not be signalled if it lingers")
	}
}

func TestAProviderThatActedIsCancelledButNeverSignalled(t *testing.T) {
	e, f := greeted(t)
	stageOf(t, e)
	if err := e.Answer(Answer{Kind: AnswerTyped, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	f.take()
	if _, ok := e.Retire(); ok {
		t.Fatal("an action may be under way")
	}
	wantSeen(t, f, `{"type":"cancel"}`, "eof")
}

func TestAClosedFlowHearsNoCancelAndAGoneProcessNoSignal(t *testing.T) {
	e, f := greeted(t)
	stageOf(t, e)
	if said, err := e.Heard(Close{}, nil); err != nil || !said.Close {
		t.Fatalf("%+v, %v", said, err)
	}
	_, ok := e.Retire()
	wantSeen(t, f, "eof")
	if !ok {
		t.Fatal("it never acted")
	}
	e, f = greeted(t)
	e.Exited()
	if _, ok := e.Retire(); ok {
		t.Fatal("a gone process may be signalled")
	}
	wantSeen(t, f, "eof")
}
