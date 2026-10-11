//! The prompt line: the sigil, the theme's label, the session's prompt and
//! the query (or its placeholder) with the caret, and the counter at the
//! end.

use super::{Style, advance, chrome, column, text};
use crate::list::Options;
use crate::list::view::Look;
use crate::render::{Caret, CaretShape, Overflow, Scene};
use crate::theme::doc;

/// Draws the prompt line across the top of `body`.
pub(super) fn line(scene: &mut Scene, look: &Look, body: [f32; 4], style: Style<'_>) {
    let theme = style.theme;
    let p = &theme.prompt;
    let [x, y, w, _] = body;
    let centre = y + p.height / 2.0;
    let mut left = x + theme.panel.padding;
    let right = x + w - theme.panel.padding;
    if let Some(sigil) = &p.sigil {
        // Images other than logis's own gear wait for the image source.
        if sigil.image == "gear" {
            chrome::gear(scene, (left + sigil.size / 2.0, centre), sigil.size, sigil.color);
        }
        left += sigil.size + p.gap;
    }
    if !p.label.is_empty() {
        let label = text(p.label.clone(), left, centre, &p.font, p.color);
        left += p.label.chars().count() as f32 * advance(&p.font) + p.gap;
        scene.texts.push(label);
    }
    let mut room = right - left;
    if let Some((matched, total)) = look.counter {
        let c = &theme.counter;
        let words = c
            .format
            .replace("{matched}", &matched.to_string())
            .replace("{total}", &total.to_string());
        room -= (words.chars().count() as f32 + 2.0) * advance(&c.font);
        let mut counter = column(text(words, left, centre, &c.font, c.color), right - left, Overflow::Clip);
        counter.align_end = true;
        scene.texts.push(counter);
    }
    scene.texts.push(query(look, left, centre, room, style));
}

/// The session's prompt and the query (or the placeholder), one line in
/// the query's font, with the caret.
fn query(look: &Look, left: f32, centre: f32, room: f32, style: Style<'_>) -> crate::render::Text {
    let theme = style.theme;
    let q = &theme.query;
    // A label stands for the default prompt: "INVOKE", not "INVOKE > ".
    let prompt = if !theme.prompt.label.is_empty() && look.prompt == Options::default().prompt {
        ""
    } else {
        look.prompt.as_str()
    };
    let mut line = prompt.to_owned();
    let mut spans = vec![(0..line.len(), theme.prompt.color)];
    let input = match look.cursor {
        Some(_) => q.color,
        None => q.placeholder,
    };
    let start = line.len();
    line.push_str(&look.input);
    spans.push((start..line.len(), input));
    spans.retain(|(r, _)| !r.is_empty());
    let mut t = column(text(line, left, centre, &q.font, q.color), room, Overflow::Ellipsis);
    t.spans = spans;
    t.caret = look.cursor.map(|cursor| Caret {
        at: start + cursor.min(look.input.len()),
        shape: match q.caret.shape {
            doc::CaretShape::Bar => CaretShape::Bar,
            doc::CaretShape::Block => CaretShape::Block,
            doc::CaretShape::Underscore => CaretShape::Underscore,
        },
        width: q.caret.width,
        color: q.caret.color,
        glow: q.caret.glow,
    });
    t
}
