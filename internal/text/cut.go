package text

// The cut of a line too wide for its column: what fits beside the
// ellipsis, the blanks dropped before it, and the offset it stands for.

// trimBlankEnd drops the blank glyphs at the logical end of what a cut
// keeps, so that the ellipsis follows the last visible glyph: blank words
// before the word the cut falls in, blanks of that word, and the trailing
// blanks of earlier spans, its width shrinking with them. In a span of the
// paragraph's direction the logical end is a range's end; in one of the
// other direction (stored in reverse) its start.
func (v *visualLine) trimBlankEnd(spans []span, rtl bool, size float32) {
	for n := len(v.ranges); n > 0; n = len(v.ranges) {
		r := &v.ranges[n-1]
		words := spans[r.span].words
		congruent := rtl == spans[r.span].rtl()
		var g *shapedGlyph
		if congruent {
			r.end = lastGlyph(words, r.start, r.end)
			if r.end != r.start {
				g = &words[r.end.word].glyphs[r.end.glyph-1]
			}
		} else {
			r.start = firstGlyph(words, r.start, r.end)
			if r.start != r.end {
				g = &words[r.start.word].glyphs[r.start.glyph]
			}
		}
		switch {
		case g == nil:
			v.ranges = v.ranges[:n-1] // nothing left in it
		case !g.blank:
			return
		default:
			v.w -= g.width(size)
			if congruent {
				r.end.glyph--
			} else {
				r.start.glyph++
			}
		}
	}
}

// lastGlyph is end moved back to just after a glyph (glyph > 0), or to
// start when no glyph lies between them.
func lastGlyph(words []word, start, end pos) pos {
	for end != start {
		if end.glyph > 0 {
			return end
		}
		end.word--
		end.glyph = len(words[end.word].glyphs)
		if end.word == start.word && end.glyph <= start.glyph {
			return start
		}
	}
	return end
}

// firstGlyph is start moved on to a glyph, or to end when no glyph lies
// between them.
func firstGlyph(words []word, start, end pos) pos {
	for start != end {
		if start.word < len(words) && start.glyph < len(words[start.word].glyphs) &&
			(start.word < end.word || start.glyph < end.glyph) {
			return start
		}
		if start.word >= end.word {
			return end
		}
		start = pos{start.word + 1, 0}
	}
	return start
}

// remainingExceeds reports whether what follows word wi of span si is
// wider than threshold.
func remainingExceeds(spans []span, size float32, si, wi int, congruent bool, threshold float32) bool {
	var acc float32
	words := spans[si].words
	from, to := wi+1, len(words)
	if !congruent {
		from, to = 0, wi
	}
	for i := from; i < to; i++ {
		if acc += words[i].width(size); acc > threshold {
			return true
		}
	}
	for s := si + 1; s < len(spans); s++ {
		for i := range spans[s].words {
			if acc += spans[s].words[i].width(size); acc > threshold {
				return true
			}
		}
	}
	return false
}

// fitGlyphs is how many of w's glyphs fit beside used within available,
// from its start (forward) or its end, and their width.
func fitGlyphs(w *word, size, used, available float32, forward bool) (int, float32) {
	var gw float32
	if forward {
		end := 0
		for i := range w.glyphs {
			g := w.glyphs[i].width(size)
			if used+gw+g > available {
				break
			}
			gw += g
			end = i + 1
		}
		return end, gw
	}
	end := len(w.glyphs)
	for i := len(w.glyphs) - 1; i >= 0; i-- {
		g := w.glyphs[i].width(size)
		if used+gw+g > available {
			break
		}
		gw += g
		end = i
	}
	return end, gw
}

// elided is the byte offset an ellipsis stands for: where the visible
// text before it (in logical order; the ellipsis is the last range) ends,
// or the text's start when none is visible.
func elided(spans []span, vl *visualLine) int {
	if !vl.ellipsized {
		return 0
	}
	for i := len(vl.ranges) - 2; i >= 0; i-- {
		end, ok := 0, false
		rangeGlyphs(spans[vl.ranges[i].span].words, vl.ranges[i], func(_ *word, g *shapedGlyph) {
			end, ok = max(end, g.end), true
		})
		if ok {
			return end
		}
	}
	return 0
}
