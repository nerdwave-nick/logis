//! The debug session: a static panel in the spirit of "forge crimson &
//! brass", drawn only from SDF shapes (text, input, themes and animation come
//! in later slices). Used to check scale handling on a live compositor.

use super::{WindowError, Windows};
use crate::render::{BackendChoice, Border, Glow, Paint, Rgba, RoundedRect, Scene};

/// The panel body in logical pixels.
const PANEL: (f32, f32) = (640.0, 360.0);
/// Room around the body for its glow.
const MARGIN: f32 = 24.0;
/// The surface the debug panel requests.
pub const DEBUG_SIZE: (u32, u32) = (
    (PANEL.0 + 2.0 * MARGIN) as u32,
    (PANEL.1 + 2.0 * MARGIN) as u32,
);

const BRASS: Rgba = Rgba::new(0.83, 0.63, 0.09, 1.0);
const CRIMSON: Rgba = Rgba::new(0.70, 0.06, 0.05, 1.0);
const SOOT_TOP: Rgba = Rgba::new(0.085, 0.055, 0.045, 0.86);
const SOOT_BOTTOM: Rgba = Rgba::new(0.045, 0.030, 0.026, 0.86);
const ASH: Rgba = Rgba::new(0.54, 0.48, 0.40, 0.28);

/// The debug panel's scene for a surface of `logical` size: the body fills
/// it up to [`PANEL`], less the glow margin, so a smaller configure than
/// requested still shows the whole panel.
pub fn debug_scene(logical: (u32, u32)) -> Scene {
    let fit = |side: u32, max: f32| (side as f32 - 2.0 * MARGIN).clamp(64.0, max);
    let panel = (fit(logical.0, PANEL.0), fit(logical.1, PANEL.1));
    let x = (logical.0 as f32 - panel.0) / 2.0;
    let y = (logical.1 as f32 - panel.1) / 2.0;
    let rect = |dx: f32, dy: f32, w: f32, h: f32, radius: f32, fill: Paint| RoundedRect {
        rect: [x + dx, y + dy, w, h],
        radius,
        fill,
        border: None,
        glow: None,
    };
    let vertical = |from: Rgba, to: Rgba| Paint::Linear {
        start: [0.0, y],
        end: [0.0, y + panel.1],
        from,
        to,
    };

    let mut body = rect(
        0.0,
        0.0,
        panel.0,
        panel.1,
        14.0,
        vertical(SOOT_TOP, SOOT_BOTTOM),
    );
    body.border = Some(Border {
        width: 2.0,
        paint: vertical(BRASS, CRIMSON),
    });
    body.glow = Some(Glow {
        radius: MARGIN - 2.0,
        color: CRIMSON.with_alpha(0.55),
    });
    let rune = rect(20.0, 18.0, 16.0, 16.0, 3.0, Paint::Solid(BRASS));
    let rule = rect(
        18.0,
        52.0,
        panel.0 - 36.0,
        1.5,
        0.75,
        Paint::Solid(BRASS.with_alpha(0.8)),
    );
    let mut selected = rect(
        2.0,
        66.0,
        panel.0 - 4.0,
        26.0,
        2.0,
        Paint::Linear {
            start: [x, 0.0],
            end: [x + panel.0, 0.0],
            from: Rgba::new(0.30, 0.07, 0.05, 0.9),
            to: Rgba::new(0.18, 0.05, 0.04, 0.6),
        },
    );
    selected.border = Some(Border {
        width: 1.0,
        paint: Paint::Solid(CRIMSON.with_alpha(0.6)),
    });
    let mut marker = rect(10.0, 70.0, 3.0, 18.0, 1.5, Paint::Solid(BRASS));
    marker.glow = Some(Glow {
        radius: 6.0,
        color: BRASS.with_alpha(0.7),
    });

    let mut shapes = vec![body, rune, rule, selected, marker];
    for (row, width) in [
        0.62_f32, 0.48, 0.71, 0.55, 0.66, 0.40, 0.58, 0.50, 0.69, 0.44,
    ]
    .into_iter()
    .enumerate()
    {
        let dy = 75.0 + 26.0 * row as f32;
        if dy + 20.0 > panel.1 {
            break;
        }
        let ash = if row == 0 {
            BRASS.with_alpha(0.75)
        } else {
            ASH
        };
        shapes.push(rect(
            22.0,
            dy,
            (panel.0 - 60.0) * width,
            8.0,
            4.0,
            Paint::Solid(ash),
        ));
    }
    Scene {
        clear: None,
        shapes,
    }
}

/// `logis daemon --debug-panel`: wakes the GPU, shows the static panel and
/// keeps it, re-rendering only on configure or scale changes, until the
/// compositor closes it or the process is ended.
pub fn run_debug_panel(choice: BackendChoice) -> Result<(), WindowError> {
    let (mut windows, mut queue) = Windows::connect(choice)?;
    let info = windows.gpu().info();
    eprintln!(
        "logis: the machine spirit wakes on {} ({:?}, {})",
        info.name, info.backend, info.driver
    );
    windows.set_verbose(true);
    windows.show(&queue.handle(), DEBUG_SIZE, Box::new(debug_scene))?;
    let qh = queue.handle();
    while windows.is_visible() {
        queue
            .blocking_dispatch(&mut windows)
            .map_err(|e| WindowError::Disconnected(e.to_string()))?;
        windows.draw_pending(&qh);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn at_the_requested_size_the_body_is_the_full_panel() {
        let scene = debug_scene(DEBUG_SIZE);
        assert_eq!(scene.shapes[0].rect, [MARGIN, MARGIN, PANEL.0, PANEL.1]);
    }

    #[test]
    fn a_smaller_configure_shrinks_the_body_and_keeps_its_glow_inside() {
        let scene = debug_scene((466, 300));
        let [x, y, w, h] = scene.shapes[0].rect;
        assert_eq!(
            (x, y, w, h),
            (MARGIN, MARGIN, 466.0 - 2.0 * MARGIN, 300.0 - 2.0 * MARGIN)
        );
        assert!(scene.shapes.iter().all(|s| s.rect[1] + s.rect[3] <= y + h));
    }
}
