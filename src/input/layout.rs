//! The compositor's keymap and the xkb state its modifier masks describe:
//! what a key means right now.

use xkbcommon::xkb::{self, Keysym};

use super::Mods;

/// wl_keyboard sends evdev codes; xkb keycodes are 8 higher.
const EVDEV_OFFSET: u32 = 8;

/// A compiled keymap and its current state.
pub struct Layout {
    keymap: xkb::Keymap,
    state: xkb::State,
    /// Shift, Control, Alt (Mod1) and Super (Mod4), as indices in the keymap.
    mods: [xkb::ModIndex; 4],
}

/// What a key means in the current state.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Key {
    /// The keysym the layout produces now (levels and locks applied).
    pub keysym: Keysym,
    /// The keysym of the key on the first layout at its first level, for
    /// shortcuts typed while another layout (say Cyrillic) is active.
    pub base: Keysym,
    /// Whether another layout than the first is active.
    pub other_layout: bool,
    /// The text it types (may be a control character or empty).
    pub text: String,
    /// The active modifiers it did not consume.
    pub mods: Mods,
}

impl Layout {
    /// The layout of `keymap`, with no modifier active.
    pub fn new(keymap: xkb::Keymap) -> Self {
        let state = xkb::State::new(&keymap);
        let mods = [
            xkb::MOD_NAME_SHIFT,
            xkb::MOD_NAME_CTRL,
            xkb::MOD_NAME_ALT,
            xkb::MOD_NAME_LOGO,
        ]
        .map(|name| keymap.mod_get_index(name));
        Self {
            keymap,
            state,
            mods,
        }
    }

    /// Applies the compositor's modifier masks and layout group.
    pub fn set_mask(&mut self, depressed: u32, latched: u32, locked: u32, group: u32) {
        self.state
            .update_mask(depressed, latched, locked, 0, 0, group);
    }

    /// What evdev key `code` means now.
    pub fn key(&self, code: u32) -> Key {
        let key = xkb::Keycode::new(code + EVDEV_OFFSET);
        let held = |index: xkb::ModIndex| {
            index != xkb::MOD_INVALID
                && self
                    .state
                    .mod_index_is_active(index, xkb::STATE_MODS_EFFECTIVE)
                && !self.state.mod_index_is_consumed(key, index)
        };
        let [shift, ctrl, alt, logo] = self.mods.map(held);
        let base = self
            .keymap
            .key_get_syms_by_level(key, 0, 0)
            .first()
            .copied()
            .unwrap_or(Keysym::NoSymbol);
        Key {
            keysym: self.state.key_get_one_sym(key),
            base,
            other_layout: self.state.key_get_layout(key) != 0,
            text: self.state.key_get_utf8(key),
            mods: Mods {
                shift,
                ctrl,
                alt,
                logo,
            },
        }
    }

    /// Whether the keymap lets evdev key `code` repeat (modifiers do not).
    pub fn repeats(&self, code: u32) -> bool {
        self.keymap
            .key_repeats(xkb::Keycode::new(code + EVDEV_OFFSET))
    }
}

#[cfg(test)]
pub(super) mod tests {
    use super::*;

    /// evdev codes used below.
    pub const A: u32 = 30;
    pub const G: u32 = 34;
    pub const EIGHT: u32 = 9;
    pub const BRACKET: u32 = 26;
    pub const TAB: u32 = 15;
    pub const KP_1: u32 = 79;
    pub const LEFT_SHIFT: u32 = 42;

    /// Real modifier masks (fixed bits in every xkb keymap).
    pub const SHIFT: u32 = 1;
    pub const LOCK: u32 = 1 << 1;
    pub const CONTROL: u32 = 1 << 2;
    pub const NUM: u32 = 1 << 4;
    pub const LEVEL3: u32 = 1 << 7;

    /// A keymap from the system's xkeyboard-config, untouched by
    /// `XKB_DEFAULT_*` in the environment.
    pub fn keymap(layout: &str, options: &str) -> xkb::Keymap {
        let context = xkb::Context::new(xkb::CONTEXT_NO_ENVIRONMENT_NAMES);
        xkb::Keymap::new_from_names(
            &context,
            "evdev",
            "pc105",
            layout,
            "",
            Some(options.to_owned()),
            xkb::KEYMAP_COMPILE_NO_FLAGS,
        )
        .unwrap_or_else(|| panic!("xkeyboard-config compiles layout {layout}"))
    }

    fn layout(name: &str) -> Layout {
        Layout::new(keymap(name, ""))
    }

    #[test]
    fn shift_is_consumed_by_letters_and_tab() {
        let mut us = layout("us");
        us.set_mask(SHIFT, 0, 0, 0);
        let a = us.key(A);
        assert_eq!(
            (a.keysym, a.text.as_str(), a.mods),
            (Keysym::A, "A", Mods::NONE)
        );
        let tab = us.key(TAB);
        assert_eq!((tab.keysym, tab.mods), (Keysym::ISO_Left_Tab, Mods::NONE));
    }

    #[test]
    fn control_stays_significant_and_types_a_control_character() {
        let mut us = layout("us");
        us.set_mask(CONTROL, 0, 0, 0);
        let g = us.key(G);
        assert_eq!((g.keysym, g.mods), (Keysym::g, Mods::CTRL));
        assert_eq!(g.text, "\u{7}");
    }

    #[test]
    fn caps_lock_changes_the_letter_but_is_never_a_modifier() {
        let mut us = layout("us");
        us.set_mask(CONTROL, 0, LOCK, 0);
        let g = us.key(G);
        assert_eq!((g.keysym, g.mods), (Keysym::G, Mods::CTRL));
        us.set_mask(0, 0, LOCK, 0);
        assert_eq!(us.key(A).text, "A");
    }

    #[test]
    fn altgr_is_consumed_so_ctrl_bracket_works_on_a_german_layout() {
        let mut de = layout("de");
        de.set_mask(CONTROL | LEVEL3, 0, 0, 0);
        let bracket = de.key(EIGHT);
        assert_eq!(
            (bracket.keysym, bracket.mods),
            (Keysym::bracketleft, Mods::CTRL)
        );
        let mut us = layout("us");
        us.set_mask(CONTROL, 0, 0, 0);
        assert_eq!(us.key(BRACKET).keysym, Keysym::bracketleft);
    }

    #[test]
    fn num_lock_turns_the_keypad_into_digits() {
        let mut us = layout("us");
        let off = us.key(KP_1);
        assert_eq!((off.keysym, off.text.as_str()), (Keysym::KP_End, ""));
        us.set_mask(0, 0, NUM, 0);
        let on = us.key(KP_1);
        assert_eq!(
            (on.keysym, on.text.as_str(), on.mods),
            (Keysym::KP_1, "1", Mods::NONE)
        );
    }

    #[test]
    fn a_second_layout_keeps_the_first_layouts_base_keysym() {
        let mut both = Layout::new(keymap("us,ru", ""));
        both.set_mask(CONTROL, 0, 0, 1);
        let g = both.key(G);
        assert!(g.other_layout);
        assert_eq!((g.keysym, g.base), (Keysym::Cyrillic_pe, Keysym::g));
    }

    #[test]
    fn letters_repeat_and_modifiers_do_not() {
        let us = layout("us");
        assert!(us.repeats(A));
        assert!(!us.repeats(LEFT_SHIFT));
    }
}
