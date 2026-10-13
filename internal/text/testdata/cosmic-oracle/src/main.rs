//! Differential oracle for logis's Go line layout (internal/text).
//!
//! Sets each case the way Rust logis v1's `render::text::shape` does and
//! prints, as JSON, every glyph cosmic-text lays out: font (by load order),
//! glyph id, pen x and y, whole-pixel advance, offsets in ems, byte range
//! and bidi level, with each line's direction, width and baseline.
//!
//! The fonts are tests/fonts, loaded in a fixed order into a FontSystem
//! built as logis's `Fonts::new` builds it (en-US, fallback lists empty).
//! Standalone, offline: `cargo run --release --offline > ../cosmic.json`.

use std::fmt::Write as _;
use std::path::PathBuf;

use cosmic_text::{
    fontdb, Align, Attrs, Buffer, Color, Ellipsize, EllipsizeHeightLimit, Fallback, Family,
    FontSystem, Hinting, Metrics, Shaping, Weight, Wrap,
};
use unicode_script::Script;

struct ResolvedOnly;

impl Fallback for ResolvedOnly {
    fn common_fallback(&self) -> &[&'static str] {
        &[]
    }
    fn forbidden_fallback(&self) -> &[&'static str] {
        &[]
    }
    fn script_fallback(&self, _: Script, _: &str) -> &[&'static str] {
        &[]
    }
}

/// A font set: files in load order and the faces texts name by index.
struct Set {
    name: &'static str,
    files: &'static [&'static str],
    faces: &'static [(&'static str, u16)],
}

const SETS: [Set; 2] = [
    Set {
        name: "default",
        files: &[
            "NotoSans-Regular.ttf",
            "NotoSansCJKjp-Regular.otf",
            "NotoColorEmoji.ttf",
            "NotoSansHebrew-Regular.ttf",
            "NotoSansArabic-Regular.ttf",
        ],
        faces: &[],
    },
    Set {
        name: "forge",
        files: &[
            "NotoSans-Regular.ttf",
            "NotoSansMono-Regular.ttf",
            "NotoSerif-SemiBold.ttf",
            "NotoSansMono-SemiBold.ttf",
            "NotoSansCJKjp-Regular.otf",
            "NotoColorEmoji.ttf",
            "NotoSansHebrew-Regular.ttf",
            "NotoSansArabic-Regular.ttf",
        ],
        faces: &[("Noto Sans Mono", 400), ("Noto Serif", 600), ("Noto Sans Mono", 600)],
    },
];

#[derive(Clone, Copy, PartialEq)]
enum Overflow {
    Clip,
    Ellipsis,
}

/// One line, as logis's render::Text describes it (logical pixels).
#[derive(Clone)]
struct Case {
    set: usize,
    text: &'static str,
    origin_x: f32,
    size: f32,
    width: Option<f32>,
    overflow: Overflow,
    align_end: bool,
    face: usize,
    tracking: f32,
    spans: Vec<(usize, usize)>,
    emphasis: Vec<(usize, usize)>,
    emphasis_face: Option<usize>,
}

fn case(set: usize, text: &'static str) -> Case {
    Case {
        set,
        text,
        origin_x: 8.0,
        size: 16.0,
        width: None,
        overflow: Overflow::Ellipsis,
        align_end: false,
        face: 0,
        tracking: 0.0,
        spans: Vec::new(),
        emphasis: Vec::new(),
        emphasis_face: None,
    }
}

fn cases() -> Vec<Case> {
    let corpus: Vec<&'static str> = include_str!("../../../../../tests/fonts/corpus.txt")
        .lines()
        .collect();
    let mut out = Vec::new();
    let plain = [
        "Hail",
        "AV",
        "office fit",
        "a機🔥",
        "abc שלום def",
        "שלום abc",
        "שלום, עולם!",
        "123 שלום",
        "(42)",
        "שלום (42) ok",
        "مرحبا بالعالم (42) ok",
        "a\tb",
        "\tx",
        "abc\t\tdef",
        "do-not-disturb\ton",
        "a\u{200D}b\u{FE0F}",
        "\u{10FFFD}",
        "x -> y != z",
        "👍🏽 🧑‍💻 🔥🔥",
        "Ça déjà vu",
        "",
        " ",
        "  lead and trail  ",
        "a\u{2029}b",
        "e\u{0301}x",
    ];
    for text in corpus.iter().copied().chain(plain) {
        out.push(case(0, text));
    }
    let long = corpus[0];
    for (width, overflow) in [
        (150.0, Overflow::Ellipsis),
        (150.0, Overflow::Clip),
        (60.0, Overflow::Ellipsis),
        (8.0, Overflow::Ellipsis),
        (0.0, Overflow::Ellipsis),
        (400.0, Overflow::Ellipsis),
    ] {
        for text in [long, "MMMMMMMMMMMMMMMMMMMM", "Ave", corpus[3], "שלום עולם ומלואו", "12/345"] {
            out.push(Case { width: Some(width), overflow, ..case(0, text) });
        }
    }
    for text in ["12/345", "שלום", "abc שלום", long] {
        for overflow in [Overflow::Clip, Overflow::Ellipsis] {
            out.push(Case { width: Some(200.0), overflow, align_end: true, ..case(0, text) });
            out.push(Case { width: Some(60.0), overflow, align_end: true, ..case(0, text) });
        }
    }
    // The faces scene of tests/faces.rs.
    out.push(Case { size: 13.0, face: 1, tracking: 0.2, ..case(1, "INVOKE") });
    let row = Case {
        spans: vec![(1, 2), (8, 9), (15, 17)],
        emphasis: vec![(1, 2), (8, 9)],
        emphasis_face: Some(2),
        ..case(1, "do-not-disturb\ton")
    };
    out.push(row.clone());
    out.push(Case { width: Some(70.0), ..row.clone() });
    out.push(Case { tracking: 0.1, ..case(1, "rite of oil") });
    out.push(Case {
        emphasis: vec![(0, 4)],
        emphasis_face: Some(1),
        ..case(1, "rite of oil")
    });
    out.push(Case {
        emphasis: vec![(1, 3)],
        emphasis_face: Some(2),
        width: Some(40.0),
        ..case(1, "Hail the machine")
    });
    out
}

/// The pieces of logis's render::text::pieces: cuts wherever a colour
/// span or an emphasis begins or ends.
fn pieces(c: &Case, len: usize) -> Vec<(usize, usize, bool, bool)> {
    let mut cuts = vec![0, len];
    for &(a, b) in c.spans.iter().chain(&c.emphasis) {
        cuts.push(a);
        cuts.push(b);
    }
    cuts.sort_unstable();
    cuts.dedup();
    cuts.windows(2)
        .filter(|w| w[0] < w[1])
        .map(|w| {
            let coloured = c.spans.iter().any(|&(a, b)| a <= w[0] && w[0] < b);
            let marked = c.emphasis.iter().any(|&(a, b)| a <= w[0] && w[0] < b);
            (w[0], w[1], coloured, marked)
        })
        .collect()
}

fn json_str(out: &mut String, s: &str) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            c if (c as u32) < 0x20 => write!(out, "\\u{:04x}", c as u32).unwrap(),
            c => out.push(c),
        }
    }
    out.push('"');
}

fn main() {
    let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../../tests/fonts");
    let mut systems = Vec::new();
    let mut ids = Vec::new();
    for set in &SETS {
        let mut system = FontSystem::new_with_locale_and_db_and_fallback(
            "en-US".into(),
            fontdb::Database::new(),
            ResolvedOnly,
        );
        let mut order = Vec::new();
        for f in set.files {
            let path = dir.join(f).canonicalize().unwrap();
            let loaded = system.db_mut().load_font_source(fontdb::Source::File(path));
            order.push(loaded[0]);
        }
        systems.push(system);
        ids.push(order);
    }
    let mut out = String::from("{\"sets\":[");
    for (i, set) in SETS.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(out, "{{\"name\":\"{}\",\"files\":[", set.name).unwrap();
        for (j, f) in set.files.iter().enumerate() {
            if j > 0 {
                out.push(',');
            }
            json_str(&mut out, f);
        }
        out.push_str("]}");
    }
    out.push_str("],\"cases\":[");
    let mut first = true;
    for c in cases() {
        for scale in [1.0f32, 1.3, 2.0] {
            if !first {
                out.push(',');
            }
            first = false;
            lay(&mut out, &mut systems[c.set], &ids[c.set], &SETS[c.set], &c, scale);
        }
    }
    out.push_str("]}\n");
    print!("{out}");
}

fn lay(out: &mut String, system: &mut FontSystem, ids: &[fontdb::ID], set: &Set, c: &Case, f: f32) {
    let size = (c.size * f).max(1.0);
    let line_height = (c.size * 1.3 * f).max(1.0);
    let column = c.width.map(|w| {
        let left = (c.origin_x * f).round() as i32;
        (left, left + (w.max(0.0) * f).round() as i32)
    });
    let width = column.map(|(l, r)| (r - l) as f32);
    let face = |i: usize| -> (String, u16) {
        match set.faces.get(i).or(set.faces.first()) {
            Some(&(family, weight)) => (family.to_owned(), weight),
            None => ("Noto Sans".to_owned(), 400),
        }
    };
    let (family, weight) = face(c.face);
    let emphasis_face = c.emphasis_face.map(face);
    let mut attrs = Attrs::new().family(Family::Name(&family)).weight(Weight(weight));
    if c.tracking != 0.0 {
        attrs = attrs.letter_spacing(c.tracking);
    }
    let emphasised = match &emphasis_face {
        Some((family, weight)) => attrs.clone().family(Family::Name(family)).weight(Weight(*weight)),
        None => attrs.clone(),
    };
    let mut buffer = Buffer::new_empty(Metrics::new(size, line_height));
    let mut b = buffer.borrow_with(system);
    b.set_wrap(Wrap::None);
    b.set_hinting(Hinting::Enabled);
    b.set_size(width, None);
    if width.is_some() && c.overflow == Overflow::Ellipsis {
        b.set_ellipsize(Ellipsize::End(EllipsizeHeightLimit::Lines(1)));
    }
    let line = c.text.replace(['\n', '\r'], " ");
    let align = c.align_end.then_some(Align::End);
    if c.spans.is_empty() && c.emphasis.is_empty() {
        b.set_text(&line, &attrs, Shaping::Advanced, align);
    } else {
        let gold = Color::rgba(0xf2, 0xc1, 0x4e, 0xff);
        let pieces = pieces(c, line.len()).into_iter().map(|(a, z, coloured, marked)| {
            let base = if marked { &emphasised } else { &attrs };
            let attrs = if coloured { base.clone().color(gold) } else { base.clone() };
            (&line[a..z], attrs)
        });
        b.set_rich_text(pieces, &attrs, Shaping::Advanced, align);
    }
    b.shape_until_scroll(false);

    out.push('{');
    write!(out, "\"set\":\"{}\",\"scale\":{},\"text\":", set.name, f).unwrap();
    json_str(out, c.text);
    write!(
        out,
        ",\"size\":{},\"line_height\":{},\"face\":{},\"tracking\":{},\"align_end\":{},\"ellipsis\":{}",
        size,
        line_height,
        c.face,
        c.tracking,
        c.align_end,
        c.overflow == Overflow::Ellipsis
    )
    .unwrap();
    match width {
        Some(w) => write!(out, ",\"width\":{w}").unwrap(),
        None => out.push_str(",\"width\":null"),
    }
    let ranges = |r: &[(usize, usize)]| -> String {
        r.iter().map(|(a, b)| format!("[{a},{b}]")).collect::<Vec<_>>().join(",")
    };
    write!(
        out,
        ",\"spans\":[{}],\"emphasis\":[{}],\"emphasis_face\":{}",
        ranges(&c.spans),
        ranges(&c.emphasis),
        c.emphasis_face.map_or("null".to_owned(), |f| f.to_string())
    )
    .unwrap();
    out.push_str(",\"runs\":[");
    for (i, run) in buffer.layout_runs().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(
            out,
            "{{\"rtl\":{},\"line_y\":{},\"line_top\":{},\"line_w\":{},\"glyphs\":[",
            run.rtl, run.line_y, run.line_top, run.line_w
        )
        .unwrap();
        for (j, g) in run.glyphs.iter().enumerate() {
            if j > 0 {
                out.push(',');
            }
            let font = ids.iter().position(|&id| id == g.font_id).map_or(-1, |p| p as i64);
            write!(
                out,
                "[{},{},{},{},{},{},{},{},{},{}]",
                font,
                g.glyph_id,
                g.x,
                g.y,
                g.w,
                g.x_offset,
                g.y_offset,
                g.start,
                g.end,
                g.level.number()
            )
            .unwrap();
        }
        out.push_str("]}");
    }
    out.push_str("]}");
}
