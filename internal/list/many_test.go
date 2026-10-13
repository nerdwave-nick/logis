package list

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// manyLines is Rust's tests/latency.rs many_lines: 100 000 distinct
// path-like lines, as a big dmenu input might be; with columns, each a
// path, a word and a number separated by tabs.
func manyLines(columns bool) []string {
	words := []string{"rite", "aspect", "cogitator", "noosphere", "spirit", "litany", "forge"}
	out := make([]string, 0, 100_000)
	for i := range 100_000 {
		path := fmt.Sprintf("/%s/%s/%s-%d", words[i%7], words[(i/7)%7], words[(i/49)%7], i)
		if columns {
			path = fmt.Sprintf("%s\t%s\t%d", path, words[(i/343)%7], i*7919%100_003)
		}
		out = append(out, path)
	}
	return out
}

// keystrokes are the latency harness's: "rite" typed, then erased.
var keystrokes = []string{"r", "ri", "rit", "rite", "rit", "ri", "r", ""}

// keystroke is what a keystroke costs the list: the new query matched
// against every item, and the positions of the nine rows in view.
func keystroke(f *Filter, query string) {
	f.SetQuery(query)
	for _, m := range f.Matches()[:min(DefaultRows, len(f.Matches()))] {
		f.Positions(m.Index)
	}
}

// One frame at 60 Hz. Filtering on a keystroke over 100 000 items must
// stay well within it, at most half a frame by the median, so that the
// frame itself fits too (spec §3: keystroke → frame within one frame).
const frame = time.Second / 60

// As the Rust latency harness: rounds from LOGIS_LATENCY_RUNS (5 by
// default), and the budget fails the run only with
// LOGIS_LATENCY_BUDGETS=enforce; otherwise the times are recorded.
func TestTypingOver100000ItemsStaysWithinAFrame(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing: not under -short or -race")
	}
	rounds := 5
	if v := os.Getenv("LOGIS_LATENCY_RUNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("LOGIS_LATENCY_RUNS=%q is no count", v)
		}
		rounds = n
	}
	enforce := os.Getenv("LOGIS_LATENCY_BUDGETS") == "enforce"
	for _, columns := range []bool{false, true} {
		f := NewFilter()
		f.Push(manyLines(columns)...)
		var times []time.Duration
		for range rounds {
			for _, q := range keystrokes {
				start := time.Now()
				keystroke(f, q)
				times = append(times, time.Since(start))
			}
		}
		slices.Sort(times)
		median, worst := times[len(times)/2], times[len(times)-1]
		t.Logf("100 000 items, columns %v: keystroke median %v, worst %v over %d", columns, median, worst, len(times))
		if enforce && median > frame/2 {
			t.Errorf("columns %v: median keystroke %v, more than half a frame (%v)", columns, median, frame/2)
		}
	}
}

func BenchmarkKeystroke100000(b *testing.B) {
	for _, columns := range []bool{false, true} {
		b.Run(fmt.Sprintf("columns=%v", columns), func(b *testing.B) {
			f := NewFilter()
			f.Push(manyLines(columns)...)
			b.ResetTimer()
			for b.Loop() {
				for _, q := range keystrokes {
					keystroke(f, q)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(keystrokes))/1e6, "ms/keystroke")
		})
	}
}

// The 100 000 lines arriving in dmenu's batches of 1024 while a query is
// in force: each batch is matched alone and merged in.
func BenchmarkStreaming100000(b *testing.B) {
	lines := manyLines(true)
	for b.Loop() {
		f := NewFilter()
		f.SetQuery("rite")
		for i := 0; i < len(lines); i += 1024 {
			f.Push(lines[i:min(i+1024, len(lines))]...)
		}
	}
}
