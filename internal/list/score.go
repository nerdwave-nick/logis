package list

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

const (
	// parallelMin: below this many candidates, matching stays on the
	// calling goroutine.
	parallelMin = 8192
	// maxWorkers is the most matching goroutines.
	maxWorkers = 8
	// chunk is how many candidates a goroutine takes at a time; small, so
	// that one slow core (an efficiency core, or one the system lends
	// elsewhere) does not hold up the rest.
	chunk = 1024
	// countingRange: scores spread over fewer values than this are sorted
	// by counting, in linear time.
	countingRange = 1 << 16
)

// score scores candidates (ascending indices), best first, into the
// memory of into when it is large enough (it must not be in use).
func (f *Filter) score(candidates []uint32, into []Match) []Match {
	raw := grow(f.raw, len(candidates))
	f.raw = raw
	chunks := (len(candidates) + chunk - 1) / chunk
	counts := grow(f.counts, chunks)
	f.counts = counts
	run := func(worker int, c int) {
		lo, hi := c*chunk, min((c+1)*chunk, len(candidates))
		counts[c] = f.scoreInto(worker, candidates[lo:hi], raw[lo:hi])
	}
	workers := 1
	if len(candidates) >= parallelMin {
		workers = min(max(runtime.GOMAXPROCS(0), 1), maxWorkers, chunks)
	}
	for w := range workers {
		f.matcher(w)
	}
	if workers == 1 {
		for c := range chunks {
			run(0, c)
		}
	} else {
		var next atomic.Int64
		var wg sync.WaitGroup
		for w := range workers {
			wg.Go(func() {
				for c := int(next.Add(1) - 1); c < chunks; c = int(next.Add(1) - 1) {
					run(w, c)
				}
			})
		}
		wg.Wait()
	}
	// The chunks follow one another in input order: compact them, then
	// sort by score alone, stably.
	n := 0
	for c, count := range counts {
		n += copy(raw[n:], raw[c*chunk:c*chunk+count])
	}
	return sortMatches(raw[:n], grow(into, n))
}

// scoreInto scores candidates into out with matcher worker; how many
// matched.
func (f *Filter) scoreInto(worker int, candidates []uint32, out []Match) int {
	m, p, n := f.matchers[worker], f.pattern, 0
	for _, i := range candidates {
		score, ok := p.Score(m, f.texts[i])
		if !ok {
			continue
		}
		if int(i) < len(f.boosts) {
			score += f.boosts[i]
		}
		out[n] = Match{Index: i, Score: score}
		n++
	}
	return n
}

func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// before is the order of matches: best score first, then input order.
func before(a, b Match) bool {
	return a.Score > b.Score || (a.Score == b.Score && a.Index < b.Index)
}

// sortMatches sorts ms, given in input order, into match order in out
// (len(out) == len(ms)).
func sortMatches(ms, out []Match) []Match {
	if len(ms) == 0 {
		return out[:0]
	}
	lo, hi := ms[0].Score, ms[0].Score
	for _, m := range ms {
		lo, hi = min(lo, m.Score), max(hi, m.Score)
	}
	if hi-lo >= countingRange {
		copy(out, ms)
		slices.SortStableFunc(out, func(a, b Match) int {
			switch {
			case a.Score > b.Score:
				return -1
			case a.Score < b.Score:
				return 1
			}
			return 0
		})
		return out
	}
	at := make([]int, hi-lo+1)
	for _, m := range ms {
		at[hi-m.Score]++
	}
	sum := 0
	for i, n := range at {
		at[i], sum = sum, sum+n
	}
	for _, m := range ms {
		out[at[hi-m.Score]] = m
		at[hi-m.Score]++
	}
	return out
}

// merge merges two lists in match order into one.
func merge(a, b []Match) []Match {
	if len(b) == 0 {
		return a
	}
	out := make([]Match, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if before(b[0], a[0]) {
			out, b = append(out, b[0]), b[1:]
		} else {
			out, a = append(out, a[0]), a[1:]
		}
	}
	out = append(out, a...)
	return append(out, b...)
}
