//! From a merged theme document to a [`Theme`]: colours looked up in the
//! palette, font roles turned into faces, ranges checked. Every error names
//! the key it is about (`border.paint[1]: no colour named 'embr' …`).

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::time::Duration;

mod parts;

use super::colour::{ColourRef, PaintRef, Palette};
use super::doc::{self, Doc};
use super::model::*;
use crate::render::Rgba;

/// The weight a face has when its role names none.
const REGULAR: u32 = 400;

/// Resolves `doc` (a theme over its bases, the bundled default last).
pub fn resolve(name: &str, dir: Option<PathBuf>, doc: Doc) -> Result<Theme, String> {
    let palette = doc.palette.clone().unwrap_or_default();
    let mut r = Resolver {
        palette: &palette,
        roles: doc.fonts.clone().unwrap_or_default(),
        faces: Vec::new(),
    };
    // The body's face comes first: it is the text's default.
    r.font("fonts", "body")?;
    let row: doc::RowDoc = need(doc.row, "row")?;
    let row_font = r.font("row.font", &need(row.font.clone(), "row.font")?)?;
    let matched = need(doc.matched, "match")?;
    let match_face = r.face_at(row_font.face, need(matched.weight, "match.weight")?)?;
    let theme = Theme {
        name: name.to_owned(),
        dir,
        window: r.window(need(doc.window, "window")?)?,
        glow: r.glow(need(doc.glow, "glow")?)?,
        border: r.border(need(doc.border, "border")?)?,
        panel: r.panel(need(doc.panel, "panel")?)?,
        rivets: r.rivets(need(doc.decorations, "decorations")?)?,
        prompt: r.prompt(need(doc.prompt, "prompt")?)?,
        query: r.query(need(doc.query, "query")?)?,
        counter: r.counter(need(doc.counter, "counter")?)?,
        rule: r.rule(need(doc.rule, "rule")?)?,
        row: r.row(row, row_font)?,
        selection: r.selection(need(doc.selection, "selection")?)?,
        matched: Match {
            color: r.colour(matched.color, "match.color")?,
            selected: r.colour(matched.selected, "match.selected")?,
            face: match_face,
            underline: need(matched.underline, "match.underline")?,
        },
        badge: r.badge(need(doc.badge, "badge")?)?,
        pending: r.pending(need(doc.pending, "pending")?)?,
        effects: r.effects(need(doc.effects, "effects")?)?,
        motion: parts::motion(need(doc.motion, "motion")?)?,
        error: r.colour(ColourRef::parse("error"), "palette.error")?,
        faces: Vec::new(),
    };
    Ok(Theme {
        faces: r.faces,
        ..theme
    })
}

pub(super) fn need<T>(value: Option<T>, key: &str) -> Result<T, String> {
    value.ok_or_else(|| format!("{key}: missing, and no base theme gives it"))
}

pub(super) fn millis(ms: Option<u32>, key: &str) -> Result<Duration, String> {
    need(ms, key).map(|ms| Duration::from_millis(u64::from(ms)))
}

/// A length: finite and not negative.
pub(super) fn length(value: Option<f32>, key: &str) -> Result<f32, String> {
    let v = need(value, key)?;
    if v.is_finite() && v >= 0.0 {
        Ok(v)
    } else {
        Err(format!("{key}: {v} is no length (0 or more)"))
    }
}

/// A strength or opacity: 0 to 1.
pub(super) fn unit(value: Option<f32>, key: &str) -> Result<f32, String> {
    let v = need(value, key)?;
    if (0.0..=1.0).contains(&v) {
        Ok(v)
    } else {
        Err(format!("{key}: {v} is out of range (0 to 1)"))
    }
}

pub(super) struct Resolver<'a> {
    palette: &'a Palette,
    roles: BTreeMap<String, doc::FontDoc>,
    faces: Vec<Face>,
}

impl Resolver<'_> {
    pub(super) fn colour(&self, c: Option<ColourRef>, key: &str) -> Result<Rgba, String> {
        need(c, key)?
            .resolve(self.palette)
            .map_err(|e| format!("{key}: {e}"))
    }

    pub(super) fn paint(&self, p: Option<PaintRef>, key: &str) -> Result<Vec<Rgba>, String> {
        need(p, key)?
            .resolve(self.palette)
            .map_err(|e| format!("{key}{e}"))
    }

    pub(super) fn colours(
        &self,
        cs: Option<Vec<ColourRef>>,
        key: &str,
    ) -> Result<Vec<Rgba>, String> {
        let cs = need(cs, key)?;
        if cs.is_empty() {
            return Err(format!("{key}: give at least one colour"));
        }
        cs.into_iter()
            .enumerate()
            .map(|(i, c)| self.colour(Some(c), &format!("{key}[{i}]")))
            .collect()
    }

    /// The index of the face of `families` at `weight`, added if new.
    fn intern(&mut self, families: Vec<String>, weight: u32) -> usize {
        let face = Face {
            families,
            weight: weight as u16,
        };
        match self.faces.iter().position(|f| *f == face) {
            Some(i) => i,
            None => {
                self.faces.push(face);
                self.faces.len() - 1
            }
        }
    }

    /// The font role `role` (named at `key`).
    pub(super) fn font(&mut self, key: &str, role: &str) -> Result<Font, String> {
        let at = format!("fonts.{role}");
        let doc = self
            .roles
            .get(role)
            .cloned()
            .ok_or_else(|| format!("{key}: no font role '{role}' in fonts"))?;
        let families = need(doc.family, &format!("{at}.family"))?.0;
        let weight = need(doc.weight.or(Some(REGULAR)), &at)?;
        check_weight(weight, &format!("{at}.weight"))?;
        let tracking = doc.tracking.unwrap_or(0.0);
        if !(-0.5..=1.0).contains(&tracking) {
            return Err(format!(
                "{at}.tracking: {tracking} is out of range (-0.5 to 1)"
            ));
        }
        Ok(Font {
            face: self.intern(families, weight),
            size: positive(doc.size, &format!("{at}.size"))?,
            tracking,
        })
    }

    /// The face with `face`'s families at another weight.
    pub(super) fn face_at(&mut self, face: usize, weight: u32) -> Result<usize, String> {
        check_weight(weight, "match.weight")?;
        let families = self.faces[face].families.clone();
        Ok(self.intern(families, weight))
    }

    pub(super) fn role(&mut self, role: Option<String>, key: &str) -> Result<Font, String> {
        let role = need(role, key)?;
        self.font(key, &role)
    }
}

/// A length greater than 0.
pub(super) fn positive(value: Option<f32>, key: &str) -> Result<f32, String> {
    let v = length(value, key)?;
    if v > 0.0 {
        Ok(v)
    } else {
        Err(format!("{key}: must be more than 0"))
    }
}

fn check_weight(weight: u32, key: &str) -> Result<(), String> {
    if (100..=900).contains(&weight) {
        Ok(())
    } else {
        Err(format!("{key}: {weight} is no weight (100 to 900)"))
    }
}
