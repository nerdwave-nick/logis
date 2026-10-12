//! Each element of a theme document resolved: lengths and strengths
//! checked, colours and paints looked up, parts that may be off turned
//! into `Option`s.

use super::{Resolver, length, millis, need, positive, unit};
use crate::theme::doc::{self, Toggle};
use crate::theme::model::*;

impl Resolver<'_> {
    pub(super) fn window(&self, w: doc::WindowDoc) -> Result<Window, String> {
        let rows = need(w.rows, "window.rows")?;
        if rows == 0 {
            return Err("window.rows: show at least one row".into());
        }
        Ok(Window {
            width: positive(w.width, "window.width")?,
            height: positive(w.height, "window.height")?,
            rows,
            margin: length(w.margin, "window.margin")?,
        })
    }

    pub(super) fn glow(&self, g: doc::GlowDoc) -> Result<Glow, String> {
        Ok(Glow {
            color: self.colour(g.color, "glow.color")?,
            radius: length(g.radius, "glow.radius")?,
            pulse: unit(g.pulse, "glow.pulse")?,
            period: millis(g.period_ms, "glow.period-ms")?,
        })
    }

    pub(super) fn border(&self, b: doc::BorderDoc) -> Result<Border, String> {
        let highlight = match need(b.highlight, "border.highlight")? {
            Toggle::Off => None,
            Toggle::On(h) => Some(Highlight {
                color: self.colour(h.color, "border.highlight.color")?,
                length: length(h.length, "border.highlight.length")?,
                period: millis(h.period_ms, "border.highlight.period-ms")?,
            }),
        };
        Ok(Border {
            width: length(b.width, "border.width")?,
            paint: self.paint(b.paint, "border.paint")?,
            highlight,
        })
    }

    pub(super) fn panel(&self, p: doc::PanelDoc) -> Result<Panel, String> {
        Ok(Panel {
            fill: self.paint(p.fill, "panel.fill")?,
            radius: length(p.radius, "panel.radius")?,
            padding: length(p.padding, "panel.padding")?,
            blur: need(p.blur, "panel.blur")?,
        })
    }

    pub(super) fn rivets(&self, d: doc::DecorationsDoc) -> Result<Option<Rivets>, String> {
        let Toggle::On(r) = need(d.rivets, "decorations.rivets")? else {
            return Ok(None);
        };
        Ok(Some(Rivets {
            size: length(r.size, "decorations.rivets.size")?,
            inset: length(r.inset, "decorations.rivets.inset")?,
            top: self.paint(r.top, "decorations.rivets.top")?,
            bottom: self.paint(r.bottom, "decorations.rivets.bottom")?,
        }))
    }

    pub(super) fn prompt(&mut self, p: doc::PromptDoc) -> Result<Prompt, String> {
        let sigil = match need(p.sigil, "prompt.sigil")? {
            Toggle::Off => None,
            Toggle::On(s) => Some(Sigil {
                image: need(s.image, "prompt.sigil.image")?,
                size: positive(s.size, "prompt.sigil.size")?,
                color: self.colour(s.color, "prompt.sigil.color")?,
            }),
        };
        Ok(Prompt {
            height: positive(p.height, "prompt.height")?,
            label: need(p.label, "prompt.label")?,
            font: self.role(p.font, "prompt.font")?,
            color: self.colour(p.color, "prompt.color")?,
            gap: length(p.gap, "prompt.gap")?,
            sigil,
        })
    }

    pub(super) fn query(&mut self, q: doc::QueryDoc) -> Result<Query, String> {
        let c = need(q.caret, "query.caret")?;
        Ok(Query {
            font: self.role(q.font, "query.font")?,
            color: self.colour(q.color, "query.color")?,
            placeholder: self.colour(q.placeholder, "query.placeholder")?,
            caret: Caret {
                shape: need(c.shape, "query.caret.shape")?,
                width: length(c.width, "query.caret.width")?,
                color: self.colour(c.color, "query.caret.color")?,
                glow: self.colour(c.glow, "query.caret.glow")?,
            },
        })
    }

    pub(super) fn counter(&mut self, c: doc::CounterDoc) -> Result<Counter, String> {
        Ok(Counter {
            font: self.role(c.font, "counter.font")?,
            color: self.colour(c.color, "counter.color")?,
            format: need(c.format, "counter.format")?,
        })
    }

    pub(super) fn rule(&self, r: doc::RuleDoc) -> Result<Rule, String> {
        let dash = match need(r.dash, "rule.dash")? {
            Toggle::Off => None,
            Toggle::On([on, off]) => {
                let on = positive(Some(on), "rule.dash[0]")?;
                Some([on, length(Some(off), "rule.dash[1]")?])
            }
        };
        let ornament = match need(r.ornament, "rule.ornament")? {
            Toggle::Off => None,
            Toggle::On(o) => Some(Ornament {
                size: length(o.size, "rule.ornament.size")?,
                fill: self.colour(o.fill, "rule.ornament.fill")?,
                edge: self.colour(o.edge, "rule.ornament.edge")?,
            }),
        };
        Ok(Rule {
            paint: self.paint(r.paint, "rule.paint")?,
            thickness: length(r.thickness, "rule.thickness")?,
            inset: length(r.inset, "rule.inset")?,
            dash,
            ornament,
        })
    }

    pub(super) fn row(&self, r: doc::RowDoc, font: Font) -> Result<Row, String> {
        Ok(Row {
            font,
            height: positive(r.height, "row.height")?,
            gap: length(r.gap, "row.gap")?,
            padding: length(r.padding, "row.padding")?,
            columns: self.colours(r.columns, "row.columns")?,
            glyph: self.colour(r.glyph, "row.glyph")?,
            column_gap: length(r.column_gap, "row.column-gap")?,
            column_max: share(r.column_max, "row.column-max")?,
        })
    }

    pub(super) fn selection(&self, s: doc::SelectionDoc) -> Result<Selection, String> {
        Ok(Selection {
            band: self.paint(s.band, "selection.band")?,
            outline: self.colour(s.outline, "selection.outline")?,
            edge: self.colour(s.edge, "selection.edge")?,
            glow: self.colour(s.glow, "selection.glow")?,
            columns: self.colours(s.columns, "selection.columns")?,
            inset: length(s.inset, "selection.inset")?,
        })
    }

    pub(super) fn badge(&mut self, b: doc::BadgeDoc) -> Result<Badge, String> {
        Ok(Badge {
            font: self.role(b.font, "badge.font")?,
            style: need(b.style, "badge.style")?,
            ink: self.colour(b.ink, "badge.ink")?,
            edge: self.colour(b.edge, "badge.edge")?,
            fill: self.colour(b.fill, "badge.fill")?,
            uppercase: need(b.uppercase, "badge.uppercase")?,
        })
    }

    pub(super) fn pending(&self, p: doc::PendingDoc) -> Result<Pending, String> {
        Ok(Pending {
            color: self.colour(p.color, "pending.color")?,
            opacity: unit(p.opacity, "pending.opacity")?,
            shimmer: unit(p.shimmer, "pending.shimmer")?,
            period: millis(p.period_ms, "pending.period-ms")?,
            mark: need(p.mark, "pending.mark")?,
        })
    }

    pub(super) fn effects(&self, e: doc::EffectsDoc) -> Result<Effects, String> {
        Ok(Effects {
            scanlines: unit(e.scanlines, "effects.scanlines")?,
            grain: unit(e.grain, "effects.grain")?,
            flicker: unit(e.flicker, "effects.flicker")?,
            vignette: unit(e.vignette, "effects.vignette")?,
            bloom: unit(e.bloom, "effects.bloom")?,
            curvature: unit(e.curvature, "effects.curvature")?,
            aberration: unit(e.aberration, "effects.aberration")?,
        })
    }
}

pub(super) fn motion(m: doc::MotionDoc) -> Result<Motion, String> {
    Ok(Motion {
        open: millis(m.open_ms, "motion.open-ms")?,
        close: millis(m.close_ms, "motion.close-ms")?,
        select: millis(m.select_ms, "motion.select-ms")?,
    })
}

/// A share of a whole: more than 0, at most 1.
fn share(value: Option<f32>, key: &str) -> Result<f32, String> {
    let v = need(value, key)?;
    if v > 0.0 && v <= 1.0 {
        Ok(v)
    } else {
        Err(format!(
            "{key}: {v} is no share of the row (more than 0, at most 1)"
        ))
    }
}
