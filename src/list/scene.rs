//! The list drawn as a cogitator panel in a theme (spec §4.9): the body
//! with its glow, border and ornaments; the prompt line with its sigil,
//! label, query, caret and counter; a rule; the rows below it, the selected
//! one on a band, matched characters set apart; badges, pending rows and a
//! toast. Rows are single lines whose tabs advance to tab stops, so
//! tab-separated columns line up and take the theme's column colours.
//!
//! Every length is the theme's, in logical pixels. A scene is drawn at a
//! moment ([`Style::at`]) so the theme's continuous effects (glow pulse,
//! travelling highlight, flicker, shimmer) move with time; at a frozen
//! moment it is a still image.

mod chrome;
mod marks;
mod prompt;
mod rows;

use super::Options;
use super::view::Look;
use crate::icons::{IconSource, NoIcons};
use crate::motion::Nanos;
use crate::render::{Overflow, Rgba, Scene, Text};
use crate::theme::{Font, Theme};

/// A theme at a moment: what a frame is drawn in.
#[derive(Clone, Copy, Debug)]
pub struct Style<'a> {
    /// The theme (already stilled when motion is reduced).
    pub theme: &'a Theme,
    /// The frame's time (CLOCK_MONOTONIC ns): the phase of the continuous
    /// effects.
    pub at: Nanos,
}

impl Style<'_> {
    /// Where a cycle of `period` stands at this moment, 0 to 1 (always 0 for
    /// a zero period).
    fn phase(&self, period: std::time::Duration) -> f32 {
        let period = period.as_nanos() as u64;
        if period == 0 {
            return 0.0;
        }
        (self.at % period) as f32 / period as f32
    }

    /// A slow wave of `period` between 0 and 1, 1 at the cycle's start.
    fn wave(&self, period: std::time::Duration) -> f32 {
        0.5 + 0.5 * (self.phase(period) * std::f32::consts::TAU).cos()
    }
}

/// The surface a list with these options asks for, and the most rows it
/// shows: the theme's window when the options name no size, else the
/// prompt line and `lines` rows, `width` average characters wide.
pub fn fit(options: &Options, theme: &Theme) -> ((u32, u32), u32) {
    let w = &theme.window;
    let margin = 2.0 * w.margin;
    let width = match options.width {
        Some(chars) => chars.max(1) as f32 * advance(&theme.row.font) + 2.0 * theme.panel.padding,
        None => w.width,
    };
    let height = match options.lines {
        None => w.height,
        Some(0) => theme.prompt.height,
        Some(n) => list_top(theme) + 2.0 * theme.row.gap + n as f32 * theme.row.height,
    };
    let size = (
        (width + margin).ceil() as u32,
        (height + margin).ceil() as u32,
    );
    (size, options.lines.unwrap_or(w.rows))
}

/// From the body's top to the space above the first row: the prompt line
/// and the rule.
fn list_top(theme: &Theme) -> f32 {
    theme.prompt.height + theme.rule.thickness
}

/// An average character's advance in `font` (for `--width` in characters).
fn advance(font: &Font) -> f32 {
    font.size * (0.6 + font.tracking)
}

/// Where the rows lie on a surface: the pointer's view of the list.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Grid {
    /// The body's left edge and right edge.
    pub left: f32,
    pub right: f32,
    /// The first row's top.
    pub first: f32,
    /// From one row's top to the next.
    pub pitch: f32,
    /// How many rows fit and are asked for.
    pub rows: usize,
}

impl Grid {
    /// The row in view (0 is the top one) under the surface-local point
    /// `(x, y)`, whether or not an item fills it: the band across the
    /// body's width.
    pub fn row_at(&self, (x, y): (f64, f64)) -> Option<usize> {
        let i = ((y - f64::from(self.first)) / f64::from(self.pitch)).floor();
        let inside = (f64::from(self.left)..f64::from(self.right)).contains(&x)
            && i >= 0.0
            && i < self.rows as f64;
        inside.then_some(i as usize)
    }

    /// The top of row `i` in view.
    pub fn top(&self, i: usize) -> f32 {
        self.first + i as f32 * self.pitch
    }
}

/// The panel's body (left, top, width, height) on a surface of `logical`
/// size: it fills the surface less the glow margin, so a smaller configure
/// still shows a whole panel (with fewer rows). Its corner radius is the
/// theme's: what blur goes behind.
pub fn body(logical: (u32, u32), theme: &Theme) -> ([f32; 4], f32) {
    let m = theme.window.margin;
    let w = (logical.0 as f32 - 2.0 * m).max(2.0 * theme.panel.padding + 1.0);
    let h = (logical.1 as f32 - 2.0 * m).max(theme.prompt.height);
    ([m, m, w, h], theme.panel.radius)
}

/// The rows of a surface of `logical` size showing at most `lines` rows.
pub fn grid(logical: (u32, u32), lines: u32, theme: &Theme) -> Grid {
    let ([x, y, w, h], _) = body(logical, theme);
    let first = y + list_top(theme) + theme.row.gap;
    let pitch = theme.row.height;
    let fit = ((y + h - theme.row.gap - first) / pitch).floor().max(0.0) as usize;
    Grid {
        left: x,
        right: x + w,
        first,
        pitch,
        rows: fit.min(lines as usize),
    }
}

/// Where the selection band rests: the top of the selected row in view,
/// or `None` when no row in view is selected.
pub fn band_top(look: &Look, logical: (u32, u32), theme: &Theme) -> Option<f32> {
    let g = grid(logical, look.lines, theme);
    look.rows
        .iter()
        .take(g.rows)
        .position(|row| row.selected)
        .map(|i| g.top(i))
}

/// The scene for `look` on a surface of `logical` size, the band on the
/// selected row, without icons (glyphs only).
pub fn scene(look: &Look, logical: (u32, u32), style: Style<'_>) -> Scene {
    scene_with(look, logical, style, &mut NoIcons)
}

/// [`scene`] with rows' icons drawn from `icons` (a row whose icon is not
/// found shows its glyph, if it has one).
pub fn scene_with(
    look: &Look,
    logical: (u32, u32),
    style: Style<'_>,
    icons: &mut dyn IconSource,
) -> Scene {
    let band = band_top(look, logical, style.theme);
    scene_at(look, logical, band, style, icons)
}

/// [`scene_with`] with the selection band at `band_top` (its glide toward
/// the selected row) instead of on the selected row; `None` draws no band.
pub fn scene_at(
    look: &Look,
    logical: (u32, u32),
    band_top: Option<f32>,
    style: Style<'_>,
    icons: &mut dyn IconSource,
) -> Scene {
    let theme = style.theme;
    let (body, _) = body(logical, theme);
    let grid = grid(logical, look.lines, theme);
    let mut scene = Scene {
        clear: None,
        ..Scene::default()
    };
    chrome::panel(&mut scene, body, style);
    prompt::line(&mut scene, look, body, style);
    if look.lines > 0 {
        chrome::rule(&mut scene, body, theme);
    }
    if let Some(top) = band_top {
        rows::band(&mut scene, body, top, theme);
    }
    rows::rows(&mut scene, look, &grid, style, icons);
    if let Some(notice) = &look.toast {
        marks::toast(&mut scene, notice, body, theme);
    }
    scene.post = chrome::post(body, style);
    scene
}

/// Whether the scene moves on its own at this theme: a continuous effect,
/// or a shimmering pending row in view. A window drawing it keeps drawing.
pub fn moving(look: &Look, theme: &Theme) -> bool {
    theme.is_continuous() || (theme.shimmers() && look.rows.iter().any(|r| r.pending))
}

/// An icon's side in the emblem column for rows in `font`, in logical
/// pixels.
pub fn icon_size(font: &Font) -> u32 {
    (font.size * 1.25).round() as u32
}

/// A line of text in `font`, its box centred on `centre`.
fn text(words: impl Into<String>, left: f32, centre: f32, font: &Font, color: Rgba) -> Text {
    let mut t = Text::new(words, [left, 0.0], font.size, color);
    t.origin[1] = centre - t.line_height / 2.0;
    t.face = font.face;
    t.tracking = font.tracking;
    t
}

/// [`text`] confined to a column.
fn column(t: Text, width: f32, overflow: Overflow) -> Text {
    t.in_column(width.max(1.0), overflow)
}

#[cfg(test)]
mod tests;
