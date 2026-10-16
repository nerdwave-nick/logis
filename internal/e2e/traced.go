package e2e

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/trace"
)

// Traced is a logis process with the test trace on (LOGIS_TEST_TRACE=1),
// its stderr read line by line: waiting for keyboard focus, key events
// and frame commits, each stamped with the client's CLOCK_MONOTONIC. t's
// cleanup kills it.
type Traced struct {
	t   testing.TB
	cmd *exec.Cmd

	mu      sync.Mutex
	pending []string      // lines read but not yet taken; guarded by mu
	ended   bool          // stderr reached its end; guarded by mu
	more    chan struct{} // a line arrived or stderr ended (capacity 1)
	exited  chan struct{} // closed once the process was waited for

	lines  []string
	events []trace.Event
	cursor int
}

// Spawn starts cmd with the trace on and its stderr read (its stdout is
// discarded unless set).
func Spawn(t testing.TB, cmd *exec.Cmd) *Traced {
	t.Helper()
	cmd.Env = append(cmd.Env, trace.Env+"=1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s does not start: %v", cmd.Path, err)
	}
	p := &Traced{t: t, cmd: cmd, more: make(chan struct{}, 1), exited: make(chan struct{})}
	go p.read(stderr)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-p.exited
	})
	return p
}

func (p *Traced) read(stderr io.Reader) {
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		p.mu.Lock()
		p.pending = append(p.pending, sc.Text())
		p.mu.Unlock()
		p.signal()
	}
	_ = p.cmd.Wait()
	p.mu.Lock()
	p.ended = true
	p.mu.Unlock()
	close(p.exited)
	p.signal()
}

func (p *Traced) signal() {
	select {
	case p.more <- struct{}{}:
	default:
	}
}

// take moves the lines read so far into lines and events; it reports
// whether stderr has ended.
func (p *Traced) take() bool {
	p.mu.Lock()
	pending, ended := p.pending, p.ended
	p.pending = nil
	p.mu.Unlock()
	for _, line := range pending {
		p.lines = append(p.lines, line)
		if e, ok := trace.Parse(line); ok {
			p.events = append(p.events, e)
		}
	}
	return ended
}

// PID is the process id.
func (p *Traced) PID() int { return p.cmd.Process.Pid }

// Stderr is everything the process wrote to stderr so far.
func (p *Traced) Stderr() string {
	p.take()
	return strings.Join(p.lines, "\n")
}

// Events is every trace event seen so far.
func (p *Traced) Events() []trace.Event {
	p.take()
	return p.events
}

// WaitFor is the next event after the last one waited for that satisfies
// want, within Patience; earlier non-matching events are passed over. Use
// it for sequences (a key, then its commit).
func (p *Traced) WaitFor(what string, want func(trace.Event) bool) trace.Event {
	p.t.Helper()
	i := p.waitIndex(what, p.cursor, want)
	p.cursor = i + 1
	return p.events[i]
}

// WaitSeen is the first event since the start that satisfies want,
// without moving the sequence cursor. Use it for facts whose order is not
// fixed (focus may come before or after the first commit).
func (p *Traced) WaitSeen(what string, want func(trace.Event) bool) trace.Event {
	p.t.Helper()
	return p.events[p.waitIndex(what, 0, want)]
}

func (p *Traced) waitIndex(what string, from int, want func(trace.Event) bool) int {
	p.t.Helper()
	deadline := time.After(Patience)
	for {
		ended := p.take()
		for i := from; i < len(p.events); i++ {
			if want(p.events[i]) {
				return i
			}
		}
		from = len(p.events)
		if ended {
			p.t.Fatalf("logis ended (%v) before %s; stderr:\n%s", p.cmd.ProcessState, what, p.Stderr())
		}
		select {
		case <-p.more:
		case <-deadline:
			p.t.Fatalf("no %s in %v; stderr:\n%s", what, Patience, p.Stderr())
		}
	}
}

// Is matches events that are exactly what.
func Is(what string) func(trace.Event) bool {
	return func(e trace.Event) bool { return e.What == what }
}

// Word matches events of word: `word` alone or `word …`.
func Word(word string) func(trace.Event) bool {
	return func(e trace.Event) bool { return e.What == word || strings.HasPrefix(e.What, word+" ") }
}

// WaitForFocus waits until the client reported keyboard focus on its
// panel: the proof the virtual keyboard types with.
func (p *Traced) WaitForFocus() Focused {
	p.t.Helper()
	p.WaitSeen("keyboard focus", Is("keyboard-enter"))
	return Focused{proof: &struct{}{}}
}

// WaitForNextFocus waits for the next keyboard focus in sequence: a
// daemon shows one window per session, and each takes the focus anew.
func (p *Traced) WaitForNextFocus() Focused {
	p.t.Helper()
	p.WaitFor("keyboard focus", Is("keyboard-enter"))
	return Focused{proof: &struct{}{}}
}

// WaitForFirstCommit waits until the client committed its first frame.
func (p *Traced) WaitForFirstCommit() trace.Event {
	p.t.Helper()
	return p.WaitSeen("a first frame commit", Is("commit"))
}

// WaitForCommit waits for the next frame commit in sequence.
func (p *Traced) WaitForCommit() trace.Event {
	p.t.Helper()
	return p.WaitFor("a frame commit", Is("commit"))
}

// WaitForCommitAfter waits for the next commit stamped at or after ns.
func (p *Traced) WaitForCommitAfter(ns uint64) trace.Event {
	p.t.Helper()
	return p.WaitFor("a frame commit", func(e trace.Event) bool { return e.What == "commit" && e.NS >= ns })
}

// WaitExit waits for the process to exit within Patience and returns its
// exit code.
func (p *Traced) WaitExit() int {
	p.t.Helper()
	select {
	case <-p.exited:
		return p.cmd.ProcessState.ExitCode()
	case <-time.After(Patience):
		p.t.Fatalf("logis did not exit; stderr:\n%s", p.Stderr())
		return -1
	}
}

// WaitUntil waits until done holds for every trace event seen so far,
// within Patience: for a state that events build up together (every
// frame answered), where WaitFor and WaitSeen match one event.
func (p *Traced) WaitUntil(what string, done func([]trace.Event) bool) {
	p.t.Helper()
	deadline := time.After(Patience)
	for {
		ended := p.take()
		if done(p.events) {
			return
		}
		if ended {
			p.t.Fatalf("logis ended (%v) before %s; stderr:\n%s", p.cmd.ProcessState, what, p.Stderr())
		}
		select {
		case <-p.more:
		case <-deadline:
			p.t.Fatalf("no %s in %v; stderr:\n%s", what, Patience, p.Stderr())
		}
	}
}
