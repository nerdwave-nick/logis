//! The dmenu list as drawn (spec §3 stories 1–3): the prompt line with the
//! query, cursor and counter, tab-separated columns lined up at tab stops,
//! the selection band and the matched characters in brass, compared with
//! goldens at scales 1.0, 1.3 and 2.0. A provider's stage adds glyphs,
//! badges, a pending row and a toast (spec §4.8). The list comes from the real
//! [`ListView`] (filtering and highlights included); fonts are pinned as in
//! tests/text.rs. Needs a GPU adapter (see tests/render.rs).

mod support;

use std::path::Path;

use logis::input::{Action, Edit, Step};
use logis::list::{Decor, Entry, Level, ListView, Notice, Options, scene};
use logis::render::{Image, OFFSCREEN_FORMAT, Renderer, Scale, Scene, render_offscreen};
use logis::text::{FontConfig, Fonts};
use support::golden::{self, Goldens, Tolerance};

fn pinned_fonts() -> Fonts {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fonts");
    let files: Vec<_> = [
        "NotoSans-Regular.ttf",
        "NotoSansHebrew-Regular.ttf",
        "NotoSansArabic-Regular.ttf",
        "NotoSansCJKjp-Regular.otf",
        "NotoColorEmoji.ttf",
    ]
    .map(|f| dir.join(f))
    .to_vec();
    let files: Vec<&Path> = files.iter().map(|p| p.as_path()).collect();
    let rules = "<fontconfig><alias><family>sans-serif</family>\
                 <prefer><family>Noto Sans</family></prefer></alias></fontconfig>";
    Fonts::new(FontConfig::isolated(rules, &files).unwrap(), "sans-serif").unwrap()
}

fn scales() -> [Scale; 3] {
    [Scale::ONE, Scale::from_120ths(156), Scale::from_integer(2)]
}

fn render(gpu: &logis::render::Gpu, scene: &Scene, logical: (u32, u32), scale: Scale) -> Image {
    let mut renderer = Renderer::new(gpu);
    renderer.set_fonts(gpu, pinned_fonts());
    render_offscreen(gpu, &mut renderer, scene, logical, scale, OFFSCREEN_FORMAT).unwrap()
}

#[track_caller]
fn check(name: &str, view: &mut ListView, options: &Options) {
    check_with(name, view, options, None);
}

#[track_caller]
fn check_with(name: &str, view: &mut ListView, options: &Options, toast: Option<Notice>) {
    let Some(gpu) = golden::env_gpu() else { return };
    let logical = scene::size(options);
    let mut look = view.look();
    look.toast = toast;
    let scene = scene::scene(&look, logical);
    let goldens = Goldens::checked_in();
    let failures: Vec<String> = scales()
        .into_iter()
        .filter_map(|scale| {
            let image = render(&gpu, &scene, logical, scale);
            let name = format!("{name}@{}", golden::scale_suffix(scale));
            goldens
                .check("list", &name, &image, Tolerance::OFFSCREEN)
                .err()
        })
        .collect();
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

fn items(texts: &[&str]) -> Vec<(String, Option<String>)> {
    texts.iter().map(|t| (t.to_string(), None)).collect()
}

#[test]
fn a_filtered_list_with_columns_highlights_and_a_counter() {
    let options = Options {
        lines: 5,
        width: 40,
        counter: true,
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push(items(&[
        "rite of oil\tunguent",
        "rite of ignition\tspark",
        "café noir\tdrink",
        "機械の精霊\tspirit",
        "Ἐν ἀρχῇ ἦν ὁ λόγος",
        "the rite of the long litany that never ends and is cut short\tlong",
    ]));
    view.apply(&Action::Edit(Edit::Insert("rite".into())));
    view.apply(&Action::Select(Step::Next));
    check("filtered", &mut view, &options);
}

#[test]
fn a_prompt_only_password_with_its_mask() {
    let options = Options {
        prompt: "pin: ".into(),
        lines: 0,
        password: Some('*'),
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.apply(&Action::Edit(Edit::Insert("1234".into())));
    check("password", &mut view, &options);
}

#[test]
fn an_empty_query_shows_the_placeholder_and_every_item() {
    let options = Options {
        lines: 3,
        placeholder: "seek the rite".into(),
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push(items(&["alpha", "beta", "gamma", "delta"]));
    check("placeholder", &mut view, &options);
}

fn entry(text: &str, glyph: Option<&str>, badges: &[&str], pending: bool) -> Entry {
    Entry {
        text: text.into(),
        match_text: None,
        decor: Some(Box::new(Decor {
            glyph: glyph.map(str::to_owned),
            icon: None,
            badges: badges.iter().map(|b| b.to_string()).collect(),
            pending,
            boost: 0,
        })),
    }
}

#[test]
fn a_provider_stage_with_glyphs_badges_a_pending_row_and_a_toast() {
    let options = Options {
        prompt: "rite: ".into(),
        lines: 5,
        width: 44,
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push([
        entry("rite of oil\tunguent", Some("⚙"), &["current"], false),
        entry("rite of ignition\tspark", Some("†"), &[], true),
        entry("litany of the forge", None, &["edict", "heresy"], false),
        entry("機械の精霊", Some("§"), &[], false),
    ]);
    view.apply(&Action::Select(Step::Next));
    let toast = Notice {
        text: "the rite of oil is begun".into(),
        level: Level::Success,
    };
    check_with("provider", &mut view, &options, Some(toast));
}

fn with_icon(text: &str, icon: &str, glyph: Option<&str>) -> Entry {
    Entry {
        text: text.into(),
        match_text: None,
        decor: Some(Box::new(Decor {
            glyph: glyph.map(str::to_owned),
            icon: Some(icon.into()),
            ..Decor::default()
        })),
    }
}

/// The app launcher's list: icons from the fixture icon theme
/// (tests/fixtures/icons) in the icon column, a glyph where an icon is
/// missing, nothing where neither is.
#[test]
fn an_app_list_draws_its_icons() {
    use logis::icons::{FrameIcons, IconCache, IconThemes, SearchPaths};
    let Some(gpu) = golden::env_gpu() else { return };
    let icons = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/icons");
    let paths = SearchPaths {
        icon_dirs: vec![icons.join("home-icons"), icons.join("share/icons")],
        pixmap_dirs: vec![icons.join("pixmaps")],
    };
    let options = Options {
        lines: 5,
        width: 40,
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push([
        with_icon("Anoint", "forge-cog", None),
        with_icon("Banner of the forge", "forge-banner", None),
        with_icon("Cogitator skull", "cogitator-skull", None),
        with_icon("Missing, with a glyph", "forge-absent", Some("⚙")),
        with_icon("Missing, without", "forge-absent", None),
    ]);
    view.apply(&Action::Select(Step::Next));
    let logical = scene::size(&options);
    let look = view.look();
    let goldens = Goldens::checked_in();
    let mut failures = Vec::new();
    for scale in scales() {
        let mut renderer = Renderer::new(&gpu);
        renderer.set_fonts(&gpu, pinned_fonts());
        let mut cache = IconCache::new(IconThemes::load(&paths, "logis-forge"));
        let mut source = FrameIcons {
            cache: &mut cache,
            gpu: &gpu,
            atlas: renderer.atlas_mut(),
            scale,
        };
        let scene = scene::scene_with(&look, logical, &mut source);
        assert_eq!(scene.images.len(), 3, "three icons are found");
        assert!(
            scene.texts.iter().any(|t| t.text == "⚙"),
            "the glyph stands in"
        );
        let image = render_offscreen(
            &gpu,
            &mut renderer,
            &scene,
            logical,
            scale,
            OFFSCREEN_FORMAT,
        )
        .unwrap();
        let name = format!("apps@{}", golden::scale_suffix(scale));
        failures.extend(
            goldens
                .check("list", &name, &image, Tolerance::OFFSCREEN)
                .err(),
        );
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}
