//! Loading themes: bundled and user files, `extends`, the default as the
//! last base, and errors that say where.

use std::path::PathBuf;
use std::time::Duration;

use super::*;
use crate::render::Rgba;

/// A scratch themes directory, removed on drop.
struct Dir(PathBuf);

impl Dir {
    fn new(name: &str) -> Self {
        let dir = std::env::temp_dir().join(format!("logis-themes-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        Self(dir)
    }

    fn with(self, name: &str, text: &str) -> Self {
        std::fs::write(self.0.join(format!("{name}.jsonc")), text).unwrap();
        self
    }

    fn themes(&self) -> Themes {
        Themes::new(Some(self.0.clone()))
    }
}

impl Drop for Dir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

#[test]
fn both_bundled_themes_load() {
    for (name, _) in BUNDLED {
        let theme = Themes::default().load(name).unwrap();
        assert_eq!(theme.name, name);
        assert_eq!(theme.dir, None);
    }
}

#[test]
fn the_default_is_forge_with_the_decided_geometry_and_motifs() {
    let forge = Theme::bundled_default();
    assert_eq!(forge.name, "forge-crimson-brass");
    assert_eq!((forge.window.width, forge.window.height), (820.0, 520.0));
    assert_eq!(forge.window.rows, 9);
    assert!(forge.rivets.is_some(), "corner rivets");
    assert_eq!(forge.prompt.sigil.as_ref().unwrap().image, "gear");
    assert!(forge.border.highlight.is_some(), "a travelling highlight");
    assert!(forge.glow.pulse > 0.0, "a pulsing glow");
    assert!(
        forge.effects.flicker > 0.0 && forge.effects.flicker < 0.05,
        "subtle flicker"
    );
    assert_eq!(forge.border.paint.len(), 3, "brass, ember, crimson");
    let body = &forge.faces[forge.row.font.face];
    assert_eq!(body.families, ["IBM Plex Mono", "monospace"]);
    let label = &forge.faces[forge.prompt.font.face];
    assert_eq!(label.families, ["Cinzel", "serif"]);
}

#[test]
fn phosphor_is_a_green_crt_with_curvature_bloom_and_inverse_selection() {
    let phosphor = Themes::default().load("cogitator-phosphor").unwrap();
    assert!(phosphor.effects.curvature > 0.0 && phosphor.effects.bloom > 0.0);
    assert!(phosphor.rivets.is_none() && phosphor.prompt.sigil.is_none());
    let face = &phosphor.faces[phosphor.row.font.face];
    assert_eq!(face.families, ["VT323", "Terminus", "monospace"]);
    // Inverse: the band is the bright phosphor, the text on it dark.
    let band = phosphor.selection.band[0];
    let text = phosphor.selection.columns[0];
    assert!(band.g > 0.9 && text.g < 0.1, "{band:?} {text:?}");
    assert!(phosphor.matched.underline);
}

#[test]
fn a_user_file_overrides_a_bundled_theme_of_its_name() {
    let dir = Dir::new("override").with(
        "forge-crimson-brass",
        r##"{"window": {"rows": 4}, "palette": {"brass": "#000000"}}"##,
    );
    let theme = dir.themes().load("forge-crimson-brass").unwrap();
    assert_eq!(theme.window.rows, 4);
    assert_eq!(theme.prompt.color, Rgba::hex(0), "the prompt is brass");
    assert_eq!(theme.dir.as_deref(), Some(dir.0.as_path()));
    assert_eq!(theme.window.width, 820.0, "the rest from the default");
}

#[test]
fn values_not_given_fall_back_to_the_default() {
    let dir = Dir::new("sparse").with("sparse", "// nothing at all\n");
    let theme = dir.themes().load("sparse").unwrap();
    let forge = Theme::bundled_default();
    assert_eq!(theme.name, "sparse");
    assert_eq!(theme.border, forge.border);
    assert_eq!(theme.faces, forge.faces);
}

#[test]
fn a_theme_extends_another_and_overrides_its_keys() {
    let dir = Dir::new("extends").with(
        "amber",
        r##"{"extends": "cogitator-phosphor", "palette": {"phosphor": "#ffb000"}}"##,
    );
    let amber = dir.themes().load("amber").unwrap();
    assert_eq!(amber.selection.band[0], Rgba::hex(0xffb000));
    let phosphor = Themes::default().load("cogitator-phosphor").unwrap();
    assert_eq!(amber.effects, phosphor.effects);
    assert_eq!(amber.rivets, None, "phosphor's 'off' carries over");
}

#[test]
fn a_theme_extending_its_own_name_amends_the_bundled_one() {
    let dir = Dir::new("amend").with(
        "cogitator-phosphor",
        r#"{"extends": "cogitator-phosphor", "effects": {"curvature": 0}}"#,
    );
    let theme = dir.themes().load("cogitator-phosphor").unwrap();
    assert_eq!(theme.effects.curvature, 0.0);
    assert!(theme.effects.bloom > 0.0, "the rest is phosphor's");
}

#[test]
fn extends_goes_one_level_and_never_in_a_circle() {
    let dir = Dir::new("levels")
        .with("a", r#"{"extends": "b"}"#)
        .with("b", r#"{"extends": "a"}"#)
        .with("c", r#"{"extends": "d"}"#)
        .with("d", r#"{"extends": "cogitator-phosphor"}"#)
        .with("e", r#"{"extends": "e"}"#);
    let e = dir.themes().load("a").unwrap_err();
    assert!(e.problem.contains("circle"), "{e}");
    let e = dir.themes().load("c").unwrap_err();
    assert!(e.problem.contains("at most one level"), "{e}");
    let e = dir.themes().load("e").unwrap_err();
    assert!(e.problem.contains("names this theme itself"), "{e}");
    let e = Dir::new("unknown-parent")
        .with("f", r#"{"extends": "nowhere"}"#)
        .themes()
        .load("f")
        .unwrap_err();
    assert!(e.problem.contains("no such theme"), "{e}");
}

#[test]
fn an_unknown_theme_names_what_there_is() {
    let e = Dir::new("unknown").themes().load("purple").unwrap_err();
    assert_eq!(e.source, "theme 'purple'");
    assert!(
        e.problem
            .contains("forge-crimson-brass, cogitator-phosphor"),
        "{e}"
    );
}

#[test]
fn a_malformed_file_is_reported_with_line_and_column() {
    let dir = Dir::new("malformed").with("bad", "{\n  \"glow\": {\"radus\": 3}\n}");
    let e = dir.themes().load("bad").unwrap_err();
    let path = dir.0.join("bad.jsonc");
    assert_eq!(e.source, format!("{}:2:18", path.display()));
    assert!(e.problem.contains("unknown field `radus`"), "{e}");
}

#[test]
fn a_bad_reference_or_value_names_its_key() {
    let dir = Dir::new("semantic")
        .with("a", r#"{"border": {"paint": ["brass", "embr"]}}"#)
        .with("b", r#"{"effects": {"bloom": 2}}"#)
        .with("c", r#"{"row": {"font": "heading"}}"#)
        .with("d", r#"{"window": {"rows": 0}}"#);
    let problem = |name| dir.themes().load(name).unwrap_err().problem;
    assert_eq!(
        problem("a"),
        "border.paint[1]: no colour named 'embr' in the palette"
    );
    assert!(problem("b").starts_with("effects.bloom: 2 is out of range"));
    assert!(problem("c").starts_with("row.font: no font role 'heading'"));
    assert!(problem("d").starts_with("window.rows:"));
}

#[test]
fn reduce_motion_stills_every_continuous_effect() {
    let forge = Theme::bundled_default();
    assert!(forge.is_continuous());
    let still = forge.clone().stilled();
    assert!(!still.is_continuous());
    assert!(!still.shimmers());
    assert_eq!(still.border.highlight.unwrap().period, Duration::ZERO);
    assert_eq!(
        still.effects.scanlines, forge.effects.scanlines,
        "static effects stay"
    );
}
