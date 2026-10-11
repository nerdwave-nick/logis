//! The themed list scene: geometry from the theme or the options, the
//! prompt line, rows and band, marks, and the continuous effects at a
//! moment.

#![allow(clippy::single_range_in_vec_init)] // one highlighted range is a case

use super::*;
use crate::list::view::Row;
use crate::list::{Level, Notice};
use crate::render::{CaretShape, Paint};
use crate::theme::Themes;

fn forge() -> Theme {
    Theme::bundled_default()
}

fn phosphor() -> Theme {
    Themes::default().load("cogitator-phosphor").unwrap()
}

fn style(theme: &Theme) -> Style<'_> {
    Style { theme, at: 0 }
}

fn look(rows: usize, selected: usize) -> Look {
    Look {
        prompt: "> ".into(),
        input: "rite".into(),
        cursor: Some(2),
        rows: (0..rows)
            .map(|i| Row {
                text: format!("row {i}\tcol"),
                highlights: vec![0..1],
                selected: i == selected,
                ..Row::default()
            })
            .collect(),
        lines: 5,
        counter: Some((rows, 9)),
        ..Look::default()
    }
}

fn options(lines: Option<u32>, width: Option<u32>) -> Options {
    Options {
        lines,
        width,
        ..Options::default()
    }
}

fn sized(theme: &Theme) -> (u32, u32) {
    fit(&options(Some(5), Some(40)), theme).0
}

#[test]
fn without_a_size_the_window_is_the_themes() {
    let theme = forge();
    let ((w, h), rows) = fit(&Options::default(), &theme);
    let m = 2.0 * theme.window.margin;
    assert_eq!((w, h), ((820.0 + m) as u32, (520.0 + m) as u32));
    assert_eq!(rows, 9);
    let (body, radius) = body((w, h), &theme);
    assert_eq!((body[2], body[3], radius), (820.0, 520.0, 14.0));
    assert_eq!(grid((w, h), rows, &theme).rows, 9, "nine rows fit");
}

#[test]
fn lines_and_width_in_the_options_size_the_window() {
    let theme = forge();
    let ((w30, h15), rows) = fit(&options(Some(15), Some(30)), &theme);
    let ((w60, h5), _) = fit(&options(Some(5), Some(60)), &theme);
    assert_eq!(rows, 15);
    assert_eq!(w60 - w30, (30.0 * advance(&theme.row.font)).round() as u32);
    assert_eq!(h15 - h5, (10.0 * theme.row.height) as u32);
    let ((_, h0), _) = fit(&options(Some(0), None), &theme);
    let m = 2.0 * theme.window.margin;
    assert_eq!(h0, (theme.prompt.height + m) as u32, "only the prompt line");
}

#[test]
fn the_prompt_line_shows_sigil_label_query_caret_and_counter() {
    let theme = forge();
    let s = scene(&look(3, 0), sized(&theme), style(&theme));
    let label = s
        .texts
        .iter()
        .find(|t| t.text == "INVOKE")
        .expect("the label");
    assert_eq!(label.face, theme.prompt.font.face);
    assert_eq!(label.tracking, 0.2);
    let query = s
        .texts
        .iter()
        .find(|t| t.text == "rite")
        .expect("the query alone");
    assert!(query.origin[0] > label.origin[0], "after the label");
    let caret = query.caret.expect("a caret");
    assert_eq!((caret.at, caret.shape), (2, CaretShape::Bar));
    assert_eq!(caret.color, Rgba::hex(0xf2c14e));
    let counter = s
        .texts
        .iter()
        .find(|t| t.text == "3/9")
        .expect("the counter");
    assert!(counter.align_end);
    assert!(
        query.width.unwrap() < counter.width.unwrap(),
        "the query leaves room"
    );
    // The gear: a ring with eight teeth and a hub, before the label.
    let gear: Vec<_> = s
        .shapes
        .iter()
        .filter(|r| r.rect[0] + r.rect[2] < label.origin[0] && r.rect[2] < 20.0)
        .collect();
    assert!(gear.len() >= 10, "{} gear shapes", gear.len());
}

#[test]
fn a_session_prompt_shows_and_the_default_one_gives_way_to_the_label() {
    let theme = forge();
    let mut l = look(1, 0);
    l.prompt = "pin: ".into();
    let s = scene(&l, sized(&theme), style(&theme));
    let query = s.texts.iter().find(|t| t.text == "pin: rite").unwrap();
    assert_eq!(query.spans[0], (0..5, theme.prompt.color));
    assert_eq!(query.caret.unwrap().at, 7, "after the prompt and 'ri'");
    // Phosphor's label is "SUMMON>"; the default prompt yields to it too.
    let p = phosphor();
    let s = scene(&look(1, 0), sized(&p), style(&p));
    assert!(s.texts.iter().any(|t| t.text == "SUMMON>"));
    assert!(s.texts.iter().any(|t| t.text == "rite"));
}

#[test]
fn a_placeholder_has_no_caret_and_the_placeholder_colour() {
    let theme = forge();
    let mut l = look(0, 0);
    l.cursor = None;
    l.input = "seek".into();
    let s = scene(&l, sized(&theme), style(&theme));
    let query = s.texts.iter().find(|t| t.text == "seek").unwrap();
    assert_eq!(query.caret, None);
    assert_eq!(query.spans, [(0..4, theme.query.placeholder)]);
}

fn row<'a>(s: &'a Scene, text: &str) -> &'a crate::render::Text {
    s.texts.iter().find(|t| t.text == text).unwrap()
}

#[test]
fn rows_take_column_colours_and_set_matches_apart() {
    let theme = forge();
    let logical = sized(&theme);
    let s = scene(&look(3, 1), logical, style(&theme));
    let plain = row(&s, "row 0\tcol");
    let [parchment, brass, _] = theme.row.columns[..] else {
        panic!("three columns")
    };
    assert_eq!(
        plain.spans,
        [
            (0..1, theme.matched.color),
            (1..6, parchment),
            (6..9, brass)
        ]
    );
    assert_eq!(plain.emphasis, [0..1]);
    assert_eq!(plain.emphasis_face, Some(theme.matched.face));
    assert!(!plain.underline_emphasis, "forge does not underline");
    let selected = row(&s, "row 1\tcol");
    assert_eq!(selected.spans[1].1, theme.selection.columns[0]);
    // Rows below the rule, one pitch apart, the band on the selected one.
    let g = grid(logical, 5, &theme);
    assert!((selected.origin[1] - plain.origin[1] - g.pitch).abs() < 1e-3);
    let band = band_top(&look(3, 1), logical, &theme).unwrap();
    assert_eq!(band, g.top(1));
    let rule = theme.prompt.height + theme.window.margin;
    assert!(plain.origin[1] > rule);
}

#[test]
fn phosphor_inverts_the_selection_and_underlines_matches() {
    let theme = phosphor();
    let s = scene(&look(3, 1), sized(&theme), style(&theme));
    let selected = row(&s, "row 1\tcol");
    assert_eq!(selected.color, theme.selection.columns[0]);
    assert!(selected.underline_emphasis);
    // The band is solid phosphor with a glow and no edge bar.
    let band = s
        .shapes
        .iter()
        .find(|r| r.fill == Paint::Solid(theme.selection.band[0]))
        .expect("the band");
    assert!(band.glow.is_some());
    // A dashed rule: many short bars.
    let rule_y = theme.window.margin + theme.prompt.height;
    let dashes = s.shapes.iter().filter(|r| r.rect[1] == rule_y).count();
    assert!(dashes > 20, "{dashes} dashes");
}

fn decorated() -> Look {
    let mut l = look(3, 0);
    l.glyphs = true;
    l.rows[0].glyph = Some("⚙".into());
    l.rows[0].badges = vec!["current".into(), "edict".into()];
    l.rows[1].pending = true;
    l
}

#[test]
fn badges_end_the_row_and_a_pending_row_is_dimmed_and_marked() {
    let theme = forge();
    let s = scene(&decorated(), sized(&theme), style(&theme));
    let first = row(&s, "row 0\tcol");
    let current = row(&s, "CURRENT");
    let edict = row(&s, "EDICT");
    assert!(first.origin[0] + first.width.unwrap() <= current.origin[0]);
    assert!(current.origin[0] < edict.origin[0], "in the given order");
    let pending = row(&s, "row 1\tcol");
    assert_eq!(
        pending.color.a,
        theme.pending.color.a * theme.pending.opacity
    );
    assert!(pending.emphasis.is_empty(), "no matches while pending");
    assert!(s.texts.iter().any(|t| t.text == "DIVINING…"));
    let glyph = row(&s, "⚙");
    assert!(glyph.origin[0] < first.origin[0]);
    // Phosphor brackets its badges instead of drawing pills.
    let p = phosphor();
    let s = scene(&decorated(), sized(&p), style(&p));
    assert!(s.texts.iter().any(|t| t.text == "[EDICT]"));
    assert!(s.texts.iter().any(|t| t.text == "[▒▒▒ AUGURING]"));
}

#[test]
fn a_toast_lies_over_the_bottom_of_the_body_in_its_levels_colour() {
    let theme = forge();
    let logical = sized(&theme);
    let mut l = look(3, 0);
    l.toast = Some(Notice {
        text: "heresy in the theme".into(),
        level: Level::Error,
    });
    let s = scene(&l, logical, style(&theme));
    let words = row(&s, "heresy in the theme");
    let (body, _) = body(logical, &theme);
    assert!(words.origin[1] + words.line_height <= body[1] + body[3]);
    let strip = s.shapes.last().unwrap();
    assert_eq!(strip.border.unwrap().paint, Paint::Solid(theme.error));
}

#[test]
fn a_gliding_band_moves_alone() {
    let theme = forge();
    let logical = sized(&theme);
    let l = look(3, 2);
    let rest = scene(&l, logical, style(&theme));
    let top = band_top(&l, logical, &theme).unwrap();
    let gliding = scene_at(&l, logical, Some(top - 10.0), style(&theme), &mut NoIcons);
    assert_eq!(gliding.texts, rest.texts);
    let moved: Vec<_> = gliding
        .shapes
        .iter()
        .zip(&rest.shapes)
        .filter(|(g, r)| g != r)
        .collect();
    assert_eq!(moved.len(), 2, "the band and its edge bar");
    for (g, r) in moved {
        assert_eq!(g.rect[1], r.rect[1] - 10.0);
    }
    assert_eq!(
        scene_at(&l, logical, Some(top), style(&theme), &mut NoIcons),
        rest,
        "at rest"
    );
}

#[test]
fn the_body_is_the_first_shape_and_the_effects_cover_it() {
    let theme = forge();
    let logical = sized(&theme);
    let (rect, radius) = body(logical, &theme);
    let s = scene(&look(3, 0), logical, style(&theme));
    assert_eq!((s.shapes[0].rect, s.shapes[0].radius), (rect, radius));
    let post = s.post.expect("forge's effects");
    assert_eq!((post.rect, post.radius), (rect, radius));
    assert_eq!(post.scanlines, theme.effects.scanlines);
}

#[test]
fn the_grid_finds_the_row_under_a_point() {
    let theme = forge();
    let logical = sized(&theme);
    let g = grid(logical, 5, &theme);
    for i in 0..5 {
        let y = f64::from(g.top(i) + g.pitch / 2.0);
        assert_eq!(g.row_at((100.0, y)), Some(i));
    }
    let below = f64::from(g.top(5) + 1.0);
    assert_eq!(g.row_at((100.0, below)), None, "past the last row");
    assert_eq!(g.row_at((100.0, 10.0)), None, "the prompt line");
    assert_eq!(
        g.row_at((2.0, f64::from(g.first) + 2.0)),
        None,
        "the margin"
    );
    assert_eq!(grid(logical, 2, &theme).rows, 2, "two lines asked for");
}

#[test]
fn no_lines_means_no_rule_and_no_rows() {
    let theme = forge();
    let mut l = look(0, 0);
    l.lines = 0;
    let logical = fit(&options(Some(0), None), &theme).0;
    let s = scene(&l, logical, style(&theme));
    let rule_y = theme.window.margin + theme.prompt.height;
    assert!(!s.shapes.iter().any(|r| r.rect[1] == rule_y), "no rule");
}

#[test]
fn the_glow_pulses_and_the_highlight_travels_until_motion_is_reduced() {
    let theme = forge();
    let logical = sized(&theme);
    let at = |theme: &Theme, ms: u64| {
        let s = scene(
            &look(1, 0),
            logical,
            Style {
                theme,
                at: ms * 1_000_000,
            },
        );
        let body = s.shapes[0];
        (
            body.glow.unwrap().color.a,
            body.border.unwrap().highlight.unwrap().angle,
        )
    };
    let (glow0, angle0) = at(&theme, 0);
    let (glow1, angle1) = at(&theme, 1500);
    assert!(glow1 < glow0, "half a pulse later the glow is dimmer");
    assert!(
        (angle0 + std::f32::consts::FRAC_PI_2).abs() < 1e-6,
        "it starts at the top"
    );
    assert!(angle1 > angle0, "the highlight moved on");
    assert!(moving(&look(1, 0), &theme));
    let still = theme.stilled();
    assert_eq!(at(&still, 0), at(&still, 1500), "reduce-motion stills them");
    assert!(!moving(&look(1, 0), &still));
}

#[test]
fn a_pending_row_shimmers_and_keeps_a_still_theme_moving() {
    let theme = forge().stilled();
    let mut shimmering = theme.clone();
    shimmering.pending.shimmer = 0.3;
    let mut l = look(2, 0);
    assert!(!moving(&l, &shimmering), "no pending row in view");
    l.rows[1].pending = true;
    assert!(moving(&l, &shimmering));
    assert!(!moving(&l, &theme), "stilled: no shimmer");
    let mark = |ms: u64| {
        let s = scene(
            &l,
            sized(&shimmering),
            Style {
                theme: &shimmering,
                at: ms * 1_000_000,
            },
        );
        row(&s, "DIVINING…").color.a
    };
    assert!(mark(800) < mark(0), "the mark's opacity swings");
}
