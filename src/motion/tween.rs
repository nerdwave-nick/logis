//! Values moving over time: a [`Tween`] between two values along a
//! [`Curve`], the [`Pose`] a whole panel takes during its open and close
//! [`Transition`], and the [`Glide`] of the selection band.
//!
//! Times are CLOCK_MONOTONIC nanoseconds ([`Nanos`]), the clock that
//! wp_presentation reports in and the test trace stamps with. A change
//! takes effect one frame interval (`lead`) before the frame that first
//! shows it, since it was asked for between frames: so that frame already
//! shows movement, rather than the old state once more.

use super::{Curve, Nanos};

/// Values a tween can move between.
pub trait Lerp: Copy {
    /// The value `t` of the way from `self` to `to` (`t` in 0..=1).
    fn lerp(self, to: Self, t: f32) -> Self;
}

impl Lerp for f32 {
    fn lerp(self, to: Self, t: f32) -> Self {
        // The ends are exact, so a finished move rests exactly on its end.
        if t >= 1.0 { to } else { self + (to - self) * t }
    }
}

/// A move from one value to another along a curve, from a start time.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Tween<T> {
    from: T,
    to: T,
    curve: Curve,
    start: Nanos,
}

impl<T: Lerp> Tween<T> {
    /// From `from` to `to` along `curve`, starting at `start`.
    pub fn new(from: T, to: T, curve: Curve, start: Nanos) -> Self {
        Self {
            from,
            to,
            curve,
            start,
        }
    }

    /// The value at `time`: `from` before the start, `to` after the end.
    pub fn at(&self, time: Nanos) -> T {
        let t = self.curve.progress(time.saturating_sub(self.start));
        self.from.lerp(self.to, t)
    }

    /// Whether the move is over at `time`.
    pub fn done(&self, time: Nanos) -> bool {
        self.curve.done(time.saturating_sub(self.start))
    }

    /// Where the move ends.
    pub fn end(&self) -> T {
        self.to
    }

    /// When the move is over.
    pub fn end_time(&self) -> Nanos {
        self.start
            .saturating_add(self.curve.duration.as_nanos() as Nanos)
    }
}

/// How a whole panel is shown: its opacity and its offset from where it
/// rests, in logical pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Pose {
    /// Multiplies every colour's alpha (0..=1).
    pub opacity: f32,
    /// Added to every position (logical pixels; +y is down).
    pub offset: [f32; 2],
}

impl Pose {
    /// At rest: fully opaque, in place.
    pub const REST: Self = Self {
        opacity: 1.0,
        offset: [0.0, 0.0],
    };

    /// Whether this is the rest pose (drawing needs no transform).
    pub fn is_rest(self) -> bool {
        self == Self::REST
    }
}

impl Default for Pose {
    fn default() -> Self {
        Self::REST
    }
}

impl Lerp for Pose {
    fn lerp(self, to: Self, t: f32) -> Self {
        Self {
            opacity: self.opacity.lerp(to.opacity, t),
            offset: [
                self.offset[0].lerp(to.offset[0], t),
                self.offset[1].lerp(to.offset[1], t),
            ],
        }
    }
}

/// Where a panel is in its life: opening into its rest pose, resting, or
/// closing out of it.
#[derive(Clone, Copy, Debug, PartialEq)]
enum Phase {
    /// Until the first frame is on screen, which starts the move to rest.
    Opening { from: Pose, curve: Curve },
    /// Moving (opening or closing) or at rest (`None`).
    Moving(Option<Tween<Pose>>),
}

/// A panel's open and close transition.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Transition {
    phase: Phase,
    closing: bool,
    last: Pose,
}

impl Transition {
    /// A panel that opens from `from` to rest along `curve`, starting with
    /// its first frame on screen.
    pub fn opening(from: Pose, curve: Curve) -> Self {
        Self {
            phase: Phase::Opening { from, curve },
            closing: false,
            last: from,
        }
    }

    /// A panel already at rest (no open transition).
    pub fn resting() -> Self {
        Self {
            phase: Phase::Moving(None),
            closing: false,
            last: Pose::REST,
        }
    }

    /// Starts closing at `now`, from the pose last drawn to `to`.
    pub fn close(&mut self, to: Pose, curve: Curve, now: Nanos) {
        self.closing = true;
        self.phase = Phase::Moving(Some(Tween::new(self.last, to, curve, now)));
    }

    /// The last frame drawn is on screen from `at`. The open's clock starts
    /// here, one interval (`lead`) back, so however long the first frame
    /// took to reach the screen, the whole open is seen after it.
    pub fn presented(&mut self, at: Nanos, lead: Nanos) {
        if let Phase::Opening { from, curve } = self.phase {
            let start = at.saturating_sub(lead);
            self.phase = Phase::Moving(Some(Tween::new(from, Pose::REST, curve, start)));
        }
    }

    /// Whether [`Self::close`] was called.
    pub fn is_closing(&self) -> bool {
        self.closing
    }

    /// The pose of a frame shown at `time`. Until its first frame is on
    /// screen ([`Self::presented`]), an opening panel is shown one interval
    /// (`lead`) into its open.
    pub fn pose(&mut self, time: Nanos, lead: Nanos) -> Pose {
        if let Phase::Opening { from, curve } = self.phase {
            let start = time.saturating_sub(lead);
            self.last = Tween::new(from, Pose::REST, curve, start).at(time);
        }
        if let Phase::Moving(Some(tween)) = self.phase {
            self.last = tween.at(time);
            if tween.done(time) && !self.closing {
                self.phase = Phase::Moving(None);
            }
        }
        self.last
    }

    /// Whether frames after `time` still move.
    pub fn moving(&self, time: Nanos) -> bool {
        match self.phase {
            Phase::Opening { .. } => true,
            Phase::Moving(tween) => tween.is_some_and(|t| !t.done(time)),
        }
    }

    /// Whether a closing panel has finished at `time` (it can go).
    pub fn finished(&self, time: Nanos) -> bool {
        self.closing && !self.moving(time)
    }

    /// When a closing panel finishes, if it is closing.
    pub fn finish_time(&self) -> Option<Nanos> {
        match self.phase {
            Phase::Moving(Some(tween)) if self.closing => Some(tween.end_time()),
            _ => None,
        }
    }
}

/// A value that glides to each new target instead of jumping: the
/// selection band's top. The first target, and the first after none, is
/// taken at once.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Glide {
    curve: Curve,
    target: Option<f32>,
    tween: Option<Tween<f32>>,
}

impl Glide {
    /// A glide along `curve`, with no target yet.
    pub fn new(curve: Curve) -> Self {
        Self {
            curve,
            target: None,
            tween: None,
        }
    }

    /// The value for a frame shown at `time` while the target is `target`,
    /// and whether later frames still move. A new target starts a glide
    /// from the value at `time - lead`.
    pub fn toward(&mut self, target: Option<f32>, time: Nanos, lead: Nanos) -> (Option<f32>, bool) {
        let Some(to) = target else {
            *self = Self::new(self.curve);
            return (None, false);
        };
        if self.target != Some(to) {
            let start = time.saturating_sub(lead);
            let from = match (self.tween, self.target) {
                (Some(tween), _) => tween.at(start),
                (None, Some(at)) => at,
                (None, None) => to,
            };
            self.target = Some(to);
            self.tween = (from != to).then(|| Tween::new(from, to, self.curve, start));
        }
        match self.tween {
            Some(tween) if tween.done(time) => {
                self.tween = None;
                (Some(to), false)
            }
            Some(tween) => (Some(tween.at(time)), true),
            None => (Some(to), false),
        }
    }
}

#[cfg(test)]
mod tests;
