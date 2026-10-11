//! The virtual-pointer injector (zwlr_virtual_pointer_v1): absolute motion
//! over the test compositor's output, buttons, wheel notches and finger
//! scrolling, each followed by a frame. Like the keyboard, it exists only
//! on a [`Harness`] connection, whose server [`super::wire::connect_verified`]
//! proved to be the compositor the harness spawned, so it can never move
//! the live session's pointer.
//!
//! Points are aimed at a client's surface through a [`Panel`]: how output
//! points map onto the surface, learnt from where the client itself says
//! the pointer entered it and then moved, so no test assumes how a
//! compositor places the surface or maps absolute motion (nested niri
//! maps it through its output's vertical flip).

use std::time::Instant;

use smithay_client_toolkit::reexports::client::Proxy;
use smithay_client_toolkit::reexports::client::protocol::wl_pointer::{Axis, AxisSource, ButtonState};
use smithay_client_toolkit::reexports::protocols_wlr::virtual_pointer::v1::client::zwlr_virtual_pointer_v1::ZwlrVirtualPointerV1;

use super::wire::Harness;

/// evdev button codes (linux/input-event-codes.h).
pub const BTN_LEFT: u32 = 0x110;
pub const BTN_MIDDLE: u32 = 0x112;

const VERTICAL: Axis = Axis::VerticalScroll;

/// What wlroots and libinput report per wheel notch as a continuous value.
const NOTCH_PIXELS: f64 = 15.0;

/// A virtual pointer on the harness connection.
pub struct VirtualPointer {
    pointer: ZwlrVirtualPointerV1,
    epoch: Instant,
}

/// How the harness's output points map onto a client's surface: two
/// pointer positions, as sent and as the client saw them.
#[derive(Clone, Copy, Debug)]
pub struct Panel {
    /// An output point the pointer was sent to and the surface point the
    /// client reported for it.
    pub anchor: ((f64, f64), (f64, f64)),
    /// Per axis, +1 if surface coordinates grow with output coordinates,
    /// -1 if the compositor mirrored that axis.
    pub sign: (f64, f64),
}

impl Panel {
    /// From two output points and the surface points the client reported.
    pub fn calibrate(sent: [(f64, f64); 2], seen: [(f64, f64); 2]) -> Self {
        let sign = |a: f64, b: f64, c: f64, d: f64| {
            assert!(a != b && c != d, "calibration needs movement on both axes");
            ((d - c) / (b - a)).signum()
        };
        Self {
            anchor: (sent[0], seen[0]),
            sign: (
                sign(sent[0].0, sent[1].0, seen[0].0, seen[1].0),
                sign(sent[0].1, sent[1].1, seen[0].1, seen[1].1),
            ),
        }
    }

    /// The output point over the surface-local point `(x, y)`.
    pub fn at(&self, (x, y): (f64, f64)) -> (f64, f64) {
        let ((ox, oy), (sx, sy)) = self.anchor;
        (ox + self.sign.0 * (x - sx), oy + self.sign.1 * (y - sy))
    }
}

impl Harness {
    /// Creates a virtual pointer on the seat, bound to the harness's output
    /// where the compositor supports that (v2), so absolute motion spans it.
    pub fn virtual_pointer(&mut self) -> VirtualPointer {
        let qh = self.qh();
        let manager = self
            .state
            .pointers
            .as_ref()
            .expect("the test compositor offers zwlr_virtual_pointer_manager_v1");
        let pointer = if manager.version() >= 2 {
            manager.create_virtual_pointer_with_output(
                Some(&self.state.seat),
                Some(&self.state.output),
                &qh,
                (),
            )
        } else {
            manager.create_virtual_pointer(Some(&self.state.seat), &qh, ())
        };
        self.roundtrip();
        VirtualPointer {
            pointer,
            epoch: Instant::now(),
        }
    }

    /// The output's size in logical pixels (the test outputs are at scale 1).
    pub fn output_size(&self) -> (u32, u32) {
        let (w, h) = self.state.mode;
        assert!(w > 0 && h > 0, "the test output announced no mode");
        (w as u32, h as u32)
    }

    /// Moves the pointer to the output point `(x, y)`.
    pub fn move_to(&mut self, vp: &VirtualPointer, (x, y): (f64, f64)) {
        let (w, h) = self.output_size();
        let clamp = |v: f64, extent: u32| v.round().clamp(0.0, f64::from(extent - 1)) as u32;
        vp.pointer
            .motion_absolute(vp.time(), clamp(x, w), clamp(y, h), w, h);
        vp.pointer.frame();
        self.roundtrip();
    }

    /// Presses or releases `button`.
    pub fn button(&mut self, vp: &VirtualPointer, button: u32, pressed: bool) {
        let state = if pressed {
            ButtonState::Pressed
        } else {
            ButtonState::Released
        };
        vp.pointer.button(vp.time(), button, state);
        vp.pointer.frame();
        self.roundtrip();
    }

    /// Presses and releases `button` where the pointer is.
    pub fn click(&mut self, vp: &VirtualPointer, button: u32) {
        self.button(vp, button, true);
        self.button(vp, button, false);
    }

    /// Turns the wheel by `notches` (positive: down), one frame.
    pub fn wheel(&mut self, vp: &VirtualPointer, notches: i32) {
        let t = vp.time();
        vp.pointer.axis_source(AxisSource::Wheel);
        vp.pointer
            .axis_discrete(t, VERTICAL, f64::from(notches) * NOTCH_PIXELS, notches);
        vp.pointer.frame();
        self.roundtrip();
    }

    /// Moves a touchpad finger by `dy` logical pixels, one frame.
    pub fn finger(&mut self, vp: &VirtualPointer, dy: f64) {
        vp.pointer.axis_source(AxisSource::Finger);
        vp.pointer.axis(vp.time(), VERTICAL, dy);
        vp.pointer.frame();
        self.roundtrip();
    }
}

impl VirtualPointer {
    fn time(&self) -> u32 {
        self.epoch.elapsed().as_millis() as u32
    }
}

impl Drop for VirtualPointer {
    fn drop(&mut self) {
        self.pointer.destroy();
    }
}
