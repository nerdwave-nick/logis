//! When the frame being drawn will be seen: the time [`crate::motion`]
//! evaluates its transitions at. With wp_presentation (spec §4.2: used when
//! offered) the compositor reports when each frame reached the screen and
//! the output's refresh interval, so the next frame's moment is the next
//! refresh after now. Without it, "now" and a 60 Hz interval stand in.
//!
//! Only CLOCK_MONOTONIC reports are used: it is the clock [`Nanos`] count
//! in, and what wp_presentation's `clock_id` normally names.

use smithay_client_toolkit::reexports::client::{Connection, Dispatch, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::presentation_time::client::{
    wp_presentation::{self, WpPresentation},
    wp_presentation_feedback::{self, WpPresentationFeedback},
};

use super::Windows;
use crate::motion::Nanos;

/// The `clk_id` of CLOCK_MONOTONIC on Linux.
const CLOCK_MONOTONIC: u32 = 1;

/// A frame's moment and the interval between frames, in nanoseconds.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct FrameTime {
    /// When the frame is expected on screen.
    pub at: Nanos,
    /// The time between frames (the output's refresh interval).
    pub interval: Nanos,
}

/// The output timing learned from presentation feedback.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct FrameClock {
    last: Option<(Nanos, Nanos)>,
}

impl FrameClock {
    /// The interval assumed without presentation feedback: 60 Hz.
    pub const FALLBACK_INTERVAL: Nanos = 16_666_667;

    /// A frame reached the screen at `at`; the output refreshes every
    /// `refresh` nanoseconds (0: unknown, e.g. a variable refresh rate).
    pub fn presented(&mut self, at: Nanos, refresh: Nanos) {
        self.last = (refresh > 0).then_some((at, refresh));
    }

    /// The moment of a frame drawn at `now`: the first refresh at or after
    /// it, or `now` itself without timing.
    pub fn frame(&self, now: Nanos) -> FrameTime {
        match self.last {
            Some((at, refresh)) if now > at => {
                let steps = (now - at).div_ceil(refresh);
                FrameTime {
                    at: at + steps * refresh,
                    interval: refresh,
                }
            }
            Some((at, refresh)) => FrameTime {
                at,
                interval: refresh,
            },
            None => FrameTime {
                at: now,
                interval: Self::FALLBACK_INTERVAL,
            },
        }
    }
}

/// The bound wp_presentation and whether its clock is ours.
pub(super) struct Presentation {
    pub(super) global: WpPresentation,
    pub(super) monotonic: bool,
}

impl Dispatch<WpPresentation, ()> for Windows {
    fn event(
        windows: &mut Self,
        _: &WpPresentation,
        event: wp_presentation::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let wp_presentation::Event::ClockId { clk_id } = event
            && let Some(p) = windows.presentation_mut()
        {
            p.monotonic = clk_id == CLOCK_MONOTONIC;
        }
    }
}

impl Dispatch<WpPresentationFeedback, ()> for Windows {
    fn event(
        windows: &mut Self,
        _: &WpPresentationFeedback,
        event: wp_presentation_feedback::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let wp_presentation_feedback::Event::Presented {
            tv_sec_hi,
            tv_sec_lo,
            tv_nsec,
            refresh,
            ..
        } = event
            && windows.presentation_mut().is_some_and(|p| p.monotonic)
        {
            let secs = (u64::from(tv_sec_hi) << 32) | u64::from(tv_sec_lo);
            let at = secs * 1_000_000_000 + u64::from(tv_nsec);
            windows.clock_mut().presented(at, u64::from(refresh));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const MS: Nanos = 1_000_000;

    #[test]
    fn without_feedback_a_frame_is_seen_now_at_60_hz() {
        let clock = FrameClock::default();
        assert_eq!(
            clock.frame(5 * MS),
            FrameTime {
                at: 5 * MS,
                interval: FrameClock::FALLBACK_INTERVAL
            }
        );
    }

    #[test]
    fn with_feedback_a_frame_is_seen_at_the_next_refresh() {
        let mut clock = FrameClock::default();
        clock.presented(100 * MS, 8 * MS);
        assert_eq!(clock.frame(101 * MS).at, 108 * MS);
        assert_eq!(clock.frame(108 * MS).at, 108 * MS);
        assert_eq!(clock.frame(130 * MS).at, 132 * MS, "refreshes later");
        assert_eq!(clock.frame(130 * MS).interval, 8 * MS);
    }

    #[test]
    fn a_report_from_the_future_of_now_is_its_own_moment() {
        let mut clock = FrameClock::default();
        clock.presented(100 * MS, 8 * MS);
        assert_eq!(clock.frame(99 * MS).at, 100 * MS);
    }

    #[test]
    fn an_unknown_refresh_forgets_the_timing() {
        let mut clock = FrameClock::default();
        clock.presented(100 * MS, 8 * MS);
        clock.presented(120 * MS, 0);
        assert_eq!(clock.frame(130 * MS).at, 130 * MS);
    }
}
