//! The keyboard as the visible session sees it: `wl_keyboard` events in,
//! [`Action`]s out. The seat forwards the protocol events; everything that
//! decides what a key does lives here.

use std::time::Instant;

use xkbcommon::xkb;

use super::compose::{Compose, Fed};
use super::{Action, Edit, FocusLoss, Key, Layout, Repeat, bindings};

/// Keymap, modifiers, Compose, repeat and the focus-loss policy.
pub struct Keyboard {
    context: xkb::Context,
    layout: Option<Layout>,
    compose: Option<Compose>,
    repeat: Repeat,
    focus_loss: FocusLoss,
}

impl Keyboard {
    /// A keyboard without a keymap yet, composing with `compose`.
    pub fn new(context: xkb::Context, compose: Option<Compose>) -> Self {
        Self {
            context,
            layout: None,
            compose,
            repeat: Repeat::default(),
            focus_loss: FocusLoss::default(),
        }
    }

    /// A keyboard composing with the process locale's Compose table (see
    /// [`Compose::from_env`]).
    pub fn from_env() -> Self {
        let context = xkb::Context::new(xkb::CONTEXT_NO_FLAGS);
        let compose = Compose::from_env(&context);
        Self::new(context, compose)
    }

    /// The xkb context keymaps are compiled in.
    pub fn context(&self) -> &xkb::Context {
        &self.context
    }

    /// What losing keyboard focus does.
    pub fn set_focus_loss(&mut self, policy: FocusLoss) {
        self.focus_loss = policy;
    }

    /// The compositor sent a (new) keymap: modifiers start over, and a
    /// sequence or a repeat in progress ends.
    pub fn set_keymap(&mut self, keymap: xkb::Keymap) {
        self.layout = Some(Layout::new(keymap));
        self.reset();
    }

    /// The compositor's modifier masks and layout group.
    pub fn modifiers(&mut self, depressed: u32, latched: u32, locked: u32, group: u32) {
        if let Some(layout) = &mut self.layout {
            layout.set_mask(depressed, latched, locked, group);
        }
    }

    /// The compositor's repeat info (a rate of 0 disables repeat).
    pub fn set_repeat(&mut self, rate: i32, delay: i32) {
        self.repeat.set_rate(rate, delay);
    }

    /// Ends a Compose sequence or a repeat in progress.
    pub fn reset(&mut self) {
        self.repeat.reset();
        if let Some(compose) = &mut self.compose {
            compose.reset();
        }
    }

    /// The panel gained keyboard focus: nothing from before carries over.
    pub fn enter(&mut self) {
        self.reset();
    }

    /// The panel lost keyboard focus: repeat and Compose stop, and the
    /// session is dismissed unless the policy keeps it.
    pub fn leave(&mut self) -> Option<Action> {
        self.reset();
        (self.focus_loss == FocusLoss::Dismiss).then_some(Action::Dismiss)
    }

    /// Evdev key `code` was pressed at `now`.
    pub fn press(&mut self, code: u32, now: Instant) -> Option<Action> {
        let layout = self.layout.as_ref()?;
        let key = layout.key(code);
        let fed = match &mut self.compose {
            Some(compose) => compose.feed(key.keysym),
            None => Fed::Pass,
        };
        let repeats = fed == Fed::Pass && layout.repeats(code) && !key.keysym.is_modifier_key();
        if repeats {
            self.repeat.press(code, now);
        } else if !key.keysym.is_modifier_key() {
            self.repeat.reset();
        }
        match fed {
            Fed::Pass => resolve(&key),
            Fed::Swallowed => None,
            Fed::Composed(text) => (!text.is_empty()).then_some(Action::Edit(Edit::Insert(text))),
        }
    }

    /// Evdev key `code` was released.
    pub fn release(&mut self, code: u32) {
        self.repeat.release(code);
    }

    /// When the next repeat is due; `None` while no key repeats, so the
    /// event loop can wait without a timeout.
    pub fn deadline(&self) -> Option<Instant> {
        self.repeat.deadline()
    }

    /// The repeats due at `now`: the repeating key's code and what it does
    /// under the current modifiers (Compose is not fed by repeats).
    pub fn repeats_due(&mut self, now: Instant) -> Vec<(u32, Option<Action>)> {
        let Some((code, count)) = self.repeat.due(now) else {
            return Vec::new();
        };
        let action = self.layout.as_ref().and_then(|l| resolve(&l.key(code)));
        (0..count).map(|_| (code, action.clone())).collect()
    }
}

/// A key's binding with consumed-modifier semantics, else its text. While
/// another layout than the first is active, a key that is no binding there
/// is tried as the first layout's key, so Ctrl+G works on a Cyrillic layout.
fn resolve(key: &Key) -> Option<Action> {
    bindings::binding(key.keysym, key.mods)
        .or_else(|| {
            key.other_layout
                .then(|| bindings::binding(key.base, key.mods))
                .flatten()
        })
        .or_else(|| bindings::typed(&key.text, key.mods))
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::super::Step;
    use super::super::compose::tests::compose;
    use super::super::layout::tests::{self as l, keymap};
    use super::*;

    const ESC: u32 = 1;
    const O: u32 = 24;
    const APOSTROPHE: u32 = 40;
    const COMPOSE: u32 = 127;
    const N: u32 = 49;

    fn keyboard() -> Keyboard {
        let mut k = Keyboard::new(xkb::Context::new(xkb::CONTEXT_NO_FLAGS), Some(compose()));
        k.set_keymap(keymap("us", "compose:menu"));
        k.set_repeat(25, 300);
        k
    }

    fn insert(s: &str) -> Option<Action> {
        Some(Action::Edit(Edit::Insert(s.to_owned())))
    }

    /// A press with the masks a compositor would send around it.
    fn shifted(k: &mut Keyboard, code: u32, now: Instant) -> Option<Action> {
        k.press(l::LEFT_SHIFT, now);
        k.modifiers(l::SHIFT, 0, 0, 0);
        let action = k.press(code, now);
        k.release(code);
        k.release(l::LEFT_SHIFT);
        k.modifiers(0, 0, 0, 0);
        action
    }

    #[test]
    fn without_a_keymap_keys_do_nothing() {
        let mut k = Keyboard::new(xkb::Context::new(xkb::CONTEXT_NO_FLAGS), None);
        assert_eq!(k.press(l::A, Instant::now()), None);
    }

    #[test]
    fn the_compose_key_with_a_real_shift_types_o_umlaut() {
        let mut k = keyboard();
        let t = Instant::now();
        assert_eq!(k.press(COMPOSE, t), None);
        assert_eq!(k.press(O, t), None);
        assert_eq!(shifted(&mut k, APOSTROPHE, t), insert("ö"));
        assert_eq!(k.deadline(), None, "composed keys do not repeat");
    }

    #[test]
    fn keys_type_text_and_shortcuts_act() {
        let mut k = keyboard();
        let t = Instant::now();
        assert_eq!(k.press(l::A, t), insert("a"));
        assert_eq!(shifted(&mut k, l::A, t), insert("A"));
        assert_eq!(k.press(ESC, t), Some(Action::Dismiss));
        k.modifiers(l::CONTROL, 0, l::LOCK, 0);
        assert_eq!(
            k.press(N, t),
            Some(Action::Select(Step::Next)),
            "Caps Lock on"
        );
    }

    #[test]
    fn ctrl_g_works_on_a_second_layout() {
        let mut k = keyboard();
        k.set_keymap(keymap("us,ru", ""));
        k.modifiers(l::CONTROL, 0, 0, 1);
        assert_eq!(k.press(l::G, Instant::now()), Some(Action::Dismiss));
        k.modifiers(0, 0, 0, 1);
        assert_eq!(k.press(l::G, Instant::now()), insert("п"));
    }

    #[test]
    fn a_held_key_repeats_under_the_current_modifiers() {
        let mut k = keyboard();
        let t = Instant::now();
        k.press(l::A, t);
        assert_eq!(k.deadline(), Some(t + Duration::from_millis(300)));
        k.press(l::LEFT_SHIFT, t);
        k.modifiers(l::SHIFT, 0, 0, 0);
        assert!(
            k.deadline().is_some(),
            "a modifier does not stop the repeat"
        );
        let due = k.repeats_due(t + Duration::from_millis(340));
        assert_eq!(due, vec![(l::A, insert("A")), (l::A, insert("A"))]);
        k.release(l::A);
        assert_eq!(k.deadline(), None);
    }

    #[test]
    fn a_keymap_change_and_focus_loss_stop_repeat_and_compose() {
        let mut k = keyboard();
        let t = Instant::now();
        k.press(l::A, t);
        k.set_keymap(keymap("us", "compose:menu"));
        assert_eq!(k.deadline(), None);

        k.press(l::A, t);
        k.press(COMPOSE, t);
        assert_eq!(k.deadline(), None, "another key ends the repeat");
        k.press(O, t);
        k.leave();
        k.enter();
        assert_eq!(
            shifted(&mut k, APOSTROPHE, t),
            insert("\""),
            "no stale sequence"
        );
    }

    #[test]
    fn focus_loss_dismisses_by_default_and_keeps_on_request() {
        let mut k = keyboard();
        assert_eq!(k.leave(), Some(Action::Dismiss));
        k.set_focus_loss(FocusLoss::Keep);
        assert_eq!(k.leave(), None);
    }

    #[test]
    fn rate_zero_never_schedules_a_repeat() {
        let mut k = keyboard();
        k.set_repeat(0, 300);
        k.press(l::A, Instant::now());
        assert_eq!(k.deadline(), None);
        assert!(
            k.repeats_due(Instant::now() + Duration::from_secs(5))
                .is_empty()
        );
    }

    #[test]
    fn the_keypad_types_digits_only_with_num_lock() {
        let mut k = keyboard();
        let t = Instant::now();
        assert_eq!(k.press(l::KP_1, t), Some(Action::Edit(Edit::End)));
        k.modifiers(0, 0, l::NUM, 0);
        assert_eq!(k.press(l::KP_1, t), insert("1"));
    }
}
