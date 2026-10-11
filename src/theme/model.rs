//! A theme resolved for drawing: every colour an [`Rgba`], every font a
//! face and size, every part that may be off an `Option`. Lengths are
//! logical pixels (spec §4.9); the renderer multiplies by the output scale.

use std::path::PathBuf;
use std::time::Duration;

use super::doc::{BadgeStyle, CaretShape};
use crate::render::Rgba;

/// A face to load: families in order of preference (resolved by
/// fontconfig, aliases and generic families included) at a weight.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Face {
    /// Family names, the most wanted first.
    pub families: Vec<String>,
    /// The OpenType weight (400 regular, 600 semibold, 700 bold).
    pub weight: u16,
}

/// A font as an element uses it.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Font {
    /// Index into [`Theme::faces`].
    pub face: usize,
    /// The em in logical pixels.
    pub size: f32,
    /// Letter spacing in ems.
    pub tracking: f32,
}

/// Every look a cogitator can wear, resolved.
#[derive(Clone, Debug, PartialEq)]
pub struct Theme {
    /// Its name (`forge-crimson-brass`, or a user file's stem).
    pub name: String,
    /// Where its relative image paths start: the user file's directory;
    /// `None` for a bundled theme.
    pub dir: Option<PathBuf>,
    /// The faces its fonts use, each once; the first is the body's.
    pub faces: Vec<Face>,
    pub window: Window,
    pub glow: Glow,
    pub border: Border,
    pub panel: Panel,
    pub rivets: Option<Rivets>,
    pub prompt: Prompt,
    pub query: Query,
    pub counter: Counter,
    pub rule: Rule,
    pub row: Row,
    pub selection: Selection,
    pub matched: Match,
    pub badge: Badge,
    pub pending: Pending,
    pub effects: Effects,
    pub motion: Motion,
    /// The palette's `error`, for error toasts.
    pub error: Rgba,
}

/// The window's default size and how much list it shows.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Window {
    /// The panel's width (its border box), when no `--width` is given.
    pub width: f32,
    /// The panel's height, when no `--lines` is given.
    pub height: f32,
    /// The most rows shown at once, when no `--lines` is given.
    pub rows: u32,
    /// Room around the panel for its glow.
    pub margin: f32,
}

/// The glow outside the panel.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Glow {
    pub color: Rgba,
    pub radius: f32,
    /// How far the glow's strength swings, 0 (steady) to 1.
    pub pulse: f32,
    /// One swing.
    pub period: Duration,
}

/// The panel's edge.
#[derive(Clone, Debug, PartialEq)]
pub struct Border {
    pub width: f32,
    /// Top to bottom, evenly spaced.
    pub paint: Vec<Rgba>,
    pub highlight: Option<Highlight>,
}

/// A bright spot travelling around the border.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Highlight {
    pub color: Rgba,
    /// Its reach along the border, either side of its centre.
    pub length: f32,
    /// One round of the border.
    pub period: Duration,
}

/// The panel's body.
#[derive(Clone, Debug, PartialEq)]
pub struct Panel {
    /// Top to bottom, evenly spaced.
    pub fill: Vec<Rgba>,
    pub radius: f32,
    /// Left and right of the prompt line and the rows.
    pub padding: f32,
    /// Blur behind the body, when the config allows it and the compositor
    /// offers it.
    pub blur: bool,
}

/// Rivets in the body's corners.
#[derive(Clone, Debug, PartialEq)]
pub struct Rivets {
    pub size: f32,
    /// From the body's edges.
    pub inset: f32,
    /// The upper pair's paint, top-left to bottom-right.
    pub top: Vec<Rgba>,
    /// The lower pair's paint.
    pub bottom: Vec<Rgba>,
}

/// The prompt line's start.
#[derive(Clone, Debug, PartialEq)]
pub struct Prompt {
    /// The prompt line's height, from the body's top to the rule.
    pub height: f32,
    /// Shown before the session's prompt, empty for none.
    pub label: String,
    pub font: Font,
    pub color: Rgba,
    /// Between the sigil, the label and the query.
    pub gap: f32,
    pub sigil: Option<Sigil>,
}

/// The image before the prompt.
#[derive(Clone, Debug, PartialEq)]
pub struct Sigil {
    /// `gear` (drawn by logis) or an image file, relative to the theme's
    /// directory.
    pub image: String,
    pub size: f32,
    /// The colour of a sigil logis draws.
    pub color: Rgba,
}

/// The query.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Query {
    pub font: Font,
    pub color: Rgba,
    /// The placeholder's colour.
    pub placeholder: Rgba,
    pub caret: Caret,
}

/// Where the next character goes.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Caret {
    pub shape: CaretShape,
    /// A bar's width (a block or underscore is a character wide).
    pub width: f32,
    pub color: Rgba,
    pub glow: Rgba,
}

/// The match counter.
#[derive(Clone, Debug, PartialEq)]
pub struct Counter {
    pub font: Font,
    pub color: Rgba,
    /// `{matched}` and `{total}` are replaced.
    pub format: String,
}

/// The rule under the prompt line.
#[derive(Clone, Debug, PartialEq)]
pub struct Rule {
    /// Left to right, evenly spaced.
    pub paint: Vec<Rgba>,
    pub thickness: f32,
    /// From the body's left and right edges.
    pub inset: f32,
    /// Dash and gap lengths, if dashed.
    pub dash: Option<[f32; 2]>,
    pub ornament: Option<Ornament>,
}

/// A boss at the rule's middle.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Ornament {
    pub size: f32,
    pub fill: Rgba,
    pub edge: Rgba,
}

/// A row of the list.
#[derive(Clone, Debug, PartialEq)]
pub struct Row {
    pub font: Font,
    /// From one row's top to the next.
    pub height: f32,
    /// Above the first row and below the last.
    pub gap: f32,
    /// From the body's left edge to the row's text.
    pub padding: f32,
    /// Per tab-separated column; the last repeats.
    pub columns: Vec<Rgba>,
    /// An emblem's colour.
    pub glyph: Rgba,
}

/// The selected row.
#[derive(Clone, Debug, PartialEq)]
pub struct Selection {
    /// Left to right, evenly spaced.
    pub band: Vec<Rgba>,
    /// Lines above and below the band.
    pub outline: Rgba,
    /// The bar at the band's start.
    pub edge: Rgba,
    pub glow: Rgba,
    /// The selected row's column colours.
    pub columns: Vec<Rgba>,
    /// From the body's left and right edges.
    pub inset: f32,
}

/// Matched characters.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Match {
    pub color: Rgba,
    /// On the selected row.
    pub selected: Rgba,
    /// Index into [`Theme::faces`]: the row's families at the match weight.
    pub face: usize,
    pub underline: bool,
}

/// A badge at a row's end.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Badge {
    pub font: Font,
    pub style: BadgeStyle,
    pub ink: Rgba,
    pub edge: Rgba,
    pub fill: Rgba,
    pub uppercase: bool,
}

/// A row whose data has not settled.
#[derive(Clone, Debug, PartialEq)]
pub struct Pending {
    pub color: Rgba,
    /// The whole row's opacity.
    pub opacity: f32,
    /// How far the mark's opacity swings, 0 to 1.
    pub shimmer: f32,
    pub period: Duration,
    /// What the row says at its end.
    pub mark: String,
}

/// The post-processing pass over the body: each a strength, 0 is off.
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct Effects {
    pub scanlines: f32,
    pub grain: f32,
    pub flicker: f32,
    pub vignette: f32,
    pub bloom: f32,
    pub curvature: f32,
    pub aberration: f32,
}

impl Effects {
    /// Whether any effect is on (the pass runs at all).
    pub fn any(&self) -> bool {
        [
            self.scanlines,
            self.grain,
            self.flicker,
            self.vignette,
            self.bloom,
            self.curvature,
            self.aberration,
        ]
        .iter()
        .any(|s| *s > 0.0)
    }
}

/// Transition timings.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Motion {
    pub open: Duration,
    pub close: Duration,
    pub select: Duration,
}

impl Theme {
    /// Whether the theme moves on its own (glow pulse, travelling
    /// highlight, flicker), so a visible window keeps drawing. A pending
    /// row's shimmer counts only while one is shown.
    pub fn is_continuous(&self) -> bool {
        let swings = |strength: f32, period: Duration| strength > 0.0 && !period.is_zero();
        swings(self.glow.pulse, self.glow.period)
            || self.border.highlight.is_some_and(|h| !h.period.is_zero())
            || self.effects.flicker > 0.0
    }

    /// Whether a shown pending row shimmers, so the window keeps drawing.
    pub fn shimmers(&self) -> bool {
        self.pending.shimmer > 0.0 && !self.pending.period.is_zero()
    }

    /// The same theme with every continuous effect stilled (the config's
    /// `reduce-motion`): no pulse, no travelling highlight (it rests at
    /// the top), no flicker, no shimmer.
    pub fn stilled(mut self) -> Self {
        self.glow.pulse = 0.0;
        if let Some(h) = &mut self.border.highlight {
            h.period = Duration::ZERO;
        }
        self.effects.flicker = 0.0;
        self.pending.shimmer = 0.0;
        self
    }
}
