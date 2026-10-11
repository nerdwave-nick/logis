//! The post-processing pass (spec §4.3, §4.9): scanlines, grain, flicker,
//! vignette, bloom, curvature and chromatic aberration over the panel's
//! body, each a strength where 0 is off. Read back pixel by pixel; the time
//! is frozen, so every term is deterministic. Needs a GPU adapter like
//! tests/render.rs.

mod support;

use logis::render::{
    Image, OFFSCREEN_FORMAT, Paint, Post, Renderer, Rgba, RoundedRect, Scale, Scene,
    render_offscreen,
};
use support::golden;

const LOGICAL: (u32, u32) = (120, 90);
/// The body the effects cover.
const BODY: [f32; 4] = [10.0, 10.0, 100.0, 70.0];

fn render_at(scene: &Scene, scale: Scale) -> Option<Image> {
    let gpu = golden::env_gpu()?;
    let mut renderer = Renderer::new(&gpu);
    Some(render_offscreen(&gpu, &mut renderer, scene, LOGICAL, scale, OFFSCREEN_FORMAT).unwrap())
}

fn render(scene: &Scene) -> Option<Image> {
    render_at(scene, Scale::ONE)
}

fn rect(rect: [f32; 4], color: Rgba) -> RoundedRect {
    RoundedRect {
        rect,
        radius: 0.0,
        fill: Paint::Solid(color),
        border: None,
        glow: None,
    }
}

/// A grey body on a black surface, with `extra` shapes over it.
fn scene(post: Option<Post>, extra: Vec<RoundedRect>) -> Scene {
    let mut shapes = vec![rect(BODY, Rgba::hex(0x808080))];
    shapes.extend(extra);
    Scene {
        clear: Some(Rgba::hex(0x000000)),
        shapes,
        post,
        ..Scene::default()
    }
}

fn post() -> Post {
    Post {
        rect: BODY,
        radius: 0.0,
        ..Post::default()
    }
}

fn luma(px: [u8; 4]) -> u32 {
    u32::from(px[0]) + u32::from(px[1]) + u32::from(px[2])
}

#[test]
fn strengths_of_zero_change_nothing() {
    let Some(plain) = render(&scene(None, vec![])) else {
        return;
    };
    let off = render(&scene(Some(post()), vec![])).unwrap();
    assert!(
        plain == off,
        "a pass with every effect off changed the image"
    );
}

#[test]
fn scanlines_darken_rows_inside_the_body_only() {
    let p = Post {
        scanlines: 0.5,
        ..post()
    };
    let Some(image) = render(&scene(Some(p), vec![])) else {
        return;
    };
    let column: Vec<u32> = (12..78).map(|y| luma(image.pixel(60, y))).collect();
    let (dark, light) = (column.iter().min().unwrap(), column.iter().max().unwrap());
    assert!(light - dark > 60, "rows {column:?}");
    // Every few rows, not one dark band.
    let darkened = column.iter().filter(|&&l| l < (dark + light) / 2).count();
    assert!(darkened > 10 && darkened < 50, "{darkened} dark rows of 66");
    // Outside the body the surface is untouched.
    assert_eq!(image.pixel(5, 40), [0, 0, 0, 255]);
}

#[test]
fn a_vignette_darkens_the_edges_more_than_the_centre() {
    let p = Post {
        vignette: 0.8,
        ..post()
    };
    let Some(image) = render(&scene(Some(p), vec![])) else {
        return;
    };
    let centre = luma(image.pixel(60, 45));
    let corner = luma(image.pixel(12, 12));
    assert!(centre > corner + 100, "centre {centre}, corner {corner}");
    assert!(
        centre >= 3 * 0x80 - 6,
        "the centre stays nearly as it was: {centre}"
    );
}

#[test]
fn grain_is_the_same_at_a_frozen_time_and_moves_with_time() {
    let p = Post {
        grain: 0.6,
        ..post()
    };
    let Some(a) = render(&scene(Some(p), vec![])) else {
        return;
    };
    let b = render(&scene(Some(p), vec![])).unwrap();
    assert!(a == b, "grain at one time differs between frames");
    let lumas: Vec<u32> = (20..60).map(|x| luma(a.pixel(x, 40))).collect();
    assert!(
        lumas.iter().max().unwrap() - lumas.iter().min().unwrap() > 20,
        "{lumas:?}"
    );
    let later = render(&scene(Some(Post { time: 1.5, ..p }), vec![])).unwrap();
    assert!(a != later, "grain did not move with time");
}

#[test]
fn flicker_trembles_the_brightness_over_time() {
    let at = |time: f32| {
        let p = Post {
            flicker: 0.5,
            time,
            ..post()
        };
        render(&scene(Some(p), vec![])).map(|i| luma(i.pixel(60, 45)))
    };
    let Some(first) = at(0.0) else { return };
    let lumas: Vec<u32> = [0.0, 0.01, 0.02, 0.033, 0.05]
        .into_iter()
        .map(|t| at(t).unwrap())
        .collect();
    assert_eq!(lumas[0], first);
    assert!(
        lumas.iter().max().unwrap() - lumas.iter().min().unwrap() > 15,
        "{lumas:?}"
    );
}

#[test]
fn bloom_spreads_light_from_bright_parts() {
    let dot = rect([58.0, 43.0, 4.0, 4.0], Rgba::hex(0xffffff));
    let dark = |post| scene(post, vec![rect(BODY, Rgba::hex(0x101010)), dot]);
    let Some(plain) = render(&dark(Some(post()))) else {
        return;
    };
    let p = Post {
        bloom: 0.8,
        ..post()
    };
    let bloomed = render(&dark(Some(p))).unwrap();
    let near = |i: &Image| luma(i.pixel(66, 45));
    assert!(
        near(&bloomed) > near(&plain) + 15,
        "{} vs {}",
        near(&bloomed),
        near(&plain)
    );
    let far = |i: &Image| luma(i.pixel(100, 70));
    assert!(
        far(&bloomed).abs_diff(far(&plain)) <= 3,
        "bloom reached the far corner"
    );
}

#[test]
fn curvature_bends_straight_lines_towards_the_edges() {
    // A horizontal white line across the body near its top.
    let line = rect([10.0, 20.0, 100.0, 2.0], Rgba::hex(0xffffff));
    let bright_rows = |i: &Image, x: u32| -> Vec<u32> {
        (10..80).filter(|&y| luma(i.pixel(x, y)) > 500).collect()
    };
    let p = Post {
        curvature: 1.0,
        ..post()
    };
    let Some(bent) = render(&scene(Some(p), vec![line])) else {
        return;
    };
    let (middle, edge) = (bright_rows(&bent, 60), bright_rows(&bent, 24));
    assert!(
        !middle.is_empty() && !edge.is_empty(),
        "{middle:?} {edge:?}"
    );
    assert!(
        edge[0] > middle[0],
        "the line does not bend: {middle:?} {edge:?}"
    );
}

#[test]
fn aberration_parts_red_from_blue_at_an_edge() {
    let bar = rect([40.0, 10.0, 40.0, 70.0], Rgba::hex(0xffffff));
    let dark = |post| scene(post, vec![rect(BODY, Rgba::hex(0)), bar]);
    let p = Post {
        aberration: 1.0,
        ..post()
    };
    let Some(image) = render(&dark(Some(p))) else {
        return;
    };
    let fringe: Vec<[u8; 4]> = (34..46).map(|x| image.pixel(x, 45)).collect();
    let split = fringe.iter().any(|px| px[0].abs_diff(px[2]) > 60);
    assert!(split, "no coloured fringe at the bar's edge: {fringe:?}");
}

#[test]
fn the_pass_scales_with_the_output() {
    let p = Post {
        scanlines: 0.5,
        vignette: 0.4,
        grain: 0.3,
        ..post()
    };
    for scale in [Scale::from_120ths(156), Scale::from_integer(2)] {
        let Some(image) = render_at(&scene(Some(p), vec![]), scale) else {
            return;
        };
        let (w, h) = scale.physical(LOGICAL);
        assert_eq!((image.width, image.height), (w, h));
        assert_eq!(image.pixel(2, h / 2), [0, 0, 0, 255], "outside the body");
    }
}
