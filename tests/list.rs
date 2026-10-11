//! The list as drawn in each bundled theme (spec §4.9, §5): the prompt line
//! with the query, caret and counter, tab-separated columns in the theme's
//! column colours, the selection band and the matched characters set
//! apart; a provider's stage with glyphs, badges, a pending row and a
//! toast; the app launcher's icons; and the theme's own window (820×520,
//! nine rows) with its effects. Compared with goldens per theme at scales
//! 1.0, 1.3 and 2.0, in `tests/goldens/themes/<theme>/`.
//!
//! Deterministic: the frame's time is frozen (the glow at full strength,
//! the highlight at the top, grain and flicker at time 0), and fonts are
//! pinned as in tests/text.rs, the themes' own families (IBM Plex Mono,
//! Cinzel, VT323) falling back to the pinned monospace and serif. The list
//! comes from the real [`ListView`]. Needs a GPU adapter (see
//! tests/render.rs).

mod support;

use std::path::Path;

use logis::icons::{FrameIcons, IconCache, IconThemes, NoIcons, SearchPaths};
use logis::input::{Action, Edit, Step};
use logis::list::scene::{self, Style};
use logis::list::{Decor, Entry, Level, ListView, Look, Notice, Options};
use logis::render::{Gpu, OFFSCREEN_FORMAT, Renderer, Scale, Scene, render_offscreen};
use logis::text::{FontConfig, Fonts};
use logis::theme::{BUNDLED, Theme, Themes};
use support::golden::{self, Goldens, Tolerance};

const RULES: &str = r#"<fontconfig>
  <alias><family>sans-serif</family><prefer><family>Noto Sans</family></prefer></alias>
  <alias><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>"#;

fn pinned_fonts() -> Fonts {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fonts");
    let files: Vec<_> = [
        "NotoSans-Regular.ttf",
        "NotoSansMono-Regular.ttf",
        "NotoSansMono-SemiBold.ttf",
        "NotoSerif-SemiBold.ttf",
        "NotoSansHebrew-Regular.ttf",
        "NotoSansArabic-Regular.ttf",
        "NotoSansCJKjp-Regular.otf",
        "NotoColorEmoji.ttf",
    ]
    .map(|f| dir.join(f))
    .to_vec();
    let files: Vec<&Path> = files.iter().map(|p| p.as_path()).collect();
    Fonts::new(FontConfig::isolated(RULES, &files).unwrap(), "sans-serif").unwrap()
}

fn scales() -> [Scale; 3] {
    [Scale::ONE, Scale::from_120ths(156), Scale::from_integer(2)]
}

fn themes() -> Vec<Theme> {
    BUNDLED
        .iter()
        .map(|(name, _)| Themes::default().load(name).unwrap())
        .collect()
}

fn renderer(gpu: &Gpu, theme: &Theme) -> Renderer {
    let mut renderer = Renderer::new(gpu);
    renderer.set_fonts(gpu, pinned_fonts());
    renderer.set_faces(&theme.faces).unwrap();
    renderer
}

/// Builds the scene of a list for a renderer at a scale (icons, if any,
/// go into that renderer's atlas).
type MakeScene<'a> = &'a dyn Fn(&mut Renderer, &Gpu, Scale, &Look, (u32, u32), Style<'_>) -> Scene;

/// Renders the list of `view` (sized by `options`) in every bundled theme
/// at every scale, its scene from `make`, and compares each with
/// `themes/<theme>/<name>@<scale>`.
#[track_caller]
fn check_with(
    name: &str,
    view: &mut ListView,
    options: &Options,
    toast: Option<Notice>,
    make: MakeScene<'_>,
) {
    let Some(gpu) = golden::env_gpu() else { return };
    let mut look = view.look();
    look.toast = toast;
    let goldens = Goldens::checked_in();
    let mut failures = Vec::new();
    for theme in themes() {
        let (logical, rows) = scene::fit(options, &theme);
        look.lines = rows;
        let style = Style {
            theme: &theme,
            at: 0,
        };
        for scale in scales() {
            let mut renderer = renderer(&gpu, &theme);
            let scene = make(&mut renderer, &gpu, scale, &look, logical, style);
            let image = render_offscreen(
                &gpu,
                &mut renderer,
                &scene,
                logical,
                scale,
                OFFSCREEN_FORMAT,
            )
            .unwrap();
            let set = format!("themes/{}", theme.name);
            let file = format!("{name}@{}", golden::scale_suffix(scale));
            failures.extend(
                goldens
                    .check(&set, &file, &image, Tolerance::OFFSCREEN)
                    .err(),
            );
        }
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

#[track_caller]
fn check(name: &str, view: &mut ListView, options: &Options, toast: Option<Notice>) {
    check_with(
        name,
        view,
        options,
        toast,
        &|_, _, _, look, logical, style| scene::scene_with(look, logical, style, &mut NoIcons),
    );
}

fn items(texts: &[&str]) -> Vec<(String, Option<String>)> {
    texts.iter().map(|t| (t.to_string(), None)).collect()
}

#[test]
fn a_filtered_list_with_columns_highlights_and_a_counter() {
    let options = Options {
        lines: Some(5),
        width: Some(50),
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
    check("filtered", &mut view, &options, None);
}

#[test]
fn a_prompt_only_password_with_its_mask() {
    let options = Options {
        prompt: "pin: ".into(),
        lines: Some(0),
        width: Some(40),
        password: Some('*'),
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.apply(&Action::Edit(Edit::Insert("1234".into())));
    check("password", &mut view, &options, None);
}

#[test]
fn an_empty_query_shows_the_placeholder_and_every_item() {
    let options = Options {
        lines: Some(3),
        width: Some(40),
        placeholder: "seek the rite".into(),
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push(items(&["alpha", "beta", "gamma", "delta"]));
    check("placeholder", &mut view, &options, None);
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
            ..Decor::default()
        })),
    }
}

#[test]
fn a_provider_stage_with_glyphs_badges_a_pending_row_and_a_toast() {
    let options = Options {
        prompt: "rite: ".into(),
        lines: Some(5),
        width: Some(52),
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
    check("provider", &mut view, &options, Some(toast));
}

/// The theme's own window: no size asked, as servitor mode and the app
/// launcher open; the rows of the design canvas.
#[test]
fn the_themes_window_with_the_rites_of_the_canvas() {
    let options = Options {
        counter: true,
        ..Options::default()
    };
    let mut view = ListView::new(options.clone());
    view.push([
        entry(
            "do-not-disturb\ton\tSilence the vox of the desktop",
            None,
            &[],
            false,
        ),
        entry(
            "keyboard-layout\tus\tThe tongue of the keys",
            None,
            &[],
            false,
        ),
        entry(
            "mouse-autohide-toggle\toff\tVeil the cursor when idle",
            None,
            &[],
            false,
        ),
        entry(
            "power-profile\t·\tThe hunger of the machine spirit",
            None,
            &[],
            true,
        ),
        entry("vox\tauto\tWho hears the machine", None, &["edict"], false),
        entry(
            "night-light\toff\tDim the light of the screens",
            None,
            &[],
            false,
        ),
        entry("theme\tforge\tThe visage of the machine", None, &[], false),
    ]);
    view.apply(&Action::Edit(Edit::Insert("o".into())));
    check("window", &mut view, &options, None);
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
    let icons = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/icons");
    let paths = SearchPaths {
        icon_dirs: vec![icons.join("home-icons"), icons.join("share/icons")],
        pixmap_dirs: vec![icons.join("pixmaps")],
    };
    let options = Options {
        lines: Some(5),
        width: Some(40),
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
    check_with(
        "apps",
        &mut view,
        &options,
        None,
        &|renderer, gpu, scale, look, logical, style| {
            let mut cache = IconCache::new(IconThemes::load(&paths, "logis-forge"));
            let mut source = FrameIcons {
                cache: &mut cache,
                gpu,
                atlas: renderer.atlas_mut(),
                scale,
            };
            let scene = scene::scene_with(look, logical, style, &mut source);
            assert_eq!(scene.images.len(), 3, "three icons are found");
            assert!(
                scene.texts.iter().any(|t| t.text == "⚙"),
                "the glyph stands in"
            );
            scene
        },
    );
}
