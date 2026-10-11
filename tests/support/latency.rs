//! Latency series, their statistics and reports against the spec §4.10
//! budgets (see tests/latency.rs for what is measured).
#![allow(dead_code)] // shared by test crates that each use only part of it

/// Warm open: client start → first frame committed (spec §4.10).
pub const WARM_OPEN_MS: f64 = 15.0;
/// First open after the client starts the daemon (spec §4.10).
pub const FIRST_OPEN_MS: f64 = 150.0;

/// Samples of one metric in milliseconds, with its budget.
#[derive(Clone, Debug, PartialEq)]
pub struct Series {
    pub name: &'static str,
    pub budget_ms: f64,
    samples: Vec<f64>,
}

impl Series {
    /// A series of `samples` (milliseconds) against `budget_ms`.
    pub fn new(name: &'static str, budget_ms: f64, mut samples: Vec<f64>) -> Self {
        assert!(!samples.is_empty(), "{name}: no samples");
        samples.sort_by(f64::total_cmp);
        Self {
            name,
            budget_ms,
            samples,
        }
    }

    pub fn len(&self) -> usize {
        self.samples.len()
    }

    pub fn min(&self) -> f64 {
        self.samples[0]
    }

    pub fn max(&self) -> f64 {
        self.samples[self.samples.len() - 1]
    }

    pub fn median(&self) -> f64 {
        let n = self.samples.len();
        if n % 2 == 1 {
            self.samples[n / 2]
        } else {
            (self.samples[n / 2 - 1] + self.samples[n / 2]) / 2.0
        }
    }

    /// The nearest-rank percentile `p` (0–100).
    pub fn percentile(&self, p: f64) -> f64 {
        let rank = (p / 100.0 * self.samples.len() as f64).ceil() as usize;
        self.samples[rank.clamp(1, self.samples.len()) - 1]
    }

    /// Whether the median is within the budget.
    pub fn within_budget(&self) -> bool {
        self.median() <= self.budget_ms
    }
}

/// Milliseconds from one CLOCK_MONOTONIC stamp to a later one.
pub fn ms_between(from_ns: u64, to_ns: u64) -> f64 {
    (to_ns as f64 - from_ns as f64) / 1e6
}

/// A Markdown table of `series` measured in `compositor`.
pub fn summary(compositor: &str, series: &[Series]) -> String {
    let profile = if cfg!(debug_assertions) {
        "debug"
    } else {
        "release"
    };
    let mut out = format!(
        "### logis latency ({compositor}, {profile} build)\n\n\
         | metric | n | median ms | p90 ms | min ms | max ms | budget ms | median/budget |\n\
         | --- | --- | --- | --- | --- | --- | --- | --- |\n"
    );
    for s in series {
        out += &format!(
            "| {} | {} | {:.2} | {:.2} | {:.2} | {:.2} | {:.2} | {:.0}% |\n",
            s.name,
            s.len(),
            s.median(),
            s.percentile(90.0),
            s.min(),
            s.max(),
            s.budget_ms,
            100.0 * s.median() / s.budget_ms
        );
    }
    out
}

/// One JSON line for a trend store: the series' statistics and samples.
pub fn json_line(compositor: &str, series: &Series) -> String {
    serde_json::json!({
        "compositor": compositor,
        "metric": series.name,
        "build": if cfg!(debug_assertions) { "debug" } else { "release" },
        "commit": std::env::var("GITHUB_SHA").ok(),
        "n": series.len(),
        "median_ms": series.median(),
        "p90_ms": series.percentile(90.0),
        "min_ms": series.min(),
        "max_ms": series.max(),
        "budget_ms": series.budget_ms,
        "samples_ms": series.samples,
    })
    .to_string()
}

/// Prints the summary and appends it where asked: JSON lines to
/// `LOGIS_LATENCY_REPORT`, the table to `GITHUB_STEP_SUMMARY`.
pub fn record(compositor: &str, series: &[Series]) {
    use std::io::Write;
    let table = summary(compositor, series);
    eprintln!("{table}");
    let append = |var: &str, text: &str| {
        if let Some(path) = std::env::var_os(var) {
            std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(&path)
                .and_then(|mut f| f.write_all(text.as_bytes()))
                .unwrap_or_else(|e| panic!("cannot append to {var}: {e}"));
        }
    };
    let lines: String = series
        .iter()
        .map(|s| json_line(compositor, s) + "\n")
        .collect();
    append("LOGIS_LATENCY_REPORT", &lines);
    append("GITHUB_STEP_SUMMARY", &(table + "\n"));
}

/// Samples per series: `LOGIS_LATENCY_RUNS`, 10 by default.
pub fn runs() -> usize {
    std::env::var("LOGIS_LATENCY_RUNS")
        .ok()
        .map(|v| v.parse().expect("LOGIS_LATENCY_RUNS is a count"))
        .unwrap_or(10)
}

/// Whether budgets fail the run (`LOGIS_LATENCY_BUDGETS=enforce`).
pub fn enforce() -> bool {
    std::env::var("LOGIS_LATENCY_BUDGETS").is_ok_and(|v| v == "enforce")
}
