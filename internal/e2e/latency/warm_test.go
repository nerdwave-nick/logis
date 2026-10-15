package latency

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// A fake daemon: this test binary again, reporting a session per `open`
// line on its stdin (show, a first commit `delay` later, settled) and its
// end per `end` line (hide, faded), stamped like logis's trace.
func TestMain(m *testing.M) {
	if os.Getenv("LATENCY_FAKE_DAEMON") == "" {
		os.Exit(m.Run())
	}
	in := bufio.NewScanner(os.Stdin)
	for n := 1; in.Scan(); {
		switch cmd, delay, _ := strings.Cut(in.Text(), " "); cmd {
		case "open":
			trace.Emit("show %d", n)
			d, _ := time.ParseDuration(delay)
			time.Sleep(d)
			trace.Emit("commit")
			trace.Emit("settled")
		case "end":
			trace.Emit("hide %d", n)
			trace.Emit("faded")
			n++
		}
	}
	os.Exit(0)
}

func fakeDaemon(t *testing.T) (*e2e.Traced, func(string)) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "LATENCY_FAKE_DAEMON=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	daemon := e2e.Spawn(t, cmd)
	return daemon, func(line string) {
		if _, err := stdin.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAWarmOpenIsTimedFromTheClientsStartToTheFirstCommit(t *testing.T) {
	daemon, say := fakeDaemon(t)
	opens := 0
	w := WarmOpen(t, daemon, nil, 3, true, func() func() {
		opens++
		say("open 5ms")
		return func() { say("end") }
	})
	if opens != 4 {
		t.Errorf("%d opens, want a warm-up and 3", opens)
	}
	if len(w.First) != 3 || len(w.Settled) != 3 {
		t.Fatalf("%+v", w)
	}
	for i, ms := range w.First {
		if ms < 5 || w.Settled[i] < ms {
			t.Errorf("open %d: first commit %.2f ms, settled %.2f ms", i, ms, w.Settled[i])
		}
	}
	// No 5 ms open is slow: a wall-clock limit, which a busy machine can
	// break.
	if testing.Short() {
		t.Logf("timing: the %v ms slow-open limit is a wall-clock check, not under -short", SlowOpenMS)
		return
	}
	for i, ms := range w.First {
		if ms > SlowOpenMS {
			t.Errorf("open %d: first commit %.2f ms", i, ms)
		}
	}
	if len(w.Slow) != 0 {
		t.Errorf("slow: %q", w.Slow)
	}
}

// Only the 150 ms open is slow; the others must stay under the limit on
// the wall clock.
func TestASlowWarmOpenIsReported(t *testing.T) {
	if testing.Short() {
		t.Skip("timing: which opens are slow depends on the wall clock, not under -short")
	}
	daemon, say := fakeDaemon(t)
	run := 0
	w := WarmOpen(t, daemon, nil, 2, false, func() func() {
		run++
		delay := "0s"
		if run == 3 {
			delay = "150ms"
		}
		say("open " + delay)
		return func() { say("end") }
	})
	if len(w.Slow) != 1 || !strings.Contains(w.Slow[0], "answered throughout") || len(w.Settled) != 0 {
		t.Errorf("%+v", w)
	}
}
