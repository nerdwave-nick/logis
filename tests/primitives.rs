//! The renderer primitives the themes added (spec §4.9, the canvas's
//! element → primitive table): gradients of up to four stops and a
//! highlight travelling round a border. Read back pixel by pixel; needs a
//! GPU adapter like tests/render.rs.

mod support;

use std::f32::consts::FRAC_PI_2;

use logis::render::{
    Border, Highlight, Image, OFFSCREEN_FORMAT, Paint, Renderer, Rgba, RoundedRect, Scale, Scene,
    render_offscreen,
};
use support::golden;

const LOGICAL: (u32, u32) = (100, 80);
const RED: Rgba = Rgba::hex(0xff0000);
const GREEN: Rgba = Rgba::hex(0x00ff00);
const BLUE: Rgba = Rgba::hex(0x0000ff);
const WHITE: Rgba = Rgba::hex(0xffffff);

fn render(scene: &Scene) -> Option<Image> {
    let gpu = golden::env_gpu()?;
    let mut renderer = Renderer::new(&gpu);
    Some(
        render_offscreen(
            &gpu,
            &mut renderer,
            scene,
            LOGICAL,
            Scale::ONE,
            OFFSCREEN_FORMAT,
        )
        .unwrap(),
    )
}

fn shape(fill: Paint, border: Option<Border>) -> Scene {
    Scene {
        clear: Some(Rgba::hex(0)),
        shapes: vec![RoundedRect {
            rect: [10.0, 10.0, 80.0, 60.0],
            radius: 0.0,
            fill,
            border,
            glow: None,
        }],
        ..Scene::default()
    }
}

#[track_caller]
fn near(actual: [u8; 4], expected: [u8; 4], tolerance: u8) {
    let close = actual
        .iter()
        .zip(expected)
        .all(|(a, e)| a.abs_diff(e) <= tolerance);
    assert!(
        close,
        "{actual:?} is not within {tolerance} of {expected:?}"
    );
}

#[test]
fn one_or_two_stops_are_the_paints_there_were() {
    let start = [0.0, 0.0];
    let end = [0.0, 10.0];
    assert_eq!(Paint::ramp(start, end, &[RED]), Paint::Solid(RED));
    assert_eq!(
        Paint::ramp(start, end, &[RED, BLUE]),
        Paint::Linear {
            start,
            end,
            from: RED,
            to: BLUE
        }
    );
    let Paint::Ramp(ramp) = Paint::ramp(start, end, &[RED, GREEN, BLUE, WHITE, RED]) else {
        panic!("three or more stops are a ramp")
    };
    assert_eq!(ramp.stops(), [RED, GREEN, BLUE, WHITE], "four at most");
}

#[test]
fn a_ramp_passes_through_each_of_its_stops_evenly_spaced() {
    let fill = Paint::ramp([10.0, 0.0], [90.0, 0.0], &[RED, GREEN, BLUE]);
    let Some(image) = render(&shape(fill, None)) else {
        return;
    };
    near(image.pixel(10, 40), [255, 0, 0, 255], 8);
    near(image.pixel(50, 40), [0, 255, 0, 255], 8);
    near(image.pixel(89, 40), [0, 6, 249, 255], 8);
    // Halfway between the first two stops: half red, half green.
    near(image.pixel(30, 40), [128, 128, 0, 255], 8);
    // Between the last two.
    near(image.pixel(70, 40), [0, 128, 128, 255], 8);
}

#[test]
fn a_border_ramp_runs_top_to_bottom_through_four_stops() {
    let paint = Paint::ramp([0.0, 10.0], [0.0, 70.0], &[RED, GREEN, BLUE, WHITE]);
    let border = Border {
        width: 4.0,
        paint,
        highlight: None,
    };
    let Some(image) = render(&shape(Paint::Solid(Rgba::hex(0)), Some(border))) else {
        return;
    };
    // The left border at the stops' heights: 10, 30, 50, 70.
    near(image.pixel(11, 10), [255, 0, 0, 255], 16);
    near(image.pixel(11, 30), [0, 255, 0, 255], 16);
    near(image.pixel(11, 50), [0, 0, 255, 255], 16);
    near(image.pixel(11, 69), [255, 255, 255, 255], 20);
}

fn highlighted(angle: f32) -> Scene {
    let border = Border {
        width: 4.0,
        paint: Paint::Solid(Rgba::hex(0x400000)),
        highlight: Some(Highlight {
            angle,
            reach: 15.0,
            color: WHITE,
        }),
    };
    shape(Paint::Solid(Rgba::hex(0)), Some(border))
}

#[test]
fn a_highlight_brightens_the_border_where_it_stands_only() {
    let dark = [0x40, 0, 0, 255];
    // At the top (-π/2: angles turn clockwise from the right, y down).
    let Some(top) = render(&highlighted(-FRAC_PI_2)) else {
        return;
    };
    let lit = top.pixel(50, 11);
    assert!(lit[1] > 200 && lit[2] > 200, "the top centre is {lit:?}");
    near(top.pixel(50, 68), dark, 2);
    near(top.pixel(11, 40), dark, 2);
    // At the right.
    let Some(right) = render(&highlighted(0.0)) else {
        return;
    };
    let lit = right.pixel(88, 40);
    assert!(lit[1] > 200, "the right middle is {lit:?}");
    near(right.pixel(50, 11), dark, 2);
    // Only the border: the inside stays black.
    near(right.pixel(80, 40), [0, 0, 0, 255], 0);
}
