// Package trace is the test trace (Rust src/window/trace.rs): a test-only
// observable of what the window saw, enabled by LOGIS_TEST_TRACE=1 and
// silent otherwise. Each event is one stderr line,
// `logis-trace: <CLOCK_MONOTONIC ns> <event>`, so a harness can wait for
// keyboard focus before typing and time commits against its own clock
// (research/bench's T0_NS method).
//
// Events so far: `keyboard-enter`, `keyboard-leave`, `key <evdev code>
// pressed|released`, `modifiers <depressed> <latched> <locked> <group>`,
// `repeat-info <rate> <delay>`, `commit` (a frame of the visible panel
// was presented and its surface committed), `settled` (the first frame
// that leaves nothing moving), `frame-done` (the compositor's frame
// callback for a visible frame), `presented` and `discarded` (its
// presentation feedback), `commit closing` (a frame of a panel
// fading out), `faded` (a hidden panel is gone), `cut` (a closing panel
// cut short by the next one is gone), `blur <rectangles>` and
// `blur-capable true|false` from the window; `keymap` (one
// compiled) and `key <evdev code> repeated` from the keyboard; `show
// debug-panel`, `query <JSON string>`, `select <row>`, `accept <row>` and
// `dismiss` from the debug panel. The
// daemon's sessions add theirs (`show <session>`, `items <n>`, `query`,
// `select`, `accept`, `typed`, `dismiss`, `toast`, `hide <session>` …) as
// Rust's README lists them.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Env is the variable that enables the trace.
const Env = "LOGIS_TEST_TRACE"

// Prefix starts every trace line.
const Prefix = "logis-trace:"

// EnabledBy reports whether a value of Env enables the trace: non-empty
// and not 0.
func EnabledBy(value string) bool { return value != "" && value != "0" }

// Line is the trace line for event at ns.
func Line(ns uint64, event string) string {
	return Prefix + " " + strconv.FormatUint(ns, 10) + " " + event
}

// Event is one parsed trace line: when (CLOCK_MONOTONIC ns) and what.
type Event struct {
	NS   uint64
	What string
}

// Parse reads a trace line; ok is false for any other line.
func Parse(line string) (e Event, ok bool) {
	rest, found := strings.CutPrefix(line, Prefix)
	if !found {
		return Event{}, false
	}
	ns, what, found := strings.Cut(strings.TrimLeft(rest, " "), " ")
	if !found {
		return Event{}, false
	}
	n, err := strconv.ParseUint(ns, 10, 64)
	if err != nil {
		return Event{}, false
	}
	return Event{NS: n, What: what}, true
}

// Tracer writes trace lines to out while on.
type Tracer struct {
	out io.Writer
	on  bool
}

// Emit writes the event fmt.Sprintf(format, args...) stamped now, if the
// trace is on. A failed write (a closed stderr) is ignored.
func (t *Tracer) Emit(format string, args ...any) {
	if !t.on {
		return
	}
	ns := MonotonicNS()
	_, _ = io.WriteString(t.out, Line(ns, fmt.Sprintf(format, args...))+"\n")
}

// process is the process's tracer: stderr, on when Env says so at the
// first event.
var process = sync.OnceValue(func() *Tracer {
	return &Tracer{out: os.Stderr, on: EnabledBy(os.Getenv(Env))}
})

// Emit writes an event to stderr when LOGIS_TEST_TRACE enables the trace.
func Emit(format string, args ...any) { process().Emit(format, args...) }

// Quote is how the trace writes text (`query <JSON string>`): a JSON
// string, with <, > and & as they are (as Rust's serde_json writes it).
func Quote(text string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(text)
	return strings.TrimSuffix(b.String(), "\n")
}
