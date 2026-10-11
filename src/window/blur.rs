//! Blur behind the cogitator (spec §4.2, ext-background-effect-v1, used
//! when offered): the compositor blurs what lies behind the panel's body,
//! a rounded rectangle. A `wl_region` is a set of rectangles in whole
//! surface-local (logical) pixels, so the rounded corners are stepped one
//! logical pixel row at a time, always inside the curve: the blur never
//! shows outside the body. The region follows the body as it moves and
//! as the surface is resized or rescaled. A compositor without the
//! protocol simply gets no blur.

use smithay_client_toolkit::compositor::{CompositorState, Region};
use smithay_client_toolkit::reexports::client::protocol::wl_surface::WlSurface;
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, QueueHandle};
use smithay_client_toolkit::reexports::protocols::ext::background_effect::v1::client::{
    ext_background_effect_manager_v1::{self, ExtBackgroundEffectManagerV1},
    ext_background_effect_surface_v1::ExtBackgroundEffectSurfaceV1,
};

use super::Windows;
use super::trace::emit;

/// The area behind a panel to blur: its body, in logical pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Backdrop {
    /// Left, top, width, height.
    pub rect: [f32; 4],
    /// The corner radius.
    pub radius: f32,
}

/// The rectangles (x, y, width, height; whole logical pixels) that fill
/// `backdrop` without leaving it: each corner row is inset to where the
/// arc runs at that row's outer edge.
pub fn region_rects(backdrop: Backdrop) -> Vec<[i32; 4]> {
    let [x, y, w, h] = backdrop.rect;
    let (left, top) = (x.ceil(), y.ceil());
    let (right, bottom) = ((x + w).floor(), (y + h).floor());
    if right <= left || bottom <= top {
        return Vec::new();
    }
    let r = backdrop.radius.clamp(0.0, w.min(h) / 2.0);
    let rows = r.ceil().min(((bottom - top) / 2.0).floor()) as i32;
    let rect =
        |l: f32, t: f32, r: f32, b: f32| [l as i32, t as i32, (r - l) as i32, (b - t) as i32];
    let mut out = Vec::new();
    // Row i's outer edge lies i logical pixels inside the body's edge.
    let inset = |i: i32| {
        let dy = (r - i as f32).max(0.0);
        (r - (r * r - dy * dy).max(0.0).sqrt()).ceil()
    };
    for i in 0..rows {
        let d = inset(i);
        let row = top + i as f32;
        out.push(rect(left + d, row, right - d, row + 1.0));
    }
    let middle = (top + rows as f32, bottom - rows as f32);
    if middle.1 > middle.0 {
        out.push(rect(left, middle.0, right, middle.1));
    }
    for i in (0..rows).rev() {
        let d = inset(i);
        let row = bottom - 1.0 - i as f32;
        out.push(rect(left + d, row, right - d, row + 1.0));
    }
    out.retain(|r| r[2] > 0 && r[3] > 0);
    out
}

/// The bound ext_background_effect_manager_v1.
pub(super) struct Effects {
    pub(super) manager: ExtBackgroundEffectManagerV1,
}

/// One surface's blur: its effect object and the region last set.
pub(super) struct Blur {
    effect: ExtBackgroundEffectSurfaceV1,
    set: Option<Vec<[i32; 4]>>,
}

impl Blur {
    /// The effect object for `surface`, with no region yet.
    pub(super) fn new(effects: &Effects, surface: &WlSurface, qh: &QueueHandle<Windows>) -> Self {
        Self {
            effect: effects.manager.get_background_effect(surface, qh, ()),
            set: None,
        }
    }

    /// Sets the blur region to `backdrop` for the next commit, unless it is
    /// already that.
    pub(super) fn follow(&mut self, compositor: &CompositorState, backdrop: Backdrop) {
        let rects = region_rects(backdrop);
        if self.set.as_ref() == Some(&rects) {
            return;
        }
        let Ok(region) = Region::new(compositor) else {
            return;
        };
        for [x, y, w, h] in &rects {
            region.add(*x, *y, *w, *h);
        }
        self.effect.set_blur_region(Some(region.wl_region()));
        emit(format_args!("blur {}", rects.len()));
        self.set = Some(rects);
    }
}

impl Drop for Blur {
    /// The region goes with the next commit (or with the surface).
    fn drop(&mut self) {
        self.effect.destroy();
    }
}

/// The compositor says whether it can blur now (the test trace reports it
/// as `blur-capable true|false`). Nothing else depends on it: a region set
/// without the capability is simply not applied.
impl Dispatch<ExtBackgroundEffectManagerV1, ()> for Windows {
    fn event(
        _: &mut Self,
        _: &ExtBackgroundEffectManagerV1,
        event: ext_background_effect_manager_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let ext_background_effect_manager_v1::Event::Capabilities { flags } = event {
            let blur = ext_background_effect_manager_v1::Capability::Blur;
            let capable = flags.into_result().is_ok_and(|f| f.contains(blur));
            emit(format_args!("blur-capable {capable}"));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn covers(rects: &[[i32; 4]], x: i32, y: i32) -> bool {
        rects
            .iter()
            .any(|[rx, ry, w, h]| x >= *rx && x < rx + w && y >= *ry && y < ry + h)
    }

    #[test]
    fn a_square_cornered_body_is_one_rectangle() {
        let rects = region_rects(Backdrop {
            rect: [24.0, 24.0, 300.0, 100.0],
            radius: 0.0,
        });
        assert_eq!(rects, [[24, 24, 300, 100]]);
    }

    #[test]
    fn rounded_corners_are_stepped_inside_the_curve() {
        let b = Backdrop {
            rect: [24.0, 24.0, 300.0, 100.0],
            radius: 14.0,
        };
        let rects = region_rects(b);
        // 14 rows at the top, the middle, 14 rows at the bottom.
        assert_eq!(rects.len(), 29);
        assert_eq!(rects[14], [24, 38, 300, 72]);
        // The top row is inset the most, the 14th row hardly at all.
        assert!(rects[0][0] > rects[13][0]);
        assert_eq!(rects[0][1], 24);
        assert_eq!(rects[28][1] + rects[28][3], 124);
        // Every covered pixel's square lies inside the rounded rectangle.
        let (cx, cy) = (24.0 + 14.0, 24.0 + 14.0);
        for &[x, y, w, h] in &rects {
            for px in x..x + w {
                for py in y..y + h {
                    // The pixel corner nearest the top-left corner's centre…
                    let (fx, fy) = (px as f32, py as f32);
                    if fx < cx && fy < cy {
                        let d = ((cx - fx).powi(2) + (cy - fy).powi(2)).sqrt();
                        assert!(d <= 14.0 + 1e-3, "({px},{py}) is outside the arc");
                    }
                }
            }
        }
        // …and the body's centre and edge midpoints are blurred.
        assert!(covers(&rects, 174, 74));
        assert!(covers(&rects, 24, 74) && covers(&rects, 174, 24));
        assert!(!covers(&rects, 24, 24), "the corner pixel is outside");
    }

    #[test]
    fn a_fractional_body_is_rounded_inwards() {
        let rects = region_rects(Backdrop {
            rect: [24.4, 23.6, 100.2, 50.0],
            radius: 0.0,
        });
        assert_eq!(rects, [[25, 24, 99, 49]]);
    }

    #[test]
    fn a_radius_beyond_half_the_short_side_is_clamped() {
        let rects = region_rects(Backdrop {
            rect: [0.0, 0.0, 40.0, 10.0],
            radius: 50.0,
        });
        assert_eq!(rects.len(), 10, "every row is a corner row");
        assert!(rects.iter().all(|r| r[0] >= 0 && r[0] + r[2] <= 40));
    }

    #[test]
    fn an_empty_body_blurs_nothing() {
        let rects = region_rects(Backdrop {
            rect: [10.0, 10.0, 0.5, 30.0],
            radius: 4.0,
        });
        assert!(rects.is_empty());
    }
}
