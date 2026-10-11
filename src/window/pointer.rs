//! The seat's pointer: forwards `wl_pointer` events over the visible panel
//! to the pointer model ([`crate::input::Pointer`]) and queues the
//! [`Action`]s it yields beside the keyboard's, so their order is kept.
//!
//! The cursor is the compositor's default arrow, set through
//! `wp_cursor_shape_v1` on every enter when the compositor offers it.
//! Without it logis sets no cursor image at all (no cursor theme is loaded
//! in-process): the compositor keeps showing the cursor it had, which is
//! what a missing cursor-shape protocol gracefully allows.
//!
//! Trace events (see [`super::trace`]): `pointer-enter <x> <y>` and
//! `pointer-motion <x> <y>` (surface coordinates), `pointer-leave`,
//! `cursor default` or `cursor unchanged` (no cursor-shape protocol),
//! `button <code> pressed|released`, and per frame `scroll wheel <120ths>`,
//! `scroll pixels <px>` or `scroll stop`.

use smithay_client_toolkit::reexports::client::globals::GlobalList;
use smithay_client_toolkit::reexports::client::protocol::wl_pointer::{self, WlPointer};
use smithay_client_toolkit::reexports::client::protocol::wl_seat::WlSeat;
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle, WEnum};
use smithay_client_toolkit::reexports::protocols::wp::cursor_shape::v1::client::{
    wp_cursor_shape_device_v1::{Shape, WpCursorShapeDeviceV1},
    wp_cursor_shape_manager_v1::WpCursorShapeManagerV1,
};

use super::Windows;
use super::trace::emit;
use crate::input::{self, Action, Point, Scroll};

/// The seat's pointer while it has one, its cursor-shape device, and the
/// pointer model.
pub(super) struct Pointer {
    pointer: Option<WlPointer>,
    shapes: Option<WpCursorShapeManagerV1>,
    shape: Option<WpCursorShapeDeviceV1>,
    model: input::Pointer,
}

impl Pointer {
    /// Binds the cursor-shape manager if the compositor offers one; the
    /// pointer follows once the seat announces the capability.
    pub(super) fn bind(globals: &GlobalList, qh: &QueueHandle<Windows>) -> Self {
        Self {
            pointer: None,
            shapes: globals.bind(qh, 1..=1, ()).ok(),
            shape: None,
            model: input::Pointer::new(),
        }
    }

    /// The seat gained or lost its pointer.
    pub(super) fn capability(&mut self, seat: &WlSeat, has: bool, qh: &QueueHandle<Windows>) {
        match (has, self.pointer.is_some()) {
            (true, false) => {
                let pointer = seat.get_pointer(qh, ());
                self.shape = self
                    .shapes
                    .as_ref()
                    .map(|m| m.get_pointer(&pointer, qh, ()));
                self.pointer = Some(pointer);
            }
            (false, true) => self.release(),
            _ => {}
        }
    }

    /// Forgets where the pointer was (the panel was shown or hidden).
    pub(super) fn reset(&mut self) {
        self.model.reset();
    }

    fn release(&mut self) {
        self.model.reset();
        if let Some(shape) = self.shape.take() {
            shape.destroy();
        }
        if let Some(pointer) = self.pointer.take().filter(|p| p.version() >= 3) {
            pointer.release();
        }
    }

    /// Sets the default arrow for the enter with `serial`.
    fn set_cursor(&self, serial: u32) {
        match &self.shape {
            Some(shape) => {
                shape.set_shape(serial, Shape::Default);
                emit(format_args!("cursor default"));
            }
            None => emit(format_args!("cursor unchanged")),
        }
    }
}

impl Drop for Pointer {
    fn drop(&mut self) {
        self.release();
        if let Some(shapes) = self.shapes.take() {
            shapes.destroy();
        }
    }
}

impl Dispatch<WlPointer, ()> for Windows {
    fn event(
        windows: &mut Self,
        pointer: &WlPointer,
        event: wl_pointer::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        use wl_pointer::{Axis, Event};
        let vertical = |axis: &WEnum<Axis>| *axis == WEnum::Value(Axis::VerticalScroll);
        // Before v5 there are no frames: every axis event is one.
        let unframed = pointer.version() < 5 && matches!(event, Event::Axis { .. });
        let mut points = Vec::new();
        let mut action = None;
        match event {
            Event::Enter {
                serial,
                surface,
                surface_x,
                surface_y,
            } if windows.shows(&surface) => {
                emit(format_args!("pointer-enter {surface_x} {surface_y}"));
                windows.pointer.set_cursor(serial);
                windows.pointer.model.enter(surface_x, surface_y);
            }
            Event::Leave { .. } => {
                if let Some(left) = windows.pointer.model.leave() {
                    emit(format_args!("pointer-leave"));
                    points.push(left);
                }
            }
            Event::Motion {
                surface_x,
                surface_y,
                ..
            } => {
                if let Some(moved) = windows.pointer.model.motion(surface_x, surface_y) {
                    emit(format_args!("pointer-motion {surface_x} {surface_y}"));
                    points.push(moved);
                }
            }
            Event::Button {
                serial,
                button,
                state: WEnum::Value(state),
                ..
            } => {
                let pressed = state == wl_pointer::ButtonState::Pressed;
                if pressed {
                    windows.activation.serial = Some(serial);
                }
                if windows.pointer.model.position().is_some() {
                    let word = if pressed { "pressed" } else { "released" };
                    emit(format_args!("button {button} {word}"));
                }
                action = windows.pointer.model.button(button, pressed);
            }
            Event::Axis { axis, value, .. } if vertical(&axis) => {
                windows.pointer.model.axis(value);
            }
            Event::AxisDiscrete { axis, discrete } if vertical(&axis) => {
                windows.pointer.model.axis_discrete(discrete);
            }
            Event::AxisValue120 { axis, value120 } if vertical(&axis) => {
                windows.pointer.model.axis_value120(value120);
            }
            Event::AxisStop { axis, .. } if vertical(&axis) => windows.pointer.model.axis_stop(),
            Event::Frame => points.extend(scrolled(windows.pointer.model.frame())),
            _ => {}
        }
        if unframed {
            points.extend(scrolled(windows.pointer.model.frame()));
        }
        windows.actions.extend(action);
        windows
            .actions
            .extend(points.into_iter().map(Action::Point));
    }
}

/// Traces each scroll of a frame.
fn scrolled(points: Vec<Point>) -> Vec<Point> {
    for point in &points {
        match point {
            Point::Scrolled(Scroll::Wheel(v), ..) => emit(format_args!("scroll wheel {v}")),
            Point::Scrolled(Scroll::Pixels(px), ..) => emit(format_args!("scroll pixels {px}")),
            Point::Scrolled(Scroll::Stop, ..) => emit(format_args!("scroll stop")),
            _ => {}
        }
    }
    points
}

impl Dispatch<WpCursorShapeManagerV1, ()> for Windows {
    fn event(
        _: &mut Self,
        _: &WpCursorShapeManagerV1,
        _: <WpCursorShapeManagerV1 as Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}

impl Dispatch<WpCursorShapeDeviceV1, ()> for Windows {
    fn event(
        _: &mut Self,
        _: &WpCursorShapeDeviceV1,
        _: <WpCursorShapeDeviceV1 as Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
