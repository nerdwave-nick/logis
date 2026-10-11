//! The pointer as the visible session sees it (spec §3 story 16): the
//! `wl_pointer` events in, [`Point`]s out. The seat forwards the protocol
//! events; this model groups them into frames and decides what they mean.
//!
//! - Position: motion reports where the pointer is, in the surface's
//!   logical pixels; leave ends it. Enter only records the position: a
//!   window that opens under a pointer at rest must not move the selection
//!   (nor undo a preselection), so only a pointer that moves points.
//! - Buttons: the primary button's press and release are reported where
//!   they happen (the list accepts a row pressed and released on the same
//!   item); the middle button pastes the primary selection, as it does
//!   everywhere on a Unix desktop.
//! - Scrolling: the vertical axis of one frame becomes one [`Scroll`]. A
//!   wheel reports in 120ths of a notch: `axis_value120` (wl_pointer v8,
//!   high-resolution wheels send fractions), else `axis_discrete` (v5–7)
//!   in whole notches. A touchpad finger, a continuous device, or a wheel
//!   without either reports logical pixels; `axis_stop` ends a finger's
//!   scroll. Whatever the `axis_source`, whole notches are used when the
//!   compositor counts them, pixels otherwise. Horizontal scrolling means
//!   nothing to a list.
//!
//! Before wl_pointer v5 there are no frames: the seat ends a frame after
//! every axis event itself.

use super::{Action, Selection};

/// The primary (left) button (linux/input-event-codes.h).
pub const BTN_LEFT: u32 = 0x110;
/// The middle button: pastes the primary selection.
pub const BTN_MIDDLE: u32 = 0x112;

/// What the pointer did over the visible session, in surface-local
/// logical pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Point {
    /// The pointer moved to `(x, y)`.
    Moved(f64, f64),
    /// The pointer left the window.
    Left,
    /// The primary button went down at `(x, y)`.
    Pressed(f64, f64),
    /// The primary button came up at `(x, y)`.
    Released(f64, f64),
    /// The list scrolls, the pointer at `(x, y)`.
    Scrolled(Scroll, f64, f64),
}

/// One frame's vertical scroll; positive moves towards later items.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Scroll {
    /// A wheel, in 120ths of a notch.
    Wheel(i32),
    /// A finger or a continuous device, in logical pixels.
    Pixels(f64),
    /// The finger lifted: a continuous scroll ended.
    Stop,
}

/// The vertical axis events of the frame being collected.
#[derive(Clone, Copy, Debug, Default)]
struct Frame {
    value: Option<f64>,
    value120: i32,
    discrete: i32,
    stop: bool,
}

/// The pointer's position over the visible window and the frame in progress.
#[derive(Clone, Debug, Default)]
pub struct Pointer {
    at: Option<(f64, f64)>,
    frame: Frame,
}

impl Pointer {
    /// A pointer that is not over the window.
    pub fn new() -> Self {
        Self::default()
    }

    /// Where the pointer is, while it is over the window.
    pub fn position(&self) -> Option<(f64, f64)> {
        self.at
    }

    /// Forgets the position and any frame in progress (the window was
    /// shown or hidden; the compositor sends a new enter for a new one).
    pub fn reset(&mut self) {
        *self = Self::default();
    }

    /// The pointer entered the window at `(x, y)`: recorded, not reported.
    pub fn enter(&mut self, x: f64, y: f64) {
        self.frame = Frame::default();
        self.at = Some((x, y));
    }

    /// The pointer moved to `(x, y)`; nothing while it is not over the window.
    pub fn motion(&mut self, x: f64, y: f64) -> Option<Point> {
        let at = self.at.as_mut()?;
        *at = (x, y);
        Some(Point::Moved(x, y))
    }

    /// The pointer left the window.
    pub fn leave(&mut self) -> Option<Point> {
        let was = self.at.take();
        self.frame = Frame::default();
        was.map(|_| Point::Left)
    }

    /// A button was pressed or released (evdev code `button`).
    pub fn button(&mut self, button: u32, pressed: bool) -> Option<Action> {
        let (x, y) = self.at?;
        match (button, pressed) {
            (BTN_LEFT, true) => Some(Action::Point(Point::Pressed(x, y))),
            (BTN_LEFT, false) => Some(Action::Point(Point::Released(x, y))),
            (BTN_MIDDLE, true) => Some(Action::Paste(Selection::Primary)),
            _ => None,
        }
    }

    /// A vertical axis value in logical pixels (continuous).
    pub fn axis(&mut self, value: f64) {
        *self.frame.value.get_or_insert(0.0) += value;
    }

    /// A vertical wheel movement in whole notches (wl_pointer v5–7).
    pub fn axis_discrete(&mut self, discrete: i32) {
        self.frame.discrete += discrete;
    }

    /// A vertical wheel movement in 120ths of a notch (wl_pointer v8+).
    pub fn axis_value120(&mut self, value120: i32) {
        self.frame.value120 += value120;
    }

    /// The vertical axis stopped (a finger lifted).
    pub fn axis_stop(&mut self) {
        self.frame.stop = true;
    }

    /// Ends the frame: what its vertical axis events scroll, if anything.
    pub fn frame(&mut self) -> Vec<Point> {
        let frame = std::mem::take(&mut self.frame);
        let Some((x, y)) = self.at else {
            return Vec::new();
        };
        let mut points = Vec::new();
        let scroll = if frame.value120 != 0 {
            Some(Scroll::Wheel(frame.value120))
        } else if frame.discrete != 0 {
            Some(Scroll::Wheel(frame.discrete.saturating_mul(120)))
        } else {
            frame.value.filter(|v| *v != 0.0).map(Scroll::Pixels)
        };
        points.extend(scroll.map(|s| Point::Scrolled(s, x, y)));
        if frame.stop {
            points.push(Point::Scrolled(Scroll::Stop, x, y));
        }
        points
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn over(x: f64, y: f64) -> Pointer {
        let mut p = Pointer::new();
        p.enter(x, y);
        p
    }

    #[test]
    fn motion_reports_the_position_from_enter_until_leave() {
        let mut p = Pointer::new();
        assert_eq!(p.motion(1.0, 2.0), None, "not over the window yet");
        p.enter(10.0, 20.0);
        assert_eq!(p.position(), Some((10.0, 20.0)), "recorded, not reported");
        assert_eq!(p.motion(11.5, 21.0), Some(Point::Moved(11.5, 21.0)));
        assert_eq!(p.position(), Some((11.5, 21.0)));
        assert_eq!(p.leave(), Some(Point::Left));
        assert_eq!(p.leave(), None);
        assert_eq!(p.motion(1.0, 2.0), None);
        assert_eq!(p.position(), None);
    }

    #[test]
    fn the_primary_button_reports_press_and_release_where_they_happen() {
        let mut p = over(5.0, 6.0);
        assert_eq!(
            p.button(BTN_LEFT, true),
            Some(Action::Point(Point::Pressed(5.0, 6.0)))
        );
        p.motion(7.0, 8.0);
        assert_eq!(
            p.button(BTN_LEFT, false),
            Some(Action::Point(Point::Released(7.0, 8.0)))
        );
    }

    #[test]
    fn the_middle_button_pastes_the_primary_selection_on_press() {
        let mut p = over(5.0, 6.0);
        assert_eq!(
            p.button(BTN_MIDDLE, true),
            Some(Action::Paste(Selection::Primary))
        );
        assert_eq!(p.button(BTN_MIDDLE, false), None);
        assert_eq!(p.button(0x111, true), None, "the right button does nothing");
    }

    #[test]
    fn buttons_outside_the_window_do_nothing() {
        let mut p = Pointer::new();
        assert_eq!(p.button(BTN_LEFT, true), None);
        assert_eq!(p.button(BTN_MIDDLE, true), None);
    }

    #[test]
    fn a_wheel_frame_scrolls_by_value120_and_ignores_its_pixels() {
        let mut p = over(1.0, 2.0);
        p.axis(15.0);
        p.axis_value120(120);
        assert_eq!(p.frame(), [Point::Scrolled(Scroll::Wheel(120), 1.0, 2.0)]);
        assert_eq!(p.frame(), [], "a frame is used once");
    }

    #[test]
    fn a_high_resolution_wheel_reports_fractions_of_a_notch() {
        let mut p = over(0.0, 0.0);
        p.axis_value120(-30);
        assert_eq!(p.frame(), [Point::Scrolled(Scroll::Wheel(-30), 0.0, 0.0)]);
    }

    #[test]
    fn discrete_steps_count_whole_notches_before_v8() {
        let mut p = over(0.0, 0.0);
        p.axis(-30.0);
        p.axis_discrete(-2);
        assert_eq!(p.frame(), [Point::Scrolled(Scroll::Wheel(-240), 0.0, 0.0)]);
    }

    #[test]
    fn a_finger_scrolls_by_pixels_and_its_stop_ends_the_scroll() {
        let mut p = over(3.0, 4.0);
        p.axis(4.5);
        p.axis(2.0);
        assert_eq!(p.frame(), [Point::Scrolled(Scroll::Pixels(6.5), 3.0, 4.0)]);
        p.axis_stop();
        assert_eq!(p.frame(), [Point::Scrolled(Scroll::Stop, 3.0, 4.0)]);
    }

    #[test]
    fn a_frame_without_vertical_movement_scrolls_nothing() {
        let mut p = over(0.0, 0.0);
        p.axis(0.0);
        assert_eq!(p.frame(), []);
        let mut away = Pointer::new();
        away.axis_value120(120);
        assert_eq!(away.frame(), [], "not over the window");
    }

    #[test]
    fn leaving_drops_the_frame_in_progress() {
        let mut p = over(0.0, 0.0);
        p.axis_value120(120);
        p.leave();
        p.enter(1.0, 1.0);
        assert_eq!(p.frame(), []);
    }
}
