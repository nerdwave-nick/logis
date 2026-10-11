//! The faces a theme's fonts ask for (spec §4.4, §4.9): families in order
//! of preference through fontconfig (a missing family falls through to the
//! next, generic families by alias), a weight per face, letter spacing, and
//! matched characters in another face, underlined. Fonts are pinned as in
//! tests/text.rs: none of the themes' own families (IBM Plex Mono, Cinzel,
//! VT323) is known, so they fall back to the pinned monospace and serif.

mod support;

use std::path::{Path, PathBuf};

use logis::render::{OFFSCREEN_FORMAT, Renderer, Rgba, Scale, Scene, Text, render_offscreen};
use logis::text::{Face, FontConfig, Fonts};
use support::golden::{self, Goldens, Tolerance};

fn font(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fonts")
        .join(name)
}

const RULES: &str = r#"<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <alias binding="same"><family>sans-serif</family><prefer><family>Noto Sans</family></prefer></alias>
  <alias binding="same"><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias binding="same"><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>
"#;

fn pinned() -> Fonts {
    let files: Vec<PathBuf> = [
        "NotoSans-Regular.ttf",
        "NotoSansMono-Regular.ttf",
        "NotoSansMono-SemiBold.ttf",
        "NotoSerif-SemiBold.ttf",
    ]
    .map(font)
    .to_vec();
    let files: Vec<&Path> = files.iter().map(PathBuf::as_path).collect();
    let config = FontConfig::isolated(RULES, &files).expect("the pinned fonts load");
    Fonts::new(config, "sans-serif").expect("sans-serif resolves")
}

fn face(families: &[&str], weight: u16) -> Face {
    Face {
        families: families.iter().map(|f| (*f).to_owned()).collect(),
        weight,
    }
}

/// The faces of forge-crimson-brass: rows, the prompt label, matches.
fn forge_faces() -> Vec<Face> {
    vec![
        face(&["IBM Plex Mono", "monospace"], 400),
        face(&["Cinzel", "serif"], 600),
        face(&["IBM Plex Mono", "monospace"], 600),
    ]
}

fn loaded(fonts: &Fonts) -> Vec<String> {
    fonts
        .loaded()
        .iter()
        .map(|f| f.path.file_name().unwrap().to_string_lossy().into_owned())
        .collect()
}

#[test]
fn missing_families_fall_through_to_the_generic_one() {
    let mut fonts = pinned();
    fonts.set_faces(&forge_faces()).unwrap();
    assert_eq!(fonts.face(0), ("Noto Sans Mono", 400));
    assert_eq!(fonts.face(1), ("Noto Serif", 600));
    assert_eq!(fonts.face(2), ("Noto Sans Mono", 600));
    let files = loaded(&fonts);
    for file in [
        "NotoSansMono-Regular.ttf",
        "NotoSerif-SemiBold.ttf",
        "NotoSansMono-SemiBold.ttf",
    ] {
        assert!(files.contains(&file.to_owned()), "{file} in {files:?}");
    }
}

#[test]
fn an_installed_first_choice_wins_and_unknown_faces_use_the_first() {
    let mut fonts = pinned();
    fonts
        .set_faces(&[face(&["Noto Serif", "monospace"], 600)])
        .unwrap();
    assert_eq!(fonts.face(0), ("Noto Serif", 600));
    assert_eq!(
        fonts.face(7),
        ("Noto Serif", 600),
        "out of range: the first"
    );
}

#[test]
fn without_faces_every_index_is_the_default_family() {
    let fonts = pinned();
    assert_eq!(fonts.face(0), ("Noto Sans", 400));
    assert_eq!(fonts.face(3), ("Noto Sans", 400));
}

fn scene() -> Scene {
    let ink = Rgba::hex(0xf1e6cc);
    let gold = Rgba::hex(0xf2c14e);
    let mut label = Text::new("INVOKE", [8.0, 6.0], 13.0, gold);
    label.face = 1;
    label.tracking = 0.2;
    let mut row = Text::new("do-not-disturb\ton", [8.0, 30.0], 16.0, ink);
    row.spans = vec![(1..2, gold), (8..9, gold)];
    row.span_face = Some(2);
    let mut underlined = row.clone();
    underlined.origin = [8.0, 56.0];
    underlined.underline_spans = true;
    Scene {
        clear: Some(Rgba::hex(0x170e0b)),
        texts: vec![label, row, underlined],
        ..Scene::default()
    }
}

#[test]
fn faces_tracking_and_underlined_matches_match_their_goldens() {
    let Some(gpu) = golden::env_gpu() else { return };
    let goldens = Goldens::checked_in();
    let failures: Vec<String> = [Scale::ONE, Scale::from_120ths(156), Scale::from_integer(2)]
        .into_iter()
        .filter_map(|scale| {
            let mut renderer = Renderer::new(&gpu);
            renderer.set_fonts(&gpu, pinned());
            renderer.set_faces(&forge_faces()).unwrap();
            let image = render_offscreen(
                &gpu,
                &mut renderer,
                &scene(),
                (220, 84),
                scale,
                OFFSCREEN_FORMAT,
            )
            .unwrap();
            let name = format!("faces@{}", golden::scale_suffix(scale));
            goldens
                .check("text", &name, &image, Tolerance::OFFSCREEN)
                .err()
        })
        .collect();
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

#[test]
fn an_underline_runs_under_the_matched_characters_only() {
    let Some(gpu) = golden::env_gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    renderer.set_fonts(&gpu, pinned());
    renderer.set_faces(&forge_faces()).unwrap();
    let mut plain = scene();
    plain.texts.truncate(2);
    plain.texts[1].origin = [8.0, 56.0];
    let render = |r: &mut Renderer, s: &Scene| {
        render_offscreen(&gpu, r, s, (220, 84), Scale::ONE, OFFSCREEN_FORMAT).unwrap()
    };
    let without = render(&mut renderer, &plain);
    let with = render(&mut renderer, &scene());
    // The underline lies in the lower part of the third line's box.
    let differs = |x: u32, y: u32| without.pixel(x, y) != with.pixel(x, y);
    let rows: Vec<u32> = (56..84)
        .filter(|&y| (0..220).any(|x| differs(x, y)))
        .collect();
    assert!(!rows.is_empty(), "no underline was drawn");
    assert!(rows.iter().all(|&y| y >= 66), "underline rows {rows:?}");
    let columns: Vec<u32> = (0..220)
        .filter(|&x| rows.iter().any(|&y| differs(x, y)))
        .collect();
    let (left, right) = (columns[0], *columns.last().unwrap());
    // Two matched characters of a 16 px monospace line: "o" and "s".
    assert!(right - left < 100, "the underline spans {left}..{right}");
    assert!(columns.len() < 30, "{} underlined columns", columns.len());
}

#[test]
fn changing_the_faces_reshapes_the_text() {
    let Some(gpu) = golden::env_gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    renderer.set_fonts(&gpu, pinned());
    renderer.set_faces(&forge_faces()).unwrap();
    let render = |r: &mut Renderer| {
        render_offscreen(&gpu, r, &scene(), (220, 84), Scale::ONE, OFFSCREEN_FORMAT).unwrap()
    };
    let forge = render(&mut renderer);
    renderer
        .set_faces(&[face(&["serif"], 600), face(&["monospace"], 400)])
        .unwrap();
    assert!(render(&mut renderer) != forge, "the old shapes were reused");
}
