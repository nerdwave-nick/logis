//! What a panel asks of its scene builder each frame ([`Frame`]) and what
//! it gets back ([`Drawing`]).

use super::blur::Backdrop;
use crate::icons::IconSource;
use crate::motion::Nanos;
use crate::render::Scene;

/// The frame being drawn.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Frame {
    /// The surface size in logical pixels.
    pub logical: (u32, u32),
    /// When the frame is expected on screen (CLOCK_MONOTONIC ns), the time
    /// to evaluate motion at.
    pub at: Nanos,
    /// The time between frames, in nanoseconds.
    pub interval: Nanos,
}

/// One frame's content.
#[derive(Clone, Debug, PartialEq)]
pub struct Drawing {
    /// What to draw, at rest (the window applies its open/close pose).
    pub scene: Scene,
    /// The panel's body, blurred behind when blur is on and offered.
    pub backdrop: Option<Backdrop>,
    /// Whether the content itself still moves (the selection gliding), so
    /// the next frame callback must draw again.
    pub moving: bool,
}

impl Drawing {
    /// A still scene with nothing to blur.
    pub fn still(scene: Scene) -> Self {
        Self {
            scene,
            backdrop: None,
            moving: false,
        }
    }
}

/// Builds a panel's content for each frame, its icons from the source (the
/// window's icon cache at the frame's scale).
pub type SceneFn = Box<dyn FnMut(&Frame, &mut dyn IconSource) -> Drawing>;
