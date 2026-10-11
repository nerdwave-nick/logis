//! Motion and blur on [`Windows`]: the parameters in force, the animated
//! close, and the optional globals that pace and blur the panels.

use std::time::{Duration, Instant};

use smithay_client_toolkit::reexports::client::QueueHandle;

use super::Windows;
use crate::motion::{Motion, Nanos};
use crate::window::clock::{FrameClock, Presentation};
use crate::window::trace::{emit, monotonic_ns};

/// How long a closing panel may outlast its transition when no frame
/// callback comes (its output went dark): then it goes without one.
const CLOSE_GRACE: Nanos = 100_000_000;

impl Windows {
    /// The motion of the panels shown from now on (and of their close).
    pub fn set_motion(&mut self, motion: Motion) {
        self.motion = motion;
    }

    /// The motion in force.
    pub fn motion(&self) -> &Motion {
        &self.motion
    }

    /// Blurs behind the panels (when the compositor offers
    /// ext-background-effect), the visible one included, or stops.
    pub fn set_blur(&mut self, blur: bool, qh: &QueueHandle<Self>) {
        self.blur = blur;
        let effects = self.effects.as_ref().filter(|_| blur);
        if let Some(panel) = &mut self.panel {
            panel.set_blur(effects, qh);
        }
    }

    /// Closes the visible panel: at once without motion (or if it was never
    /// shown), else it hands keyboard and pointer back at once and fades
    /// out over the close transition. A later [`Self::show`] cuts that
    /// short. The caller answers its session before calling this; nothing
    /// here waits.
    pub fn close(&mut self) {
        self.keyboard.reset();
        let Some(mut panel) = self.panel.take() else {
            return;
        };
        if self.motion.close.duration.is_zero() || !panel.was_shown() {
            return;
        }
        panel.close(&self.motion, monotonic_ns(), &self.shell.compositor);
        self.closing = Some(panel);
    }

    /// When the closing panel must go even without a frame callback.
    pub(in crate::window) fn motion_deadline(&self) -> Option<Instant> {
        let finish = self.closing.as_ref()?.finish_time()? + CLOSE_GRACE;
        let left = finish.saturating_sub(monotonic_ns());
        Some(Instant::now() + Duration::from_nanos(left))
    }

    /// Drops a closing panel that outlasted its transition and grace (its
    /// frame callbacks stopped).
    pub(in crate::window) fn expire_closing(&mut self) {
        let now = monotonic_ns();
        let overdue = self
            .closing
            .as_ref()
            .and_then(|p| p.finish_time())
            .is_some_and(|end| now >= end + CLOSE_GRACE);
        if overdue && self.closing.as_ref().is_some_and(|p| p.finished(now)) {
            self.closing = None;
            emit(format_args!("faded"));
        }
    }

    pub(in crate::window) fn presentation_mut(&mut self) -> Option<&mut Presentation> {
        self.presentation.as_mut()
    }

    pub(in crate::window) fn clock_mut(&mut self) -> &mut FrameClock {
        &mut self.clock
    }
}
