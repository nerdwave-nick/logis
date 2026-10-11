//! Motion (spec §4.3, v1): the cogitator's open and close transitions and
//! the selection band's movement. Pure values and arithmetic; the window
//! paces them by its frame callbacks (and wp_presentation timing when the
//! compositor offers it), and draws nothing once they have settled.
//!
//! Durations and curves are [`Motion`] parameters with defaults; the theme
//! slice may expose them. `LOGIS_MOTION` ([`Motion::from_env`]) turns
//! motion off (`off`, for deterministic tests and the motion-averse) or
//! slows it down by a factor (`4` runs every transition four times longer).

mod apply;
mod easing;
mod tween;

use std::time::Duration;

pub use easing::Easing;
pub use tween::{Glide, Lerp, Pose, Transition, Tween};

/// A point in time: CLOCK_MONOTONIC in nanoseconds.
pub type Nanos = u64;

/// How long a transition takes and how its progress is eased.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Curve {
    /// From start to end; zero means "at once".
    pub duration: Duration,
    /// How progress in time maps to progress in value.
    pub easing: Easing,
}

impl Curve {
    /// No transition: always at the end.
    pub const INSTANT: Self = Self {
        duration: Duration::ZERO,
        easing: Easing::Linear,
    };

    /// `easing` over `millis` milliseconds.
    pub const fn millis(millis: u64, easing: Easing) -> Self {
        Self {
            duration: Duration::from_millis(millis),
            easing,
        }
    }

    /// The eased progress (0..=1) `elapsed` nanoseconds after the start.
    pub fn progress(&self, elapsed: Nanos) -> f32 {
        let total = self.duration.as_nanos();
        if total == 0 {
            return 1.0;
        }
        let t = (u128::from(elapsed).min(total) as f64 / total as f64) as f32;
        self.easing.apply(t)
    }

    /// Whether the transition is over `elapsed` nanoseconds after its start.
    pub fn done(&self, elapsed: Nanos) -> bool {
        u128::from(elapsed) >= self.duration.as_nanos()
    }
}

/// Every motion parameter of the cogitator.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Motion {
    /// The open transition, from [`Self::open_from`] to rest. It starts
    /// with the window's first frame, which already shows the panel (the
    /// "first frame" of the latency budget is that first commit).
    pub open: Curve,
    /// The pose the panel opens from.
    pub open_from: Pose,
    /// The close transition, from the pose last shown to
    /// [`Self::close_to`]; the session's answer is sent before it starts.
    pub close: Curve,
    /// The pose the panel closes into (then it is gone).
    pub close_to: Pose,
    /// How the selection band moves to a newly selected row.
    pub selection: Curve,
}

impl Motion {
    /// The variable read by [`Self::from_env`].
    pub const ENV: &str = "LOGIS_MOTION";

    /// No motion at all: panels appear and vanish at once, the band jumps.
    pub const OFF: Self = Self {
        open: Curve::INSTANT,
        open_from: Pose::REST,
        close: Curve::INSTANT,
        close_to: Pose::REST,
        selection: Curve::INSTANT,
    };

    /// Whether nothing ever moves.
    pub fn is_off(&self) -> bool {
        [self.open, self.close, self.selection]
            .iter()
            .all(|c| c.duration.is_zero())
    }

    /// Every duration multiplied by `factor` (0 turns motion off).
    pub fn slowed(self, factor: f64) -> Self {
        let scale = |c: Curve| Curve {
            duration: c.duration.mul_f64(factor.max(0.0)),
            ..c
        };
        Self {
            open: scale(self.open),
            close: scale(self.close),
            selection: scale(self.selection),
            ..self
        }
    }

    /// The motion `LOGIS_MOTION` asks for: unset or empty for the default,
    /// `off` (or `0`) for none, a positive factor to slow it down.
    pub fn from_env_value(value: Option<&std::ffi::OsStr>) -> Result<Self, String> {
        let Some(value) = value.filter(|v| !v.is_empty()) else {
            return Ok(Self::default());
        };
        let text = value.to_string_lossy();
        if text == "off" {
            return Ok(Self::OFF);
        }
        match text.parse::<f64>() {
            Ok(0.0) => Ok(Self::OFF),
            Ok(factor) if factor.is_finite() && factor > 0.0 && factor <= 100.0 => {
                Ok(Self::default().slowed(factor))
            }
            _ => Err(format!(
                "{}={text} is no rite of motion: use 'off' or a factor such as 4",
                Self::ENV
            )),
        }
    }

    /// [`Self::from_env_value`] of the process environment.
    pub fn from_env() -> Result<Self, String> {
        Self::from_env_value(std::env::var_os(Self::ENV).as_deref())
    }
}

impl Default for Motion {
    /// Quick and calm: the panel rises into place and fades in over 150 ms
    /// (starting at 40 % so its first frame is already plainly there),
    /// sinks and fades out over 110 ms, and the band glides in 90 ms.
    fn default() -> Self {
        Self {
            open: Curve::millis(150, Easing::EASE_OUT),
            open_from: Pose {
                opacity: 0.4,
                offset: [0.0, 10.0],
            },
            close: Curve::millis(110, Easing::EASE_IN),
            close_to: Pose {
                opacity: 0.0,
                offset: [0.0, 6.0],
            },
            selection: Curve::millis(90, Easing::EASE_OUT),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsStr;

    const MS: Nanos = 1_000_000;

    #[test]
    fn a_curve_progresses_from_0_to_1_over_its_duration() {
        let c = Curve::millis(100, Easing::Linear);
        assert_eq!(c.progress(0), 0.0);
        assert!((c.progress(25 * MS) - 0.25).abs() < 1e-6);
        assert_eq!(c.progress(100 * MS), 1.0);
        assert_eq!(c.progress(500 * MS), 1.0);
        assert!(!c.done(99 * MS));
        assert!(c.done(100 * MS));
    }

    #[test]
    fn an_instant_curve_is_always_done() {
        assert_eq!(Curve::INSTANT.progress(0), 1.0);
        assert!(Curve::INSTANT.done(0));
    }

    #[test]
    fn the_default_moves_and_off_does_not() {
        assert!(!Motion::default().is_off());
        assert!(Motion::OFF.is_off());
        assert!(Motion::default().slowed(0.0).is_off());
    }

    #[test]
    fn slowing_multiplies_every_duration_and_keeps_the_poses() {
        let slow = Motion::default().slowed(4.0);
        assert_eq!(slow.open.duration, Duration::from_millis(600));
        assert_eq!(slow.close.duration, Duration::from_millis(440));
        assert_eq!(slow.selection.duration, Duration::from_millis(360));
        assert_eq!(slow.open_from, Motion::default().open_from);
    }

    #[test]
    fn the_environment_turns_motion_off_or_slows_it() {
        let from = |v: Option<&str>| Motion::from_env_value(v.map(OsStr::new));
        assert_eq!(from(None), Ok(Motion::default()));
        assert_eq!(from(Some("")), Ok(Motion::default()));
        assert_eq!(from(Some("off")), Ok(Motion::OFF));
        assert_eq!(from(Some("0")), Ok(Motion::OFF));
        assert_eq!(from(Some("2")), Ok(Motion::default().slowed(2.0)));
        for bad in ["fast", "-1", "inf", "NaN", "1000"] {
            let e = from(Some(bad)).unwrap_err();
            assert!(e.contains("LOGIS_MOTION"), "{bad}: {e}");
        }
    }

    #[test]
    fn the_default_open_starts_visibly_and_ends_at_rest() {
        let m = Motion::default();
        assert!(
            m.open_from.opacity >= 0.3,
            "the first frame shows the panel"
        );
        assert_eq!(m.close_to.opacity, 0.0, "a closed panel is gone");
    }
}
