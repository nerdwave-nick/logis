package latency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests of the latency statistics and reports.

func TestStatisticsUseTheNearestRank(t *testing.T) {
	s := NewSeries("x", 10, []float64{4, 1, 3, 2, 5})
	if s.Min() != 1 || s.Median() != 3 || s.Max() != 5 || s.Percentile(90) != 5 {
		t.Errorf("min %v median %v max %v p90 %v", s.Min(), s.Median(), s.Max(), s.Percentile(90))
	}
	if even := NewSeries("y", 10, []float64{1, 2, 3, 4}); even.Median() != 2.5 {
		t.Errorf("even median %v", even.Median())
	}
}

func TestMillisecondsBetweenMonotonicStamps(t *testing.T) {
	if got := MsBetween(1_000_000, 8_500_000); got != 7.5 {
		t.Errorf("%v", got)
	}
}

func TestTheSummaryShowsEachSeriesAgainstItsBudget(t *testing.T) {
	s := NewSeries("show → first commit", 150, []float64{30, 60, 45})
	table := Summary("niri-nested", []Series{s})
	for _, want := range []string{"| show → first commit | 3 | 45.00 |", "| 150.00 | 30% |", "niri-nested"} {
		if !strings.Contains(table, want) {
			t.Errorf("no %q in\n%s", want, table)
		}
	}
	if !s.WithinBudget() {
		t.Error("45 of 150 is over budget")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(JSONLine("niri-nested", s)), &v); err != nil {
		t.Fatal(err)
	}
	if v["metric"] != "show → first commit" || v["median_ms"] != 45.0 || v["budget_ms"] != 150.0 ||
		v["compositor"] != "niri-nested" || len(v["samples_ms"].([]any)) != 3 {
		t.Errorf("%v", v)
	}
}

func TestAMedianOverBudgetIsOutOfBudget(t *testing.T) {
	if NewSeries("x", 10, []float64{9, 11, 12}).WithinBudget() {
		t.Error("median 11 within 10")
	}
}

func TestRoundTripsAreTheSyncRequestsOfAWaylandDebugLog(t *testing.T) {
	log := []string{
		"[ 1.0] {mesa formats query} \x1b[31m\x1b[0m -> \x1b[34mwl_display\x1b[35m#1\x1b[36m.sync\x1b[0m(new id wl_callback#29)",
		"[ 1.1] -> wl_display#1.get_registry(new id wl_registry#28)",
		"[ 1.2] wl_callback#29.done(1234)",
		"[ 1.3]  -> wl_display#1.sync(new id wl_callback#30)",
		"logis-trace: 99 show 2",
	}
	if n := RoundTrips(log); n != 2 {
		t.Errorf("%d round trips", n)
	}
}

func TestASlowOpenNamesTheCompositorOnlyWhenItsStallMadeItSlow(t *testing.T) {
	theirs := SlowOpen(1902.9, 1996.9, true)
	if !strings.Contains(theirs, "1902.9 ms") || !strings.Contains(theirs, "answered nobody for 1996.9 ms") {
		t.Error(theirs)
	}
	// Without the stall this open would still have been slow.
	busy := SlowOpen(666.7, 70, true)
	if !strings.Contains(busy, "stalled only 70.0 ms") || !strings.Contains(busy, "logis, or the machine under load, was slow") {
		t.Error(busy)
	}
	ours := SlowOpen(120, 0, false)
	if !strings.Contains(ours, "120.0 ms") || !strings.Contains(ours, "answered throughout") {
		t.Error(ours)
	}
}

func TestRecordAppendsJSONLinesAndTheTableWhereAsked(t *testing.T) {
	dir := t.TempDir()
	report, summary := filepath.Join(dir, "latency.jsonl"), filepath.Join(dir, "summary.md")
	t.Setenv(ReportEnv, report)
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	s := []Series{NewSeries("a", 1, []float64{1}), NewSeries("b", 2, []float64{2})}
	Record(t, "sway-headless", s)
	Record(t, "sway-headless", s[:1])
	lines, _ := os.ReadFile(report)
	if n := strings.Count(string(lines), "\n"); n != 3 {
		t.Errorf("%d JSON lines:\n%s", n, lines)
	}
	if table, _ := os.ReadFile(summary); strings.Count(string(table), "### logis latency") != 2 {
		t.Errorf("summary:\n%s", table)
	}
}

func TestRunsAndEnforcementComeFromTheEnvironment(t *testing.T) {
	t.Setenv(RunsEnv, "")
	t.Setenv(BudgetsEnv, "")
	if Runs(t) != 10 || Enforce() {
		t.Errorf("defaults: %d runs, enforce %v", Runs(t), Enforce())
	}
	t.Setenv(RunsEnv, "20")
	t.Setenv(BudgetsEnv, "enforce")
	if Runs(t) != 20 || !Enforce() {
		t.Errorf("set: %d runs, enforce %v", Runs(t), Enforce())
	}
}
