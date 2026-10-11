//! A theme file as written: every key optional, each value checked for its
//! kind while serde reads it (so serde_json can say where a value is
//! wrong), unknown keys refused. A theme lies over the theme it extends and
//! over the bundled default ([`Merge`]); the result is resolved in
//! [`super::resolve`].

use std::collections::BTreeMap;
use std::fmt;

use serde::de::{self, Deserializer, Visitor};
use serde::{Deserialize, Serialize, Serializer};

use super::colour::{ColourRef, PaintRef, Palette};

/// A value that lies over another of the same kind: a section key by key,
/// anything else whole.
pub trait Merge {
    /// `self` where it says something, `base` elsewhere.
    fn over(self, base: Self) -> Self;
}

impl<T: Merge> Merge for Option<T> {
    fn over(self, base: Self) -> Self {
        match (self, base) {
            (Some(a), Some(b)) => Some(a.over(b)),
            (a, b) => a.or(b),
        }
    }
}

macro_rules! whole {
    ($($t:ty),*) => {$(
        impl Merge for $t {
            fn over(self, _base: Self) -> Self {
                self
            }
        }
    )*};
}

whole!(
    String,
    f32,
    u32,
    bool,
    ColourRef,
    PaintRef,
    Families,
    Vec<ColourRef>,
    [f32; 2],
    CaretShape,
    BadgeStyle
);

impl Merge for Palette {
    fn over(self, base: Self) -> Self {
        Palette::over(self, base)
    }
}

impl<T: Merge> Merge for BTreeMap<String, T> {
    fn over(mut self, base: Self) -> Self {
        for (key, value) in base {
            let merged = match self.remove(&key) {
                Some(mine) => mine.over(value),
                None => value,
            };
            self.insert(key, merged);
        }
        self
    }
}

/// A part of a theme that may be turned off: `false`, or its settings.
#[derive(Clone, Debug, PartialEq)]
pub enum Toggle<T> {
    /// Not drawn.
    Off,
    /// Drawn with these settings (missing ones come from the base).
    On(T),
}

impl<T: Merge> Merge for Toggle<T> {
    fn over(self, base: Self) -> Self {
        match (self, base) {
            (Self::On(a), Self::On(b)) => Self::On(a.over(b)),
            (mine, _) => mine,
        }
    }
}

impl<'de, T: Deserialize<'de>> Deserialize<'de> for Toggle<T> {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        #[derive(Deserialize)]
        #[serde(untagged)]
        enum Either<T> {
            Flag(bool),
            On(T),
        }
        // Untagged enums lose serde_json's position for a bad inner value;
        // a theme author gets the key from the message instead.
        match Either::deserialize(d)? {
            Either::Flag(false) => Ok(Self::Off),
            Either::Flag(true) => Err(de::Error::custom(
                "true is not a setting here: give the settings as an object, or false to turn it off",
            )),
            Either::On(t) => Ok(Self::On(t)),
        }
    }
}

impl<T: Serialize> Serialize for Toggle<T> {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        match self {
            Self::Off => s.serialize_bool(false),
            Self::On(t) => t.serialize(s),
        }
    }
}

/// Font families in order of preference: a name or an array of names; the
/// last is usually a generic family (`monospace`, `serif`, `sans-serif`).
#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(transparent)]
pub struct Families(pub Vec<String>);

impl<'de> Deserialize<'de> for Families {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct V;
        impl<'de> Visitor<'de> for V {
            type Value = Families;
            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("a font family, or an array of families in order of preference")
            }
            fn visit_str<E: de::Error>(self, v: &str) -> Result<Families, E> {
                if v.is_empty() {
                    return Err(E::invalid_value(de::Unexpected::Str(v), &self));
                }
                Ok(Families(vec![v.to_owned()]))
            }
            fn visit_seq<A: de::SeqAccess<'de>>(self, mut seq: A) -> Result<Families, A::Error> {
                let mut names = Vec::new();
                while let Some(name) = seq.next_element::<String>()? {
                    if name.is_empty() {
                        return Err(de::Error::invalid_value(de::Unexpected::Str(""), &self));
                    }
                    names.push(name);
                }
                if names.is_empty() {
                    return Err(de::Error::invalid_length(0, &self));
                }
                Ok(Families(names))
            }
        }
        d.deserialize_any(V)
    }
}

/// The caret's form.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum CaretShape {
    /// A thin upright bar.
    Bar,
    /// A block a character wide.
    Block,
    /// A low bar a character wide.
    Underscore,
}

/// How a badge is drawn.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum BadgeStyle {
    /// A pill with an edge.
    Outline,
    /// A filled pill.
    Fill,
    /// Its text in square brackets, no pill.
    Bracket,
}

/// Defines a section: a struct of optional keys (kebab-case in the file),
/// unknown keys refused, merged key by key.
macro_rules! section {
    ($(#[$m:meta])* $name:ident { $($(#[$fm:meta])* $field:ident: $ty:ty,)* }) => {
        $(#[$m])*
        #[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
        #[serde(deny_unknown_fields, rename_all = "kebab-case")]
        pub struct $name {
            $($(#[$fm])* #[serde(default, skip_serializing_if = "Option::is_none")]
            pub $field: Option<$ty>,)*
        }

        impl Merge for $name {
            fn over(self, base: Self) -> Self {
                Self { $($field: self.$field.over(base.$field),)* }
            }
        }
    };
}

section!(
    /// A whole theme file.
    Doc {
        #[serde(rename = "$schema")]
        schema: String,
        extends: String,
        palette: Palette,
        fonts: BTreeMap<String, FontDoc>,
        window: WindowDoc,
        glow: GlowDoc,
        border: BorderDoc,
        panel: PanelDoc,
        decorations: DecorationsDoc,
        prompt: PromptDoc,
        query: QueryDoc,
        counter: CounterDoc,
        rule: RuleDoc,
        row: RowDoc,
        selection: SelectionDoc,
        #[serde(rename = "match")]
        matched: MatchDoc,
        badge: BadgeDoc,
        pending: PendingDoc,
        effects: EffectsDoc,
        motion: MotionDoc,
    }
);

section!(FontDoc {
    family: Families,
    size: f32,
    weight: u32,
    tracking: f32,
});
section!(WindowDoc {
    width: f32,
    height: f32,
    rows: u32,
    margin: f32,
});
section!(GlowDoc {
    color: ColourRef,
    radius: f32,
    pulse: f32,
    period_ms: u32,
});
section!(BorderDoc { width: f32, paint: PaintRef, highlight: Toggle<HighlightDoc>, });
section!(HighlightDoc {
    color: ColourRef,
    length: f32,
    period_ms: u32,
});
section!(PanelDoc {
    fill: PaintRef,
    radius: f32,
    padding: f32,
    blur: bool,
});
section!(DecorationsDoc { rivets: Toggle<RivetsDoc>, });
section!(RivetsDoc {
    size: f32,
    inset: f32,
    top: PaintRef,
    bottom: PaintRef,
});
section!(PromptDoc {
    height: f32,
    label: String,
    font: String,
    color: ColourRef,
    gap: f32,
    sigil: Toggle<SigilDoc>,
});
section!(SigilDoc {
    image: String,
    size: f32,
    color: ColourRef,
});
section!(QueryDoc {
    font: String,
    color: ColourRef,
    placeholder: ColourRef,
    caret: CaretDoc,
});
section!(CaretDoc {
    shape: CaretShape,
    width: f32,
    color: ColourRef,
    glow: ColourRef,
});
section!(CounterDoc {
    font: String,
    color: ColourRef,
    format: String,
});
section!(RuleDoc {
    paint: PaintRef,
    thickness: f32,
    inset: f32,
    dash: Toggle<[f32; 2]>,
    ornament: Toggle<OrnamentDoc>,
});
section!(OrnamentDoc {
    size: f32,
    fill: ColourRef,
    edge: ColourRef,
});
section!(RowDoc {
    font: String,
    height: f32,
    gap: f32,
    padding: f32,
    columns: Vec<ColourRef>,
    glyph: ColourRef,
});
section!(SelectionDoc {
    band: PaintRef,
    outline: ColourRef,
    edge: ColourRef,
    glow: ColourRef,
    columns: Vec<ColourRef>,
    inset: f32,
});
section!(MatchDoc {
    color: ColourRef,
    selected: ColourRef,
    weight: u32,
    underline: bool,
});
section!(BadgeDoc {
    font: String,
    style: BadgeStyle,
    ink: ColourRef,
    edge: ColourRef,
    fill: ColourRef,
    uppercase: bool,
});
section!(PendingDoc {
    color: ColourRef,
    opacity: f32,
    shimmer: f32,
    period_ms: u32,
    mark: String,
});
section!(EffectsDoc {
    scanlines: f32,
    grain: f32,
    flicker: f32,
    vignette: f32,
    bloom: f32,
    curvature: f32,
    aberration: f32,
});
section!(MotionDoc {
    open_ms: u32,
    close_ms: u32,
    select_ms: u32,
});

#[cfg(test)]
mod tests {
    use super::*;

    fn doc(json: &str) -> Doc {
        serde_json::from_str(json).unwrap()
    }

    #[test]
    fn a_section_lies_over_its_base_key_by_key() {
        let mine = doc(r#"{"glow": {"radius": 9}, "window": {"rows": 4}}"#);
        let base = doc(r#"{"glow": {"radius": 22, "pulse": 0.15}, "motion": {"open-ms": 150}}"#);
        let both = mine.over(base);
        let glow = both.glow.unwrap();
        assert_eq!((glow.radius, glow.pulse), (Some(9.0), Some(0.15)));
        assert_eq!(both.window.unwrap().rows, Some(4));
        assert_eq!(both.motion.unwrap().open_ms, Some(150));
    }

    #[test]
    fn false_turns_a_part_off_whatever_the_base_says() {
        let mine = doc(r#"{"decorations": {"rivets": false}}"#);
        let base = doc(r#"{"decorations": {"rivets": {"size": 6, "inset": 10}}}"#);
        let rivets = mine.over(base).decorations.unwrap().rivets;
        assert_eq!(rivets, Some(Toggle::Off));
    }

    #[test]
    fn settings_over_a_part_that_is_on_merge_and_over_off_replace() {
        let mine = doc(r#"{"decorations": {"rivets": {"size": 4}}}"#);
        let on = doc(r#"{"decorations": {"rivets": {"size": 6, "inset": 10}}}"#);
        let Some(Toggle::On(r)) = mine.clone().over(on).decorations.unwrap().rivets else {
            panic!("rivets are on")
        };
        assert_eq!((r.size, r.inset), (Some(4.0), Some(10.0)));
        let off = doc(r#"{"decorations": {"rivets": false}}"#);
        let Some(Toggle::On(r)) = mine.over(off).decorations.unwrap().rivets else {
            panic!("rivets are on")
        };
        assert_eq!((r.size, r.inset), (Some(4.0), None));
    }

    #[test]
    fn true_is_no_setting() {
        let e = serde_json::from_str::<Doc>(r#"{"decorations": {"rivets": true}}"#).unwrap_err();
        assert!(e.to_string().contains("false to turn it off"), "{e}");
    }

    #[test]
    fn fonts_merge_by_role_and_palettes_by_name() {
        let mine = doc(r##"{"fonts": {"body": {"size": 20}}, "palette": {"brass": "#000000"}}"##);
        let base = doc(
            r##"{"fonts": {"body": {"family": "Mono", "size": 16}, "label": {"size": 9}},
                 "palette": {"brass": "#d4a017", "gold": "#f2c14e"}}"##,
        );
        let both = mine.over(base);
        let fonts = both.fonts.unwrap();
        assert_eq!(fonts["body"].size, Some(20.0));
        assert_eq!(fonts["body"].family, Some(Families(vec!["Mono".into()])));
        assert!(fonts.contains_key("label"));
        assert_eq!(both.palette.unwrap().0.len(), 2);
    }

    #[test]
    fn unknown_keys_and_wrong_kinds_are_refused_with_their_place() {
        let e = serde_json::from_str::<Doc>("{\n \"glow\": {\"radus\": 1}}").unwrap_err();
        assert!(e.to_string().contains("unknown field `radus`"), "{e}");
        assert_eq!(e.line(), 2);
        let e = serde_json::from_str::<Doc>(r#"{"row": {"height": "tall"}}"#).unwrap_err();
        assert!(e.to_string().contains("invalid type"), "{e}");
        let e =
            serde_json::from_str::<Doc>(r#"{"query": {"caret": {"shape": "star"}}}"#).unwrap_err();
        assert!(e.to_string().contains("`bar`"), "{e}");
        assert!(serde_json::from_str::<Doc>(r#"{"fonts": {"body": {"family": []}}}"#).is_err());
    }
}
