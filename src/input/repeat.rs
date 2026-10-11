//! Client-side key repeat (wl_keyboard before version 10): the compositor
//! announces a rate and a delay, and the client repeats the last pressed
//! key itself. This is only a deadline: the event loop sleeps until
//! [`Repeat::deadline`] while a key is held and has no timeout otherwise,
//! so nothing wakes the CPU when no key is held.

use std::time::{Duration, Instant};

/// More repeats than this are never emitted at once; a loop that fell
/// further behind (a stalled frame, a suspend) skips ahead instead.
const MAX_BURST: u32 = 4;

/// The repeat rate and the held key.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Repeat {
    /// `None`: repeat is disabled (rate 0).
    pace: Option<Pace>,
    held: Option<Held>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Pace {
    delay: Duration,
    interval: Duration,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Held {
    code: u32,
    next: Instant,
}

impl Default for Repeat {
    /// The X server's defaults (25 per second after 600 ms), for a
    /// wl_keyboard older than version 4, which sends no repeat info.
    fn default() -> Self {
        let mut repeat = Self {
            pace: None,
            held: None,
        };
        repeat.set_rate(25, 600);
        repeat
    }
}

impl Repeat {
    /// Applies the compositor's repeat info: `rate` per second after
    /// `delay` ms; a rate of 0 (or less) disables repeat. A key held at the
    /// time stops repeating.
    pub fn set_rate(&mut self, rate: i32, delay: i32) {
        self.held = None;
        self.pace = (rate > 0).then(|| Pace {
            delay: Duration::from_millis(delay.max(0) as u64),
            interval: Duration::from_secs(1) / rate as u32,
        });
    }

    /// Whether repeat is enabled.
    pub fn enabled(&self) -> bool {
        self.pace.is_some()
    }

    /// Key `code` was pressed at `now`: it repeats from now on, replacing
    /// any other.
    pub fn press(&mut self, code: u32, now: Instant) {
        self.held = self.pace.map(|pace| Held {
            code,
            next: now + pace.delay,
        });
    }

    /// Key `code` was released; it stops repeating if it was the one.
    pub fn release(&mut self, code: u32) {
        if self.held.is_some_and(|h| h.code == code) {
            self.held = None;
        }
    }

    /// Stops any repeat (focus left, keymap changed, panel hidden).
    pub fn reset(&mut self) {
        self.held = None;
    }

    /// When the next repeat is due; `None` when nothing repeats.
    pub fn deadline(&self) -> Option<Instant> {
        self.held.map(|h| h.next)
    }

    /// The held key and how many repeats are due at `now` (at most
    /// [`MAX_BURST`]), or `None` if none is.
    pub fn due(&mut self, now: Instant) -> Option<(u32, u32)> {
        let pace = self.pace?;
        let held = self.held.as_mut()?;
        let mut count = 0;
        while held.next <= now && count < MAX_BURST {
            held.next += pace.interval;
            count += 1;
        }
        if held.next <= now {
            held.next = now + pace.interval;
        }
        (count > 0).then_some((held.code, count))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ms(n: u64) -> Duration {
        Duration::from_millis(n)
    }

    /// 25 per second (40 ms) after 300 ms.
    fn repeat() -> Repeat {
        let mut r = Repeat::default();
        r.set_rate(25, 300);
        r
    }

    #[test]
    fn nothing_is_due_and_nothing_waits_while_no_key_is_held() {
        let mut r = repeat();
        let t = Instant::now();
        assert_eq!(r.deadline(), None);
        assert_eq!(r.due(t + ms(10_000)), None);
    }

    #[test]
    fn a_held_key_repeats_after_the_delay_at_the_rate() {
        let mut r = repeat();
        let t = Instant::now();
        r.press(30, t);
        assert_eq!(r.deadline(), Some(t + ms(300)));
        assert_eq!(r.due(t + ms(299)), None);
        assert_eq!(r.due(t + ms(300)), Some((30, 1)));
        assert_eq!(r.deadline(), Some(t + ms(340)));
        assert_eq!(r.due(t + ms(339)), None);
        assert_eq!(r.due(t + ms(420)), Some((30, 3)));
        assert_eq!(r.deadline(), Some(t + ms(460)));
    }

    #[test]
    fn a_late_loop_gets_a_bounded_burst_and_skips_ahead() {
        let mut r = repeat();
        let t = Instant::now();
        r.press(30, t);
        let late = t + ms(60_000);
        assert_eq!(r.due(late), Some((30, MAX_BURST)));
        assert_eq!(r.deadline(), Some(late + ms(40)));
    }

    #[test]
    fn rate_zero_disables_repeat() {
        let mut r = repeat();
        let t = Instant::now();
        r.set_rate(0, 300);
        assert!(!r.enabled());
        r.press(30, t);
        assert_eq!(r.deadline(), None);
        assert_eq!(r.due(t + ms(10_000)), None);
    }

    #[test]
    fn releasing_the_held_key_stops_it_and_another_key_does_not() {
        let mut r = repeat();
        let t = Instant::now();
        r.press(30, t);
        r.release(31);
        assert!(r.deadline().is_some());
        r.release(30);
        assert_eq!(r.deadline(), None);
    }

    #[test]
    fn a_new_press_replaces_the_held_key() {
        let mut r = repeat();
        let t = Instant::now();
        r.press(30, t);
        r.press(31, t + ms(100));
        assert_eq!(r.due(t + ms(400)), Some((31, 1)));
    }

    #[test]
    fn reset_and_new_repeat_info_stop_the_held_key() {
        let mut r = repeat();
        let t = Instant::now();
        r.press(30, t);
        r.reset();
        assert_eq!(r.deadline(), None);
        r.press(30, t);
        r.set_rate(30, 200);
        assert_eq!(r.deadline(), None);
    }

    #[test]
    fn without_repeat_info_the_x_defaults_apply() {
        let mut r = Repeat::default();
        let t = Instant::now();
        r.press(30, t);
        assert_eq!(r.deadline(), Some(t + ms(600)));
        assert_eq!(r.due(t + ms(640)), Some((30, 2)));
    }
}
