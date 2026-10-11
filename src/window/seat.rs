//! The seat's keyboard, at its smallest: raw evdev key presses for whoever
//! shows a panel, and the test trace of focus, keys and modifiers (see
//! [`super::trace`]). The keyboard slice replaces this with xkbcommon
//! (keysyms, compose, repeat) and must keep the trace events.

use smithay_client_toolkit::reexports::client::globals::GlobalList;
use smithay_client_toolkit::reexports::client::protocol::wl_keyboard::{self, WlKeyboard};
use smithay_client_toolkit::reexports::client::protocol::wl_seat::{self, WlSeat};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle, WEnum};

use super::Windows;
use super::trace::emit;

/// The first seat the compositor offers, its keyboard while it has one, and
/// the key presses not yet taken.
pub(super) struct Keyboard {
    seat: Option<WlSeat>,
    keyboard: Option<WlKeyboard>,
    presses: Vec<u32>,
}

impl Keyboard {
    /// Binds the first seat, if the compositor offers one; its keyboard
    /// follows once the seat announces the capability.
    pub(super) fn bind(globals: &GlobalList, qh: &QueueHandle<Windows>) -> Self {
        Self {
            seat: globals.bind(qh, 1..=7, ()).ok(),
            keyboard: None,
            presses: Vec::new(),
        }
    }

    /// The evdev codes pressed since the last call, oldest first.
    pub(super) fn take_presses(&mut self) -> Vec<u32> {
        std::mem::take(&mut self.presses)
    }
}

impl Drop for Keyboard {
    fn drop(&mut self) {
        if let Some(keyboard) = self.keyboard.take().filter(|k| k.version() >= 3) {
            keyboard.release();
        }
        if let Some(seat) = self.seat.take().filter(|s| s.version() >= 5) {
            seat.release();
        }
    }
}

impl Dispatch<WlSeat, ()> for Windows {
    fn event(
        windows: &mut Self,
        seat: &WlSeat,
        event: wl_seat::Event,
        _: &(),
        _: &Connection,
        qh: &QueueHandle<Self>,
    ) {
        let wl_seat::Event::Capabilities {
            capabilities: WEnum::Value(capabilities),
        } = event
        else {
            return;
        };
        let state = windows.keyboard_mut();
        let has = capabilities.contains(wl_seat::Capability::Keyboard);
        match (has, state.keyboard.take()) {
            (true, None) => state.keyboard = Some(seat.get_keyboard(qh, ())),
            (true, kept) => state.keyboard = kept,
            (false, Some(gone)) if gone.version() >= 3 => gone.release(),
            (false, _) => {}
        }
    }
}

impl Dispatch<WlKeyboard, ()> for Windows {
    fn event(
        windows: &mut Self,
        _: &WlKeyboard,
        event: wl_keyboard::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        match event {
            wl_keyboard::Event::Enter { surface, .. } if windows.shows(&surface) => {
                emit(format_args!("keyboard-enter"));
            }
            wl_keyboard::Event::Leave { surface, .. } if windows.shows(&surface) => {
                emit(format_args!("keyboard-leave"));
            }
            wl_keyboard::Event::Key {
                key,
                state: WEnum::Value(state),
                ..
            } => {
                let pressed = state == wl_keyboard::KeyState::Pressed;
                let word = if pressed { "pressed" } else { "released" };
                emit(format_args!("key {key} {word}"));
                if pressed {
                    windows.keyboard_mut().presses.push(key);
                }
            }
            wl_keyboard::Event::Modifiers {
                mods_depressed,
                mods_latched,
                mods_locked,
                group,
                ..
            } => emit(format_args!(
                "modifiers {mods_depressed} {mods_latched} {mods_locked} {group}"
            )),
            // The keymap fd is closed on drop; repeat comes with the
            // keyboard slice.
            _ => {}
        }
    }
}
