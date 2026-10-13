// Package latency is the latency harness's statistics and reports (Rust
// tests/support/latency.rs), measured the research/bench way (T0_NS): the
// harness reads CLOCK_MONOTONIC (trace.MonotonicNS) just before it acts —
// injects a key, starts a client — and the client stamps its milestones
// with the same clock in its test trace (`commit` …). Each series is
// reported against its spec §3 budget: LOGIS_LATENCY_REPORT=<file> appends
// JSON lines, GITHUB_STEP_SUMMARY gets the table, and
// LOGIS_LATENCY_BUDGETS=enforce fails a median over budget (for the
// owner's quiet machine; CI records the trend only).
//
// Cold start / first open is not measured here: the owner benchmarks it
// separately.
package latency

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The environment the harness reads.
const (
	ReportEnv  = "LOGIS_LATENCY_REPORT"
	RunsEnv    = "LOGIS_LATENCY_RUNS"
	BudgetsEnv = "LOGIS_LATENCY_BUDGETS"
)

// WarmOpenMS is the warm open budget (spec §3: ≤ 15 ms).
const WarmOpenMS = 15.0

// SlowOpenMS: no single warm open may take longer (Rust ticket d49bceee),
// enforced with the budgets; a median within budget can hide a two-second
// open.
const SlowOpenMS = 100.0

// Series is the samples of one metric in milliseconds, with its budget.
type Series struct {
	Name     string
	BudgetMS float64
	samples  []float64 // sorted
}

// NewSeries is a series of samples (ms) against budgetMS; it needs one
// sample at least.
func NewSeries(name string, budgetMS float64, samples []float64) Series {
	if len(samples) == 0 {
		panic(name + ": no samples")
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return Series{Name: name, BudgetMS: budgetMS, samples: sorted}
}

// Len is the number of samples.
func (s Series) Len() int { return len(s.samples) }

// Min is the smallest sample.
func (s Series) Min() float64 { return s.samples[0] }

// Max is the largest sample.
func (s Series) Max() float64 { return s.samples[len(s.samples)-1] }

// Median is the middle sample, or the mean of the middle two.
func (s Series) Median() float64 {
	n := len(s.samples)
	if n%2 == 1 {
		return s.samples[n/2]
	}
	return (s.samples[n/2-1] + s.samples[n/2]) / 2
}

// Percentile is the nearest-rank percentile p (0–100).
func (s Series) Percentile(p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(s.samples))))
	return s.samples[min(max(rank, 1), len(s.samples))-1]
}

// WithinBudget reports whether the median is within the budget.
func (s Series) WithinBudget() bool { return s.Median() <= s.BudgetMS }

// MsBetween is the milliseconds from one CLOCK_MONOTONIC stamp to a later one.
func MsBetween(fromNS, toNS uint64) float64 { return (float64(toNS) - float64(fromNS)) / 1e6 }

// Summary is a Markdown table of series measured in compositor.
func Summary(compositor string, series []Series) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### logis latency (%s, %s)\n\n", compositor, build)
	b.WriteString("| metric | n | median ms | p90 ms | min ms | max ms | budget ms | median/budget |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range series {
		fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %.2f | %.2f | %.2f | %.0f%% |\n", s.Name, s.Len(),
			s.Median(), s.Percentile(90), s.Min(), s.Max(), s.BudgetMS, 100*s.Median()/s.BudgetMS)
	}
	return b.String()
}

// JSONLine is one JSON line for a trend store: the series' statistics and
// samples.
func JSONLine(compositor string, s Series) string {
	var commit *string
	if sha, ok := os.LookupEnv("GITHUB_SHA"); ok {
		commit = &sha
	}
	line, _ := json.Marshal(map[string]any{
		"compositor": compositor, "metric": s.Name, "build": build, "commit": commit,
		"n": s.Len(), "median_ms": s.Median(), "p90_ms": s.Percentile(90),
		"min_ms": s.Min(), "max_ms": s.Max(), "budget_ms": s.BudgetMS, "samples_ms": s.samples,
	})
	return string(line)
}

// Record logs the summary and appends it where asked: JSON lines to
// LOGIS_LATENCY_REPORT, the table to GITHUB_STEP_SUMMARY.
func Record(t testing.TB, compositor string, series []Series) {
	t.Helper()
	table := Summary(compositor, series)
	t.Log("\n" + table)
	var lines strings.Builder
	for _, s := range series {
		lines.WriteString(JSONLine(compositor, s) + "\n")
	}
	appendTo(t, ReportEnv, lines.String())
	appendTo(t, "GITHUB_STEP_SUMMARY", table+"\n")
}

func appendTo(t testing.TB, env, text string) {
	t.Helper()
	path := os.Getenv(env)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = f.WriteString(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		t.Fatalf("cannot append to %s: %v", env, err)
	}
}

// Runs is the samples per series: LOGIS_LATENCY_RUNS, 10 by default.
func Runs(t testing.TB) int {
	t.Helper()
	v := os.Getenv(RunsEnv)
	if v == "" {
		return 10
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("%s=%s is not a count", RunsEnv, v)
	}
	return n
}

// Enforce reports whether budgets fail the run
// (LOGIS_LATENCY_BUDGETS=enforce).
func Enforce() bool { return os.Getenv(BudgetsEnv) == "enforce" }

// EnforceBudgets fails t for each series whose median is over budget, when
// Enforce says so.
func EnforceBudgets(t testing.TB, series []Series) {
	t.Helper()
	if !Enforce() {
		return
	}
	for _, s := range series {
		if !s.WithinBudget() {
			t.Errorf("%s: median %.2f ms over its %.2f ms budget", s.Name, s.Median(), s.BudgetMS)
		}
	}
}
