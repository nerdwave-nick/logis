//! The key bindings (spec §4.5; Ctrl+V and Shift+Insert paste the
//! clipboard, as the ticket for pointer and paste names them), matched with consumed-modifier semantics:
//! a binding sees the keysym the layout produced and only the modifiers
//! the key did not consume ([`Mods`]). Lock and Num Lock are never part of
//! a binding, so Caps Lock leaves Ctrl+N alone and Num Lock only decides
//! whether the keypad types digits.

use xkbcommon::xkb::Keysym;

use super::{Action, Edit, Selection, Step};

/// The modifiers that are active and were not consumed by the key.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Mods {
    pub shift: bool,
    pub ctrl: bool,
    pub alt: bool,
    /// Super / the logo key.
    pub logo: bool,
}

impl Mods {
    /// No modifier.
    pub const NONE: Self = Self {
        shift: false,
        ctrl: false,
        alt: false,
        logo: false,
    };
    /// Shift alone.
    pub const SHIFT: Self = Self {
        shift: true,
        ..Self::NONE
    };
    /// Control alone.
    pub const CTRL: Self = Self {
        ctrl: true,
        ..Self::NONE
    };

    /// Whether a modifier that turns a key into a command is held: text is
    /// typed only without one.
    pub fn commands(self) -> bool {
        self.ctrl || self.alt || self.logo
    }
}

/// The action bound to `keysym` with the unconsumed modifiers `mods`.
pub fn binding(keysym: Keysym, mods: Mods) -> Option<Action> {
    use Action::{Accept, Dismiss, Select};
    use Step::{Next, Previous};
    let edit = |e| Some(Action::Edit(e));
    let plain = mods == Mods::NONE;
    let ctrl = mods == Mods::CTRL;
    match keysym {
        Keysym::Escape if plain => Some(Dismiss),
        Keysym::g | Keysym::G | Keysym::bracketleft if ctrl => Some(Dismiss),
        Keysym::Return | Keysym::KP_Enter if plain => Some(Accept),
        Keysym::Return | Keysym::KP_Enter if mods == Mods::SHIFT => Some(Action::Descend),
        Keysym::Up | Keysym::KP_Up if plain => Some(Select(Previous)),
        Keysym::Down | Keysym::KP_Down if plain => Some(Select(Next)),
        // Shift+Tab is ISO_Left_Tab with Shift consumed on most layouts;
        // where Shift is left unconsumed it is Tab or ISO_Left_Tab + Shift.
        Keysym::ISO_Left_Tab if plain || mods == Mods::SHIFT => Some(Select(Previous)),
        Keysym::Tab if mods == Mods::SHIFT => Some(Select(Previous)),
        Keysym::Tab if plain => Some(Select(Next)),
        Keysym::p | Keysym::P if ctrl => Some(Select(Previous)),
        Keysym::n | Keysym::N if ctrl => Some(Select(Next)),
        Keysym::BackSpace if plain || mods == Mods::SHIFT => edit(Edit::DeleteBack),
        Keysym::Delete | Keysym::KP_Delete if plain => edit(Edit::DeleteForward),
        Keysym::w | Keysym::W if ctrl => edit(Edit::DeleteWordBack),
        Keysym::u | Keysym::U if ctrl => edit(Edit::Clear),
        Keysym::v | Keysym::V if ctrl => Some(Action::Paste(Selection::Clipboard)),
        Keysym::Insert | Keysym::KP_Insert if mods == Mods::SHIFT => {
            Some(Action::Paste(Selection::Clipboard))
        }
        Keysym::Left | Keysym::KP_Left if plain => edit(Edit::Left),
        Keysym::Right | Keysym::KP_Right if plain => edit(Edit::Right),
        Keysym::Home | Keysym::KP_Home if plain => edit(Edit::Home),
        Keysym::End | Keysym::KP_End if plain => edit(Edit::End),
        _ => None,
    }
}

/// The text a key types into the query: printable text, and only without
/// a command modifier (Ctrl+U's control character types nothing).
pub fn typed(text: &str, mods: Mods) -> Option<Action> {
    let printable = !text.is_empty() && !text.chars().any(char::is_control);
    (printable && !mods.commands()).then(|| Action::Edit(Edit::Insert(text.to_owned())))
}

#[cfg(test)]
mod tests {
    use super::*;

    const ALT: Mods = Mods {
        alt: true,
        ..Mods::NONE
    };

    #[test]
    fn escape_ctrl_g_and_ctrl_bracket_dismiss() {
        assert_eq!(binding(Keysym::Escape, Mods::NONE), Some(Action::Dismiss));
        for sym in [Keysym::g, Keysym::G, Keysym::bracketleft] {
            assert_eq!(binding(sym, Mods::CTRL), Some(Action::Dismiss), "{sym:?}");
        }
        assert_eq!(binding(Keysym::g, Mods::NONE), None);
        assert_eq!(binding(Keysym::Escape, Mods::CTRL), None);
    }

    #[test]
    fn return_and_keypad_enter_accept() {
        assert_eq!(binding(Keysym::Return, Mods::NONE), Some(Action::Accept));
        assert_eq!(binding(Keysym::KP_Enter, Mods::NONE), Some(Action::Accept));
    }

    #[test]
    fn shift_return_descends() {
        assert_eq!(binding(Keysym::Return, Mods::SHIFT), Some(Action::Descend));
        assert_eq!(
            binding(Keysym::KP_Enter, Mods::SHIFT),
            Some(Action::Descend)
        );
    }

    #[test]
    fn up_down_tab_and_ctrl_n_p_move() {
        let prev = Some(Action::Select(Step::Previous));
        let next = Some(Action::Select(Step::Next));
        assert_eq!(binding(Keysym::Up, Mods::NONE), prev);
        assert_eq!(binding(Keysym::Down, Mods::NONE), next);
        assert_eq!(binding(Keysym::Tab, Mods::NONE), next);
        assert_eq!(binding(Keysym::ISO_Left_Tab, Mods::NONE), prev);
        assert_eq!(binding(Keysym::Tab, Mods::SHIFT), prev);
        assert_eq!(binding(Keysym::ISO_Left_Tab, Mods::SHIFT), prev);
        assert_eq!(binding(Keysym::n, Mods::CTRL), next);
        assert_eq!(binding(Keysym::p, Mods::CTRL), prev);
        assert_eq!(binding(Keysym::N, Mods::CTRL), next, "Caps Lock on");
        assert_eq!(binding(Keysym::Tab, ALT), None, "Alt+Tab is not ours");
    }

    #[test]
    fn editing_keys_edit() {
        let edit = |e| Some(Action::Edit(e));
        assert_eq!(
            binding(Keysym::BackSpace, Mods::NONE),
            edit(Edit::DeleteBack)
        );
        assert_eq!(
            binding(Keysym::BackSpace, Mods::SHIFT),
            edit(Edit::DeleteBack)
        );
        assert_eq!(binding(Keysym::u, Mods::CTRL), edit(Edit::Clear));
        assert_eq!(binding(Keysym::w, Mods::CTRL), edit(Edit::DeleteWordBack));
        assert_eq!(
            binding(Keysym::Delete, Mods::NONE),
            edit(Edit::DeleteForward)
        );
        assert_eq!(binding(Keysym::Left, Mods::NONE), edit(Edit::Left));
        assert_eq!(binding(Keysym::KP_End, Mods::NONE), edit(Edit::End));
    }

    #[test]
    fn ctrl_v_and_shift_insert_paste_the_clipboard() {
        let paste = Some(Action::Paste(super::super::Selection::Clipboard));
        assert_eq!(binding(Keysym::v, Mods::CTRL), paste);
        assert_eq!(binding(Keysym::V, Mods::CTRL), paste, "Caps Lock on");
        assert_eq!(binding(Keysym::Insert, Mods::SHIFT), paste);
        assert_eq!(binding(Keysym::KP_Insert, Mods::SHIFT), paste);
        assert_eq!(binding(Keysym::v, Mods::NONE), None, "v types");
        assert_eq!(binding(Keysym::Insert, Mods::NONE), None);
    }

    #[test]
    fn text_is_typed_only_printable_and_without_command_modifiers() {
        let insert = |s: &str| Some(Action::Edit(Edit::Insert(s.to_owned())));
        assert_eq!(typed("ö", Mods::NONE), insert("ö"));
        assert_eq!(typed("A", Mods::SHIFT), insert("A"));
        assert_eq!(typed("u", Mods::CTRL), None);
        assert_eq!(typed("x", ALT), None);
        assert_eq!(typed("\u{15}", Mods::NONE), None, "a control character");
        assert_eq!(typed("\r", Mods::NONE), None);
        assert_eq!(typed("", Mods::NONE), None);
    }
}
