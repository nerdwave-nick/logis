//! How servitor mode looks: its stages, rows and words (spec §4.8; the
//! mapping in docs/provider-protocol.md, "servitor mode on this protocol").

use crate::provider::wire::{Input, Item, Stage};

use super::census::{Declaration, Entry};

/// The stage ids. An inscription's is `inscription:<key>`.
pub const RITE: &str = "rite";
pub const ASPECT: &str = "aspect";

/// The window: wide enough for a rite, its aspect and its purpose.
const WIDTH: u32 = 64;
const LINES: u32 = 10;

/// How long a notice of trouble shows.
pub const TOAST_SECONDS: u32 = 6;

/// What `"·"` stands for, as in servitor's own census table: not known.
const UNKNOWN: &str = "·";
/// The emblems of a rite and of an edict.
const RITE_GLYPH: &str = "⚙";
const EDICT_GLYPH: &str = "†";
/// The badge of the aspect a rite stands in.
const CURRENT: &str = "current";

pub const NO_RITES: &str = "the Librarium holds no rite";

/// The rite stage, empty: the census fills it as it speaks.
pub fn rites() -> Stage {
    Stage {
        id: RITE.to_owned(),
        prompt: Some("rite: ".to_owned()),
        placeholder: "choose a rite".to_owned(),
        width: Some(WIDTH),
        lines: Some(LINES),
        ..Stage::default()
    }
}

/// A rite's row: its name, its aspect when known and its purpose. While its
/// auspex is awaited it is pending, and nothing it says of its aspect or
/// standing is shown yet.
pub fn rite(entry: &Entry) -> Item {
    let pending = entry.awaited();
    let aspect = entry.current().unwrap_or(UNKNOWN);
    let mut badges = Vec::new();
    if entry.edict {
        badges.push("edict".to_owned());
    }
    if !pending {
        match entry.standing.as_str() {
            "heretical" => badges.push("heresy".to_owned()),
            s @ ("corrupted" | "tainted") => badges.push(s.to_owned()),
            _ if entry.desecrated => badges.push("desecrated".to_owned()),
            _ => {}
        }
    }
    Item {
        id: entry.rite.clone(),
        columns: vec![entry.rite.clone(), aspect.to_owned(), entry.purpose.clone()],
        glyph: Some(if entry.edict { EDICT_GLYPH } else { RITE_GLYPH }.to_owned()),
        badges,
        pending,
        ..Item::default()
    }
}

/// A rite's aspects in written order, the current one marked, and the one
/// to select: the current aspect, only when it may be trusted.
pub fn aspects(entry: &Entry) -> (Vec<Item>, Option<String>) {
    let current = entry.current();
    let items = entry
        .aspects
        .iter()
        .map(|aspect| Item {
            id: aspect.clone(),
            columns: vec![aspect.clone()],
            badges: if current == Some(aspect.as_str()) {
                vec![CURRENT.to_owned()]
            } else {
                Vec::new()
            },
            ..Item::default()
        })
        .collect();
    (items, current.map(str::to_owned))
}

pub fn aspect_stage(rite: &str, items: Vec<Item>, select: Option<String>) -> Stage {
    Stage {
        id: ASPECT.to_owned(),
        prompt: Some("aspect: ".to_owned()),
        placeholder: format!("bring {rite} into which aspect?"),
        items,
        select,
        ..Stage::default()
    }
}

pub fn inscription_id(declaration: &Declaration) -> String {
    format!("inscription:{}", declaration.key)
}

/// The prompt for one inscription: its purpose, its decree for the aspect
/// (or the edict's) as the query, and a word required when it is mandatory.
pub fn inscription_stage(declaration: &Declaration, aspect: Option<&str>) -> Stage {
    let key = &declaration.key;
    let (prompt, input) = if declaration.mandatory {
        (format!("{key}*: "), Input::Required)
    } else {
        (format!("{key}: "), Input::Optional)
    };
    let placeholder = if declaration.purpose.is_empty() {
        format!("the inscription {key}")
    } else {
        declaration.purpose.clone()
    };
    Stage {
        id: inscription_id(declaration),
        prompt: Some(prompt),
        placeholder,
        query: declaration.decreed(aspect).unwrap_or_default().to_owned(),
        input,
        ..Stage::default()
    }
}

pub fn heretical(rite: &str) -> String {
    format!("the rite {rite} stands in heresy: the inquisition must purge it before it is invoked")
}

pub fn mandatory(key: &str) -> String {
    format!("the inscription {key} is mandatory: inscribe a word")
}

pub fn no_servitor(error: &str) -> String {
    format!("no servitor answers the summons: {error}")
}

pub fn broken_off(status: &str) -> String {
    format!("the census fell silent before its end ({status}); see the cogitator's log")
}

pub fn not_roused(error: &str) -> String {
    format!("the servitor could not be roused: {error}")
}
