//! The panel itself: its body with the glow (pulsing) and the border (its
//! gradient and travelling highlight), the corner rivets, the rule under
//! the prompt line, and the post-processing effects over the body.

use std::f32::consts::{FRAC_PI_2, TAU};

use super::Style;
use crate::render::{Border, Glow, Highlight, Paint, Post, Rgba, RoundedRect, Scene};
use crate::theme::Theme;

/// The body, its glow and border, then the rivets.
pub(super) fn panel(scene: &mut Scene, body: [f32; 4], style: Style<'_>) {
    let theme = style.theme;
    let [x, y, w, h] = body;
    let vertical = |stops: &[Rgba]| Paint::ramp([0.0, y], [0.0, y + h], stops);
    let glow = &theme.glow;
    // The pulse dims the glow and brings it back, never above its colour.
    let strength = 1.0 - glow.pulse * (1.0 - style.wave(glow.period));
    let highlight = theme.border.highlight.map(|hl| Highlight {
        // From the top, clockwise; at rest (no period) it stays there.
        angle: -FRAC_PI_2 + TAU * style.phase(hl.period),
        reach: hl.length,
        color: hl.color,
    });
    scene.shapes.push(RoundedRect {
        rect: body,
        radius: theme.panel.radius,
        fill: vertical(&theme.panel.fill),
        border: Some(Border {
            width: theme.border.width,
            paint: vertical(&theme.border.paint),
            highlight,
        }),
        glow: Some(Glow {
            radius: glow.radius,
            color: glow.color.with_alpha(glow.color.a * strength),
        }),
    });
    if let Some(r) = &theme.rivets {
        let d = r.size;
        let (left, right) = (x + r.inset, x + w - r.inset - d);
        let (top, bottom) = (y + r.inset, y + h - r.inset - d);
        for (cx, cy, stops) in [
            (left, top, &r.top),
            (right, top, &r.top),
            (left, bottom, &r.bottom),
            (right, bottom, &r.bottom),
        ] {
            scene.shapes.push(disc([cx, cy, d, d], stops));
        }
    }
}

/// A round boss lit from its top left.
fn disc(rect: [f32; 4], stops: &[Rgba]) -> RoundedRect {
    let [x, y, w, h] = rect;
    RoundedRect {
        rect,
        radius: w.min(h) / 2.0,
        fill: Paint::ramp([x + w * 0.3, y + h * 0.3], [x + w, y + h], stops),
        border: None,
        glow: None,
    }
}

/// The rule under the prompt line: its gradient across, dashed or not,
/// with its ornament at the middle.
pub(super) fn rule(scene: &mut Scene, body: [f32; 4], theme: &Theme) {
    let rule = &theme.rule;
    let [x, y, w, _] = body;
    let (left, right) = (x + rule.inset, x + w - rule.inset);
    let top = y + theme.prompt.height;
    if right <= left || rule.thickness <= 0.0 {
        return;
    }
    let paint = Paint::ramp([left, 0.0], [right, 0.0], &rule.paint);
    let bar = |from: f32, to: f32| RoundedRect {
        rect: [from, top, to - from, rule.thickness],
        radius: 0.0,
        fill: paint,
        border: None,
        glow: None,
    };
    match rule.dash {
        None => scene.shapes.push(bar(left, right)),
        Some([on, off]) => {
            let mut at = left;
            while at < right {
                scene.shapes.push(bar(at, (at + on).min(right)));
                at += on + off;
            }
        }
    }
    if let Some(o) = &rule.ornament {
        let centre = (left + right) / 2.0;
        let mid = top + rule.thickness / 2.0;
        let half = o.size / 2.0;
        scene.shapes.push(RoundedRect {
            rect: [centre - half, mid - half, o.size, o.size],
            radius: half,
            fill: Paint::Solid(o.fill),
            border: Some(Border {
                width: 1.0,
                paint: Paint::Solid(o.edge),
                highlight: None,
            }),
            glow: None,
        });
    }
}

/// The gear sigil, drawn by logis: a ring, a hub and eight teeth around
/// it, `size` square with its centre at `(cx, cy)`. A generic motif.
pub(super) fn gear(scene: &mut Scene, (cx, cy): (f32, f32), size: f32, color: Rgba) {
    let unit = size / 24.0;
    let ring = 5.0 * unit;
    let stroke = 2.0 * unit;
    scene.shapes.push(RoundedRect {
        rect: [cx - ring - stroke / 2.0, cy - ring - stroke / 2.0, 2.0 * ring + stroke, 2.0 * ring + stroke],
        radius: ring + stroke / 2.0,
        fill: Paint::Solid(Rgba::TRANSPARENT),
        border: Some(Border {
            width: stroke,
            paint: Paint::Solid(color),
            highlight: None,
        }),
        glow: None,
    });
    let hub = 1.6 * unit;
    scene.shapes.push(dot((cx, cy), hub, color));
    for i in 0..8 {
        let angle = i as f32 * TAU / 8.0;
        let reach = 8.6 * unit;
        let centre = (cx + reach * angle.cos(), cy + reach * angle.sin());
        scene.shapes.push(dot(centre, 1.4 * unit, color));
    }
}

fn dot((cx, cy): (f32, f32), radius: f32, color: Rgba) -> RoundedRect {
    RoundedRect {
        rect: [cx - radius, cy - radius, 2.0 * radius, 2.0 * radius],
        radius,
        fill: Paint::Solid(color),
        border: None,
        glow: None,
    }
}

/// The theme's effects over the body, if any is on; their time is the
/// frame's, in seconds within the hour.
pub(super) fn post(body: [f32; 4], style: Style<'_>) -> Option<Post> {
    let theme = style.theme;
    let e = &theme.effects;
    let post = Post {
        rect: body,
        radius: theme.panel.radius,
        scanlines: e.scanlines,
        grain: e.grain,
        flicker: e.flicker,
        vignette: e.vignette,
        bloom: e.bloom,
        curvature: e.curvature,
        aberration: e.aberration,
        time: (style.at % 3_600_000_000_000) as f32 / 1e9,
    };
    post.any().then_some(post)
}
