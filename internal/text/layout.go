package text

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/bidi"
)

// pos is a word and a glyph within it.
type pos struct{ word, glyph int }

// vlRange is part of a span on the visual line (cosmic-text's VlRange):
// its words from start to end (an end glyph > 0 includes part of that
// word); span -1 is the ellipsis.
type vlRange struct {
	span       int
	start, end pos
	level      int8
}

type visualLine struct {
	ranges     []vlRange
	w          float32
	ellipsized bool
}

func (v *visualLine) add(spans []span, si int, start, end pos, w float32) {
	if start == end {
		return
	}
	v.ranges = append(v.ranges, vlRange{span: si, start: start, end: end, level: spans[si].level})
	v.w += w
}

// Layout sets s on one unwrapped line at size pixels per em with
// whole-pixel advances (the algorithm follows cosmic-text's): bidi runs,
// words at line break opportunities, runs of one face shaped with fallback
// over the loaded fonts, tabs to the next stop of 8 spaces, and — in a
// column — the end alignment and the ellipsis. Line breaks in s are not
// interpreted (callers replace them). Fallback faces are loaded first (see
// Ensure).
func (f *Fonts) Layout(s string, size float32, opt LineOptions) Line {
	f.Ensure(s)
	if opt.Column && opt.Ellipsis {
		f.Ensure("…")
	}
	st := &setting{f: f, line: s, opt: opt}
	spans := st.spans()
	var ellipsis []shapedGlyph
	if opt.Column && opt.Ellipsis {
		ellipsis = st.ellipsis()
	}
	vl := layoutSpans(spans, st.rtl, size, opt, ellipsis)
	return place(spans, ellipsis, &vl, st.rtl, size, opt)
}

// layoutSpans fits the spans on one visual line (cosmic-text's
// layout_spans, forward): in a column with an ellipsis, words up to the
// one that overflows, which keeps the glyphs that still fit beside the
// ellipsis.
func layoutSpans(spans []span, rtl bool, size float32, opt LineOptions, ellipsis []shapedGlyph) visualLine {
	var vl visualLine
	check := opt.Column && opt.Ellipsis && !math.IsInf(float64(opt.Width), 0)
	maxWidth := float32(math.Inf(1))
	if opt.Column {
		maxWidth = opt.Width
	}
	var ellipsisW float32
	for i := range ellipsis {
		ellipsisW += ellipsis[i].width(size)
	}
	var total float32
outer:
	for si := range spans {
		sp := &spans[si]
		congruent := rtl == sp.rtl()
		var rangeW float32
		for k := range sp.words {
			wi := k
			if !congruent {
				wi = len(sp.words) - 1 - k
			}
			w := &sp.words[wi]
			ww := w.width(size)
			if check && (total+rangeW+ww > maxWidth ||
				remainingExceeds(spans, size, si, wi, congruent, ellipsisW) && total+rangeW+ww+ellipsisW > maxWidth) {
				end, gw := fitGlyphs(w, size, total+rangeW, max(maxWidth-ellipsisW, 0), congruent)
				if congruent {
					vl.add(spans, si, pos{}, pos{wi, end}, rangeW+gw)
				} else {
					vl.add(spans, si, pos{wi, end}, pos{len(sp.words), 0}, rangeW+gw)
				}
				vl.ellipsized = true
				break outer
			}
			rangeW += ww
		}
		total += rangeW
		vl.add(spans, si, pos{}, pos{len(sp.words), 0}, rangeW)
	}
	if vl.ellipsized {
		level := int8(0)
		if rtl {
			level = 1
		}
		if n := len(vl.ranges); n > 0 {
			level = vl.ranges[n-1].level
		}
		vl.ranges = append(vl.ranges, vlRange{span: -1, end: pos{1, 0}, level: level})
		vl.w += ellipsisW
	}
	return vl
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

// rangeGlyphs are the glyphs a range covers, word by word.
func rangeGlyphs(words []word, r vlRange, visit func(w *word, g *shapedGlyph)) {
	last := r.end.word
	if r.end.glyph != 0 {
		last++
	}
	for i := r.start.word; i < last && i < len(words); i++ {
		gs := words[i].glyphs
		lo, hi := 0, len(gs)
		if i == r.start.word {
			lo = r.start.glyph
		}
		if i == r.end.word {
			hi = r.end.glyph
		}
		for j := lo; j < hi && j < len(gs); j++ {
			visit(&words[i], &gs[j])
		}
	}
}

// place positions the visual line's glyphs (cosmic-text's layout_to_buffer
// with hinting): ranges in bidi order, whole-pixel advances, a right to
// left line from its right edge, end alignment within the column.
func place(spans []span, ellipsis []shapedGlyph, vl *visualLine, rtl bool, size float32, opt LineOptions) Line {
	line := Line{RTL: rtl, LayoutWidth: vl.w}
	if len(vl.ranges) == 0 {
		return line
	}
	lineWidth := vl.w
	if opt.Column {
		lineWidth = opt.Width
	}
	var x float32
	if rtl {
		x = lineWidth
	}
	if opt.AlignEnd {
		x = alignEnd(x, lineWidth, vl.w, rtl)
	}
	x = float32(math.Round(float64(x)))
	cut := elided(spans, vl)
	levels := make([]int8, len(vl.ranges))
	for i, r := range vl.ranges {
		levels[i] = r.level
	}
	order := bidi.Reorder(levels)
	if rtl {
		reverse(order)
	}
	emit := func(r vlRange, g *shapedGlyph) {
		adv := float32(math.Round(float64(float32(size * g.xAdvance))))
		if rtl {
			x -= adv
		}
		glyph := Glyph{
			Font: g.font, ID: g.id, Advance: adv, Level: r.level,
			X: x, XOffset: float32(size * g.xOffset), Y: -float32(size * g.yOffset),
			Cluster: g.start, End: g.end,
		}
		if r.span < 0 {
			glyph.Cluster, glyph.End, glyph.Ellipsis = cut, cut, true
		}
		line.Glyphs = append(line.Glyphs, glyph)
		line.Width += adv
		line.Ascent = max(line.Ascent, float32(size*g.ascent))
		line.Descent = max(line.Descent, float32(size*g.descent))
		if !rtl {
			x += adv
		}
	}
	for _, i := range order {
		r := vl.ranges[i]
		if r.span < 0 {
			for j := range ellipsis {
				emit(r, &ellipsis[j])
			}
			continue
		}
		rangeGlyphs(spans[r.span].words, r, func(_ *word, g *shapedGlyph) { emit(r, g) })
	}
	return line
}

// alignEnd moves a line's start so that it ends at the column's end edge.
func alignEnd(x, lineWidth, w float32, rtl bool) float32 {
	if rtl {
		return x - max(lineWidth-w, 0)
	}
	return x + max(lineWidth-w, 0)
}

// elided is the byte offset an ellipsis stands for: where the visible
// text before it ends, or where the text after it starts if none comes
// before.
func elided(spans []span, vl *visualLine) int {
	if !vl.ellipsized {
		return 0
	}
	at := -1
	for i, r := range vl.ranges {
		if r.span < 0 {
			at = i
		}
	}
	bounds := func(r vlRange) (int, int, bool) {
		lo, hi, ok := math.MaxInt, 0, false
		rangeGlyphs(spans[r.span].words, r, func(_ *word, g *shapedGlyph) {
			lo, hi, ok = min(lo, g.start), max(hi, g.end), true
		})
		return lo, hi, ok
	}
	before := 0
	for i := at - 1; i >= 0; i-- {
		if _, hi, ok := bounds(vl.ranges[i]); ok {
			before = hi
			break
		}
	}
	if before != 0 {
		return before
	}
	after := 0
	for _, sp := range spans {
		for _, w := range sp.words {
			for _, g := range w.glyphs {
				after = max(after, g.end)
			}
		}
	}
	for i := at + 1; i < len(vl.ranges); i++ {
		if lo, _, ok := bounds(vl.ranges[i]); ok {
			after = lo
			break
		}
	}
	return after
}
