package trace

import (
	"bytes"
	"testing"
)

// Ports of Rust src/window/trace.rs's tests and of the line parser in
// tests/support/e2e/client.rs.

func TestALineCarriesThePrefixTheTimeAndTheEvent(t *testing.T) {
	if got := Line(1_234_567_890, "key 30 pressed"); got != "logis-trace: 1234567890 key 30 pressed" {
		t.Errorf("line %q", got)
	}
}

func TestTheTraceIsOnOnlyForASetNonZeroValue(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "yes": true, "0": false, "": false} {
		if got := EnabledBy(value); got != want {
			t.Errorf("EnabledBy(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestTheMonotonicClockMovesForward(t *testing.T) {
	a := MonotonicNS()
	b := MonotonicNS()
	if a == 0 || b < a {
		t.Errorf("clock went %d then %d", a, b)
	}
}

func TestTraceLinesParseIntoTimeAndEvent(t *testing.T) {
	e, ok := Parse("logis-trace: 42 key 30 pressed")
	if !ok || e.NS != 42 || e.What != "key 30 pressed" {
		t.Errorf("parsed %+v, %v", e, ok)
	}
	for _, line := range []string{
		"logis: the machine spirit wakes",
		"logis-trace: soon commit",
		"logis-trace: 42",
	} {
		if e, ok := Parse(line); ok {
			t.Errorf("%q parsed as %+v", line, e)
		}
	}
}

func TestEmitWritesStampedLinesOnlyWhenEnabled(t *testing.T) {
	var out bytes.Buffer
	off := &Tracer{out: &out}
	off.Emit("commit")
	if out.Len() != 0 {
		t.Fatalf("a disabled trace wrote %q", out.String())
	}
	on := &Tracer{out: &out, on: true}
	before := MonotonicNS()
	on.Emit("select %d", 2)
	e, ok := Parse(string(bytes.TrimSuffix(out.Bytes(), []byte("\n"))))
	if !ok || e.What != "select 2" || e.NS < before || e.NS > MonotonicNS() {
		t.Errorf("emitted %q", out.String())
	}
}
