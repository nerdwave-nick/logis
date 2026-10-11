//! Colours as a theme writes them: `#rrggbb` or `#rrggbbaa`, `transparent`,
//! or a palette name (`brass`, or `soot.1` for the second of a palette
//! entry's colours), each optionally followed by `/alpha` (0 to 1), which
//! multiplies the colour's own alpha: `crimson/0.55`.

use std::collections::BTreeMap;
use std::fmt;

use serde::de::{self, Deserializer, SeqAccess, Unexpected, Visitor};
use serde::{Deserialize, Serialize, Serializer};

use crate::render::Rgba;

/// A colour reference, checked for its form when read and resolved against
/// the palette once the theme is whole.
#[derive(Clone, Debug, PartialEq)]
pub struct ColourRef {
    base: Base,
    alpha: f32,
    written: String,
}

#[derive(Clone, Debug, PartialEq)]
enum Base {
    Literal(Rgba),
    Named { name: String, index: usize },
}

impl ColourRef {
    /// Reads `text`; `None` if it is no colour reference.
    pub fn parse(text: &str) -> Option<Self> {
        let (base, alpha) = match text.split_once('/') {
            Some((base, alpha)) => (base, parse_alpha(alpha)?),
            None => (text, 1.0),
        };
        let base = if base == "transparent" {
            Base::Literal(Rgba::TRANSPARENT)
        } else if let Some(hex) = base.strip_prefix('#') {
            Base::Literal(parse_hex(hex)?)
        } else {
            let (name, index) = match base.split_once('.') {
                Some((name, index)) => (name, index.parse().ok()?),
                None => (base, 0),
            };
            if !is_name(name) {
                return None;
            }
            Base::Named {
                name: name.to_owned(),
                index,
            }
        };
        Some(Self {
            base,
            alpha,
            written: text.to_owned(),
        })
    }

    /// The colour, its palette name looked up in `palette`.
    pub fn resolve(&self, palette: &Palette) -> Result<Rgba, String> {
        let colour = match &self.base {
            Base::Literal(c) => *c,
            Base::Named { name, index } => {
                let entry = palette
                    .0
                    .get(name)
                    .ok_or_else(|| format!("no colour named '{name}' in the palette"))?;
                *entry.colours().get(*index).ok_or_else(|| {
                    format!(
                        "'{name}' has {} colour(s); '{name}.{index}' names none",
                        entry.colours().len()
                    )
                })?
            }
        };
        Ok(colour.with_alpha(colour.a * self.alpha))
    }
}

/// A palette name: a lowercase letter, then lowercase letters, digits, `-`
/// or `_`. The schema's pattern says the same.
pub fn is_name(name: &str) -> bool {
    let mut chars = name.chars();
    chars.next().is_some_and(|c| c.is_ascii_lowercase())
        && chars.all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-' || c == '_')
}

fn parse_hex(hex: &str) -> Option<Rgba> {
    if !matches!(hex.len(), 6 | 8) || !hex.bytes().all(|b| b.is_ascii_hexdigit()) {
        return None;
    }
    let byte = |i: usize| u8::from_str_radix(&hex[i..i + 2], 16).ok().map(f32::from);
    let a = if hex.len() == 8 { byte(6)? } else { 255.0 };
    Some(Rgba::new(
        byte(0)? / 255.0,
        byte(2)? / 255.0,
        byte(4)? / 255.0,
        a / 255.0,
    ))
}

fn parse_alpha(text: &str) -> Option<f32> {
    let digits = |s: &str| !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit());
    let plain = match text.split_once('.') {
        Some((whole, frac)) => digits(whole) && digits(frac),
        None => digits(text),
    };
    let alpha: f32 = text.parse().ok()?;
    (plain && (0.0..=1.0).contains(&alpha)).then_some(alpha)
}

impl<'de> Deserialize<'de> for ColourRef {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        d.deserialize_any(ColourVisitor)
    }
}

impl Serialize for ColourRef {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&self.written)
    }
}

struct ColourVisitor;

impl ColourVisitor {
    const WANTS: &str = "a colour: \"#rrggbb\", \"#rrggbbaa\", \"transparent\" or a palette \
                         name such as \"brass\" or \"soot.1\", optionally with \"/alpha\" \
                         (0 to 1) such as \"crimson/0.55\"";
}

impl Visitor<'_> for ColourVisitor {
    type Value = ColourRef;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(Self::WANTS)
    }

    fn visit_str<E: de::Error>(self, v: &str) -> Result<ColourRef, E> {
        ColourRef::parse(v).ok_or_else(|| E::invalid_value(Unexpected::Str(v), &self))
    }
}

/// A paint as a theme writes it: one colour, or 1 to 4 colours as evenly
/// spaced gradient stops (each element says which way its gradient runs).
#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(transparent)]
pub struct PaintRef(pub Vec<ColourRef>);

impl PaintRef {
    /// The most stops a gradient may have (the renderer's limit).
    pub const MAX_STOPS: usize = 4;

    /// Every stop resolved against `palette`.
    pub fn resolve(&self, palette: &Palette) -> Result<Vec<Rgba>, String> {
        self.0
            .iter()
            .enumerate()
            .map(|(i, c)| c.resolve(palette).map_err(|e| format!("[{i}]: {e}")))
            .collect()
    }
}

impl<'de> Deserialize<'de> for PaintRef {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        d.deserialize_any(PaintVisitor)
    }
}

struct PaintVisitor;

impl<'de> Visitor<'de> for PaintVisitor {
    type Value = PaintRef;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "a paint: one colour, or an array of 1 to {} colours (gradient stops); \
             a colour is {}",
            PaintRef::MAX_STOPS,
            ColourVisitor::WANTS
        )
    }

    fn visit_str<E: de::Error>(self, v: &str) -> Result<PaintRef, E> {
        ColourVisitor.visit_str(v).map(|c| PaintRef(vec![c]))
    }

    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<PaintRef, A::Error> {
        let mut stops = Vec::new();
        while let Some(stop) = seq.next_element()? {
            stops.push(stop);
        }
        if stops.is_empty() || stops.len() > PaintRef::MAX_STOPS {
            return Err(de::Error::invalid_length(stops.len(), &self));
        }
        Ok(PaintRef(stops))
    }
}

/// A palette entry: one colour or several (`soot.0`, `soot.1`), each
/// `#rrggbb` or `#rrggbbaa`.
#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(transparent)]
pub struct Swatch(Vec<String>, #[serde(skip)] Vec<Rgba>);

impl Swatch {
    fn colours(&self) -> &[Rgba] {
        &self.1
    }
}

impl<'de> Deserialize<'de> for Swatch {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        d.deserialize_any(SwatchVisitor)
    }
}

struct SwatchVisitor;

impl SwatchVisitor {
    fn hex<E: de::Error>(&self, v: &str) -> Result<Rgba, E> {
        v.strip_prefix('#')
            .and_then(parse_hex)
            .ok_or_else(|| E::invalid_value(Unexpected::Str(v), self))
    }
}

impl<'de> Visitor<'de> for SwatchVisitor {
    type Value = Swatch;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("a palette colour: \"#rrggbb\" or \"#rrggbbaa\", or a non-empty array of them")
    }

    fn visit_str<E: de::Error>(self, v: &str) -> Result<Swatch, E> {
        Ok(Swatch(vec![v.to_owned()], vec![self.hex(v)?]))
    }

    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Swatch, A::Error> {
        let (mut written, mut colours) = (Vec::new(), Vec::new());
        while let Some(v) = seq.next_element::<String>()? {
            colours.push(self.hex(&v)?);
            written.push(v);
        }
        if colours.is_empty() {
            return Err(de::Error::invalid_length(0, &self));
        }
        Ok(Swatch(written, colours))
    }
}

/// The named colours of a theme.
#[derive(Clone, Debug, Default, PartialEq, Serialize)]
#[serde(transparent)]
pub struct Palette(pub BTreeMap<String, Swatch>);

impl<'de> Deserialize<'de> for Palette {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let map = BTreeMap::<String, Swatch>::deserialize(d)?;
        if let Some(bad) = map.keys().find(|k| !is_name(k)) {
            return Err(de::Error::invalid_value(
                Unexpected::Str(bad),
                &"a palette name: a lowercase letter, then lowercase letters, digits, '-' or '_'",
            ));
        }
        Ok(Self(map))
    }
}

impl Palette {
    /// This palette over `base`: its names win, `base`'s others remain.
    pub fn over(mut self, base: Self) -> Self {
        for (name, swatch) in base.0 {
            self.0.entry(name).or_insert(swatch);
        }
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn palette() -> Palette {
        serde_json::from_str(r##"{"brass": "#d4a017", "soot": ["#170e0b", "#0b080780"]}"##).unwrap()
    }

    fn colour(text: &str) -> Result<Rgba, String> {
        ColourRef::parse(text)
            .ok_or_else(|| format!("{text} is no colour"))?
            .resolve(&palette())
    }

    #[test]
    fn hex_colours_carry_an_optional_alpha_byte() {
        assert_eq!(colour("#ff8000"), Ok(Rgba::hex(0xff8000)));
        let c = colour("#ff800080").unwrap();
        assert!((c.a - 128.0 / 255.0).abs() < 1e-6);
        assert_eq!(colour("transparent"), Ok(Rgba::TRANSPARENT));
    }

    #[test]
    fn palette_names_and_indices_resolve() {
        assert_eq!(colour("brass"), Ok(Rgba::hex(0xd4a017)));
        assert_eq!(colour("soot"), Ok(Rgba::hex(0x170e0b)));
        assert_eq!(colour("soot.1").unwrap().a, 128.0 / 255.0);
        assert!(
            colour("soot.2")
                .unwrap_err()
                .contains("'soot.2' names none")
        );
        assert!(
            colour("embr")
                .unwrap_err()
                .contains("no colour named 'embr'")
        );
    }

    #[test]
    fn an_alpha_suffix_multiplies_the_alpha() {
        assert_eq!(colour("brass/0.5"), Ok(Rgba::hex(0xd4a017).with_alpha(0.5)));
        let half_of_half = colour("soot.1/0.5").unwrap().a;
        assert!((half_of_half - 64.0 / 255.0).abs() < 1e-3);
        assert_eq!(colour("#ffffff/0"), Ok(Rgba::hex(0xffffff).with_alpha(0.0)));
    }

    #[test]
    fn malformed_colours_are_no_colours() {
        for bad in [
            "",
            "#fff",
            "#12345",
            "#gggggg",
            "Brass",
            "1brass",
            "brass/",
            "brass/1.5",
            "brass/-0.1",
            "brass/.5",
            "soot.x",
            "brass/0.5/0.5",
            "#ff8000/abc",
        ] {
            assert!(ColourRef::parse(bad).is_none(), "{bad:?}");
        }
    }

    #[test]
    fn a_paint_is_one_colour_or_up_to_four_stops() {
        let one: PaintRef = serde_json::from_str(r#""brass""#).unwrap();
        assert_eq!(one.0.len(), 1);
        let three: PaintRef = serde_json::from_str(r##"["brass", "soot.0", "#000000"]"##).unwrap();
        assert_eq!(three.resolve(&palette()).unwrap().len(), 3);
        for bad in [r#"[]"#, r#"["a","b","c","d","e"]"#, "1", r##"["#00"]"##] {
            assert!(serde_json::from_str::<PaintRef>(bad).is_err(), "{bad}");
        }
        let missing: PaintRef = serde_json::from_str(r#"["brass", "embr"]"#).unwrap();
        assert!(
            missing
                .resolve(&palette())
                .unwrap_err()
                .starts_with("[1]: ")
        );
    }

    #[test]
    fn palette_entries_are_hex_only_with_lowercase_names() {
        for bad in [
            r#"{"brass": "gold"}"#,
            r##"{"Brass": "#d4a017"}"##,
            r#"{"brass": []}"#,
            r#"{"brass": 1}"#,
        ] {
            assert!(serde_json::from_str::<Palette>(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn a_palette_over_another_keeps_its_own_names() {
        let mine: Palette = serde_json::from_str(r##"{"brass": "#000000"}"##).unwrap();
        let both = mine.over(palette());
        assert_eq!(both.0.len(), 2);
        let brass = ColourRef::parse("brass").unwrap().resolve(&both);
        assert_eq!(brass, Ok(Rgba::hex(0)));
    }
}
