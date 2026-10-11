//! The test trace: a test-only observable of what the window saw, enabled by
//! `LOGIS_TEST_TRACE=1` and silent otherwise. Each event is one stderr line,
//! `logis-trace: <CLOCK_MONOTONIC ns> <event>`, so a harness can wait for
//! keyboard focus before typing and time commits against its own clock.
//!
//! Events: `keyboard-enter`, `keyboard-leave`, `keymap` (one compiled),
//! `repeat-info <rate> <delay>`, `key <evdev code> pressed|released`,
//! `key <evdev code> repeated` (a client-side repeat), `modifiers
//! <depressed> <latched> <locked> <group>` and `commit` (a frame was
//! presented and its surface committed). The debug panel adds what each
//! key did: `query <JSON string>`, `select <row>`, `accept <row>` and
//! `dismiss`.

use std::fmt;
use std::sync::OnceLock;

/// The variable that enables the trace.
pub const ENV: &str = "LOGIS_TEST_TRACE";

/// The prefix of every trace line.
pub const PREFIX: &str = "logis-trace:";

/// Whether a value of [`ENV`] enables the trace: set, non-empty, not 0.
pub fn enabled_by(value: Option<&std::ffi::OsStr>) -> bool {
    value.is_some_and(|v| !v.is_empty() && v != "0")
}

/// The trace line for `event` at `ns`.
pub fn line(ns: u64, event: fmt::Arguments<'_>) -> String {
    format!("{PREFIX} {ns} {event}")
}

/// CLOCK_MONOTONIC in nanoseconds, the clock harnesses time against.
pub fn monotonic_ns() -> u64 {
    let t = rustix::time::clock_gettime(rustix::time::ClockId::Monotonic);
    t.tv_sec as u64 * 1_000_000_000 + t.tv_nsec as u64
}

/// Emits `event` when the trace is enabled. A closed stderr is ignored.
pub fn emit(event: fmt::Arguments<'_>) {
    static ENABLED: OnceLock<bool> = OnceLock::new();
    if *ENABLED.get_or_init(|| enabled_by(std::env::var_os(ENV).as_deref())) {
        use std::io::Write;
        let _ = writeln!(std::io::stderr(), "{}", line(monotonic_ns(), event));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_line_carries_the_prefix_the_time_and_the_event() {
        let l = line(1_234_567_890, format_args!("key {} {}", 30, "pressed"));
        assert_eq!(l, "logis-trace: 1234567890 key 30 pressed");
    }

    #[test]
    fn the_trace_is_on_only_for_a_set_non_zero_value() {
        assert!(enabled_by(Some("1".as_ref())));
        assert!(!enabled_by(Some("0".as_ref())));
        assert!(!enabled_by(Some("".as_ref())));
        assert!(!enabled_by(None));
    }

    #[test]
    fn the_monotonic_clock_moves_forward() {
        let a = monotonic_ns();
        let b = monotonic_ns();
        assert!(a > 0 && b >= a);
    }
}
