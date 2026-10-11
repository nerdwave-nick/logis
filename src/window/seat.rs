//! The seat and its keyboard: forwards `wl_keyboard` events to the input
//! model ([`crate::input::Keyboard`]) and queues the [`Action`]s it yields
//! for whoever shows a panel, plus the test trace (see [`super::trace`]) of
//! focus, keys, modifiers, repeat info and repeats. The seat's pointer is
//! [`super::pointer`]'s.

use std::os::fd::OwnedFd;
use std::time::Instant;

use smithay_client_toolkit::reexports::client::globals::GlobalList;
use smithay_client_toolkit::reexports::client::protocol::wl_keyboard::{self, WlKeyboard};
use smithay_client_toolkit::reexports::client::protocol::wl_seat::{self, WlSeat};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle, WEnum};
use xkbcommon::xkb;

use super::Windows;
use super::trace::emit;
use crate::input::{self, Action, FocusLoss};

/// The first seat the compositor offers, its keyboard while it has one, and
/// the input model.
pub(super) struct Keyboard {
    seat: Option<WlSeat>,
    keyboard: Option<WlKeyboard>,
    input: input::Keyboard,
}

impl Keyboard {
    /// Binds the first seat, if the compositor offers one (up to v9, for
    /// the pointer's high-resolution `axis_value120`); its keyboard follows
    /// once the seat announces the capability. The Compose table is loaded
    /// now, at start, not on the first key.
    pub(super) fn bind(globals: &GlobalList, qh: &QueueHandle<Windows>) -> Self {
        Self {
            seat: globals.bind(qh, 1..=9, ()).ok(),
            keyboard: None,
            input: input::Keyboard::from_env(),
        }
    }

    /// The seat, if the compositor offers one.
    pub(super) fn seat(&self) -> Option<&WlSeat> {
        self.seat.as_ref()
    }

    pub(super) fn set_focus_loss(&mut self, policy: FocusLoss) {
        self.input.set_focus_loss(policy);
    }

    /// Ends a repeat or Compose sequence (the panel was shown or hidden).
    pub(super) fn reset(&mut self) {
        self.input.reset();
    }

    /// When the next key repeat is due, if a key repeats.
    pub(super) fn deadline(&self) -> Option<Instant> {
        self.input.deadline()
    }

    /// The repeats due at `now`.
    pub(super) fn repeat(&mut self, now: Instant) -> Vec<Action> {
        let mut actions = Vec::new();
        for (code, action) in self.input.repeats_due(now) {
            emit(format_args!("key {code} repeated"));
            actions.extend(action);
        }
        actions
    }

    fn keymap(&mut self, format: WEnum<wl_keyboard::KeymapFormat>, fd: OwnedFd, size: u32) {
        if format != WEnum::Value(wl_keyboard::KeymapFormat::XkbV1) || size == 0 {
            return;
        }
        // SAFETY: the fd is the compositor's keymap of `size` bytes, which
        // xkbcommon maps privately and read-only (wl_keyboard v7+).
        let keymap = unsafe {
            xkb::Keymap::new_from_fd(
                self.input.context(),
                fd,
                size as usize,
                xkb::KEYMAP_FORMAT_TEXT_V1,
                xkb::KEYMAP_COMPILE_NO_FLAGS,
            )
        };
        // A broken keymap leaves the last one in force.
        if let Ok(Some(keymap)) = keymap {
            self.input.set_keymap(keymap);
            emit(format_args!("keymap"));
        }
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
        let has_pointer = capabilities.contains(wl_seat::Capability::Pointer);
        windows.pointer.capability(seat, has_pointer, qh);
        let state = windows.keyboard_mut();
        let has = capabilities.contains(wl_seat::Capability::Keyboard);
        match (has, state.keyboard.take()) {
            (true, None) => state.keyboard = Some(seat.get_keyboard(qh, ())),
            (true, kept) => state.keyboard = kept,
            (false, Some(gone)) => {
                state.input.reset();
                if gone.version() >= 3 {
                    gone.release();
                }
            }
            (false, None) => {}
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
            wl_keyboard::Event::Keymap { format, fd, size } => {
                windows.keyboard_mut().keymap(format, fd, size);
            }
            wl_keyboard::Event::Enter { surface, .. } if windows.shows(&surface) => {
                emit(format_args!("keyboard-enter"));
                windows.keyboard_mut().input.enter();
            }
            wl_keyboard::Event::Leave { surface, .. } if windows.shows(&surface) => {
                emit(format_args!("keyboard-leave"));
                let action = windows.keyboard_mut().input.leave();
                windows.actions.extend(action);
            }
            wl_keyboard::Event::Key {
                serial,
                key,
                state: WEnum::Value(state),
                ..
            } => {
                let pressed = state == wl_keyboard::KeyState::Pressed;
                if pressed {
                    windows.activation.serial = Some(serial);
                }
                let word = if pressed { "pressed" } else { "released" };
                emit(format_args!("key {key} {word}"));
                let keyboard = windows.keyboard_mut();
                if pressed {
                    let action = keyboard.input.press(key, Instant::now());
                    windows.actions.extend(action);
                } else {
                    keyboard.input.release(key);
                }
            }
            wl_keyboard::Event::Modifiers {
                mods_depressed,
                mods_latched,
                mods_locked,
                group,
                ..
            } => {
                emit(format_args!(
                    "modifiers {mods_depressed} {mods_latched} {mods_locked} {group}"
                ));
                let input = &mut windows.keyboard_mut().input;
                input.modifiers(mods_depressed, mods_latched, mods_locked, group);
            }
            wl_keyboard::Event::RepeatInfo { rate, delay } => {
                emit(format_args!("repeat-info {rate} {delay}"));
                windows.keyboard_mut().input.set_repeat(rate, delay);
            }
            _ => {}
        }
    }
}
