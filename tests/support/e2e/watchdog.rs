//! A watch on the test compositor itself: does it answer at all? A thread
//! of its own makes a `wl_display.sync` round trip on a connection of its
//! own every [`EVERY`] and keeps each that took longer than [`STALL_NS`].
//! A slow measurement that overlaps such a stall was the compositor's, not
//! the client's: a nested niri whose window the session does not draw (on
//! another workspace, scrolled out of view) gets frame callbacks about once
//! a second and blocks in its buffer swap meanwhile, answering nobody for
//! up to two seconds (ticket d49bceee).

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use logis::window::trace::monotonic_ns;

use super::TestCompositor;
use super::wire::connect_verified;

/// A round trip longer than this is a stall.
pub const STALL_NS: u64 = 50_000_000;

/// How often the compositor is asked.
const EVERY: Duration = Duration::from_millis(10);

/// The watch; it ends when dropped (or when the compositor goes).
pub struct Watchdog {
    /// Each stall as CLOCK_MONOTONIC ns (asked, answered).
    stalls: Arc<Mutex<Vec<(u64, u64)>>>,
    stop: Arc<AtomicBool>,
}

impl Watchdog {
    pub fn start(compositor: &TestCompositor) -> Self {
        let connection = connect_verified(compositor.socket(), compositor.pid())
            .unwrap_or_else(|e| panic!("{e}"));
        let stalls = Arc::new(Mutex::new(Vec::new()));
        let stop = Arc::new(AtomicBool::new(false));
        let (seen, stopped) = (Arc::clone(&stalls), Arc::clone(&stop));
        thread::spawn(move || {
            while !stopped.load(Ordering::Relaxed) {
                let asked = monotonic_ns();
                if connection.roundtrip().is_err() {
                    return;
                }
                let answered = monotonic_ns();
                if answered - asked > STALL_NS {
                    seen.lock().unwrap().push((asked, answered));
                }
                thread::sleep(EVERY);
            }
        });
        Self { stalls, stop }
    }

    /// The longest time in ms the compositor did not answer within
    /// `from..=to` (CLOCK_MONOTONIC ns), if it stalled then.
    pub fn stalled_within(&self, from: u64, to: u64) -> Option<f64> {
        self.stalls
            .lock()
            .unwrap()
            .iter()
            .map(|&(asked, answered)| (asked.max(from), answered.min(to)))
            .filter(|(a, b)| a < b)
            .map(|(a, b)| (b - a) as f64 / 1e6)
            .max_by(f64::total_cmp)
    }
}

impl Drop for Watchdog {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
    }
}
