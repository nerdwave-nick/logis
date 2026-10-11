//! Which bytes of a row's text the query matched, for highlighting.

use std::ops::Range;

/// The byte ranges of `text` covering the grapheme `positions` (ascending),
/// adjacent graphemes joined into one range.
pub fn byte_ranges(text: &str, positions: &[u32]) -> Vec<Range<usize>> {
    use unicode_segmentation::UnicodeSegmentation;
    let mut ranges: Vec<Range<usize>> = Vec::new();
    let mut wanted = positions.iter().copied().peekable();
    // ASCII text is matched byte by byte (nucleo's own convention).
    let units: Box<dyn Iterator<Item = (usize, &str)>> = if text.is_ascii() {
        Box::new((0..text.len()).map(|i| (i, &text[i..=i])))
    } else {
        Box::new(text.grapheme_indices(true))
    };
    for (n, (start, unit)) in units.enumerate() {
        let Some(&next) = wanted.peek() else { break };
        if next as usize != n {
            continue;
        }
        wanted.next();
        let end = start + unit.len();
        match ranges.last_mut() {
            Some(last) if last.end == start => last.end = end,
            _ => ranges.push(start..end),
        }
    }
    ranges
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one joined range is a case
mod tests {
    use super::*;

    #[test]
    fn grapheme_positions_become_joined_byte_ranges() {
        assert_eq!(byte_ranges("xaxbc", &[1, 3, 4]), [1..2, 3..5]);
        assert_eq!(byte_ranges("e\u{301}tude", &[0, 1]), [0..4]);
        assert_eq!(byte_ranges("öλ", &[1]), [2..4]);
        assert_eq!(byte_ranges("ab", &[]), Vec::<Range<usize>>::new());
    }
}
