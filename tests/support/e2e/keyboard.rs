//! The virtual-keyboard injector (zwp_virtual_keyboard_v1): installs a US
//! keymap, sends real modifier key events together with the modifier masks
//! (compositors do not derive masks from a virtual keyboard's keys), and
//! types only with a [`Focused`] proof that the client reported keyboard
//! focus. It exists only on a [`Harness`] connection, so only inside the
//! test compositor.

use std::os::fd::AsFd;
use std::time::Instant;

use smithay_client_toolkit::reexports::protocols_misc::zwp_virtual_keyboard_v1::client::zwp_virtual_keyboard_v1::ZwpVirtualKeyboardV1;

use super::client::Focused;
use super::wire::Harness;

/// The keymap: evdev keycodes, the "complete" types and compat, and the US
/// layout with the Menu key as Compose (XKB option `compose:menu`),
/// resolved by the compositor from its xkeyboard-config. The test
/// compositors' own keymaps say the same (see [`super::NIRI_CONFIG`]), so
/// a client sees one meaning whichever keymap reaches it.
pub const KEYMAP: &str = r#"xkb_keymap {
    xkb_keycodes { include "evdev+aliases(qwerty)" };
    xkb_types { include "complete" };
    xkb_compat { include "complete" };
    xkb_symbols { include "pc+us+inet(evdev)+compose(menu)" };
    xkb_geometry { include "pc(pc105)" };
};
"#;

/// evdev key codes used by tests (linux/input-event-codes.h).
pub mod code {
    pub const ESC: u32 = 1;
    pub const BACKSPACE: u32 = 14;
    pub const TAB: u32 = 15;
    pub const ENTER: u32 = 28;
    pub const LEFT_CTRL: u32 = 29;
    pub const LEFT_SHIFT: u32 = 42;
    pub const LEFT_ALT: u32 = 56;
    pub const SPACE: u32 = 57;
    pub const CAPS_LOCK: u32 = 58;
    pub const NUM_LOCK: u32 = 69;
    pub const KP_7: u32 = 71;
    pub const KP_8: u32 = 72;
    pub const KP_9: u32 = 73;
    pub const KP_4: u32 = 75;
    pub const KP_5: u32 = 76;
    pub const KP_6: u32 = 77;
    pub const KP_1: u32 = 79;
    pub const KP_2: u32 = 80;
    pub const KP_3: u32 = 81;
    pub const KP_0: u32 = 82;
    pub const KP_ENTER: u32 = 96;
    pub const HOME: u32 = 102;
    pub const UP: u32 = 103;
    pub const LEFT: u32 = 105;
    pub const RIGHT: u32 = 106;
    pub const END: u32 = 107;
    pub const DOWN: u32 = 108;
    pub const INSERT: u32 = 110;
    pub const DELETE: u32 = 111;
    pub const LEFT_META: u32 = 125;
    pub const COMPOSE: u32 = 127;
    pub const LEFT_BRACE: u32 = 26;
    pub const G: u32 = 34;
    pub const N: u32 = 49;
    pub const P: u32 = 25;
    pub const U: u32 = 22;
    pub const W: u32 = 17;
    pub const X: u32 = 45;
}

/// A held modifier: its key and its real-modifier bit in the keymap
/// (xkbcommon fixes Shift, Lock, Control, Mod1…Mod5 as bits 0…7).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Modifier {
    Shift,
    Control,
    Alt,
    Super,
}

impl Modifier {
    pub fn code(self) -> u32 {
        match self {
            Self::Shift => code::LEFT_SHIFT,
            Self::Control => code::LEFT_CTRL,
            Self::Alt => code::LEFT_ALT,
            Self::Super => code::LEFT_META,
        }
    }

    pub fn mask(self) -> u32 {
        match self {
            Self::Shift => 1,
            Self::Control => 1 << 2,
            Self::Alt => 1 << 3,
            Self::Super => 1 << 6,
        }
    }
}

/// A lock toggled by a key press: Caps Lock (Lock, bit 1) or Num Lock
/// (Mod2, bit 4).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Lock {
    Caps,
    Num,
}

impl Lock {
    pub fn code(self) -> u32 {
        match self {
            Self::Caps => code::CAPS_LOCK,
            Self::Num => code::NUM_LOCK,
        }
    }

    pub fn mask(self) -> u32 {
        match self {
            Self::Caps => 1 << 1,
            Self::Num => 1 << 4,
        }
    }
}

/// The US layout's printable keys: unshifted, shifted, evdev code.
const US: [(char, char, u32); 49] = [
    ('1', '!', 2),
    ('2', '@', 3),
    ('3', '#', 4),
    ('4', '$', 5),
    ('5', '%', 6),
    ('6', '^', 7),
    ('7', '&', 8),
    ('8', '*', 9),
    ('9', '(', 10),
    ('0', ')', 11),
    ('-', '_', 12),
    ('=', '+', 13),
    ('\t', '\t', 15),
    ('q', 'Q', 16),
    ('w', 'W', 17),
    ('e', 'E', 18),
    ('r', 'R', 19),
    ('t', 'T', 20),
    ('y', 'Y', 21),
    ('u', 'U', 22),
    ('i', 'I', 23),
    ('o', 'O', 24),
    ('p', 'P', 25),
    ('[', '{', 26),
    (']', '}', 27),
    ('\n', '\n', 28),
    ('a', 'A', 30),
    ('s', 'S', 31),
    ('d', 'D', 32),
    ('f', 'F', 33),
    ('g', 'G', 34),
    ('h', 'H', 35),
    ('j', 'J', 36),
    ('k', 'K', 37),
    ('l', 'L', 38),
    (';', ':', 39),
    ('\'', '"', 40),
    ('`', '~', 41),
    ('\\', '|', 43),
    ('z', 'Z', 44),
    ('x', 'X', 45),
    ('c', 'C', 46),
    ('v', 'V', 47),
    ('b', 'B', 48),
    ('n', 'N', 49),
    ('m', 'M', 50),
    (',', '<', 51),
    ('.', '>', 52),
    ('/', '?', 53),
];

/// The key and Shift state that types `c` on the US layout.
pub fn us_key(c: char) -> Option<(u32, bool)> {
    if c == ' ' {
        return Some((code::SPACE, false));
    }
    US.iter().find_map(|&(lower, upper, code)| {
        if c == lower {
            Some((code, false))
        } else if c == upper {
            Some((code, true))
        } else {
            None
        }
    })
}

/// A virtual keyboard on the harness connection.
pub struct VirtualKeyboard {
    keyboard: ZwpVirtualKeyboardV1,
    depressed: u32,
    locked: u32,
    epoch: Instant,
}

impl Harness {
    /// Creates a virtual keyboard on the seat and installs [`KEYMAP`].
    /// Create it before starting the client: a headless compositor's seat
    /// gains its keyboard capability only from this device.
    pub fn virtual_keyboard(&mut self) -> VirtualKeyboard {
        let qh = self.qh();
        let keyboard = self
            .state
            .keyboards
            .create_virtual_keyboard(&self.state.seat, &qh, ());
        let mut bytes = KEYMAP.as_bytes().to_vec();
        bytes.push(0);
        let path = self.runtime_dir.join("keymap.xkb");
        std::fs::write(&path, &bytes).unwrap();
        let file = std::fs::File::open(&path).unwrap();
        // 1 = WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1
        keyboard.keymap(1, file.as_fd(), bytes.len() as u32);
        self.roundtrip();
        VirtualKeyboard {
            keyboard,
            depressed: 0,
            locked: 0,
            epoch: Instant::now(),
        }
    }

    fn send_key(&mut self, kb: &VirtualKeyboard, code: u32, pressed: bool) {
        let time = kb.epoch.elapsed().as_millis() as u32;
        kb.keyboard.key(time, code, u32::from(pressed));
        self.roundtrip();
    }

    fn send_modifiers(&mut self, kb: &VirtualKeyboard) {
        kb.keyboard.modifiers(kb.depressed, 0, kb.locked, 0);
        self.roundtrip();
    }

    /// Presses or releases key `code`.
    pub fn key(&mut self, kb: &mut VirtualKeyboard, _: &Focused, code: u32, pressed: bool) {
        self.send_key(kb, code, pressed);
    }

    /// Presses and releases key `code` in one go: both requests leave in
    /// one flush, so a stalled compositor cannot hold the key down long
    /// enough to repeat it (a tap is never a hold).
    pub fn tap(&mut self, kb: &mut VirtualKeyboard, _: &Focused, code: u32) {
        let time = kb.epoch.elapsed().as_millis() as u32;
        kb.keyboard.key(time, code, 1);
        kb.keyboard.key(time, code, 0);
        self.roundtrip();
    }

    /// Presses a modifier key and then sends its mask.
    pub fn hold(&mut self, kb: &mut VirtualKeyboard, _: &Focused, modifier: Modifier) {
        self.send_key(kb, modifier.code(), true);
        kb.depressed |= modifier.mask();
        self.send_modifiers(kb);
    }

    /// Releases a modifier key and then clears its mask.
    pub fn release(&mut self, kb: &mut VirtualKeyboard, _: &Focused, modifier: Modifier) {
        self.send_key(kb, modifier.code(), false);
        kb.depressed &= !modifier.mask();
        self.send_modifiers(kb);
    }

    /// Taps a lock key and toggles its locked mask.
    pub fn toggle(&mut self, kb: &mut VirtualKeyboard, focused: &Focused, lock: Lock) {
        self.tap(kb, focused, lock.code());
        kb.locked ^= lock.mask();
        self.send_modifiers(kb);
    }

    /// Types `text` on the US layout, holding a real Shift for upper-case
    /// and shifted symbols. Panics on a character the layout lacks.
    pub fn type_text(&mut self, kb: &mut VirtualKeyboard, focused: &Focused, text: &str) {
        for c in text.chars() {
            let (code, shift) =
                us_key(c).unwrap_or_else(|| panic!("{c:?} is not on the US layout"));
            if shift {
                self.hold(kb, focused, Modifier::Shift);
            }
            self.tap(kb, focused, code);
            if shift {
                self.release(kb, focused, Modifier::Shift);
            }
        }
    }
}

impl Drop for VirtualKeyboard {
    fn drop(&mut self) {
        self.keyboard.destroy();
    }
}
