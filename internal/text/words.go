package text

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/bidi"
	"github.com/nerdwave-nick/logis/internal/grapheme"
	"github.com/nerdwave-nick/logis/internal/linebreak"
)

// word is cosmic-text's ShapeWord: a run between break opportunities, or
// one white space character (blank).
type word struct {
	blank  bool
	glyphs []shapedGlyph
}

func (w *word) width(size float32) float32 {
	var sum float32
	for i := range w.glyphs {
		sum += w.glyphs[i].width(size)
	}
	return sum
}

// span is cosmic-text's ShapeSpan: the words of one bidi level run.
type span struct {
	level int8
	words []word
}

func (s *span) rtl() bool { return s.level%2 == 1 }

// setting is what shapes a line: its options and each byte's face.
type setting struct {
	f    *Fonts
	line string
	opt  LineOptions
	rtl  bool
}

// faceAt is the face index of byte i.
func (s *setting) faceAt(i int) int {
	for _, fs := range s.opt.Faces {
		if i >= fs.Start && i < fs.End {
			return fs.Face
		}
	}
	return s.opt.Face
}

// compatible reports faces that shape alike (cosmic-text compares family,
// weight, stretch and style).
func (s *setting) compatible(a, b int) bool {
	fa, wa := s.f.Face(a)
	fb, wb := s.f.Face(b)
	return fa == fb && wa == wb
}

// spans is cosmic-text's ShapeLine::build: bidi level runs, their words
// shaped.
func (s *setting) spans() []span {
	runes := []rune(s.line)
	levels, rtl := bidi.Resolve(runes)
	s.rtl = rtl
	var spans []span
	start, i := 0, 0
	for at := range s.line {
		if i > 0 && levels[i] != levels[i-1] {
			spans = append(spans, s.span(start, at, levels[i-1]))
			start = at
		}
		i++
	}
	if len(s.line) > 0 {
		spans = append(spans, s.span(start, len(s.line), levels[len(levels)-1]))
	}
	return spans
}

// tabStops widens each tab (shaped as a space) to the next stop at size:
// stops are 8 drawn spaces apart from the line's start, so the glyph
// after a tab stands exactly at its stop.
func (s *setting) tabStops(spans []span, size float32) {
	var x float32
	for si := range spans {
		for wi := range spans[si].words {
			for gi := range spans[si].words[wi].glyphs {
				g := &spans[si].words[wi].glyphs[gi]
				if g.end == g.start+1 && s.line[g.start] == '\t' {
					if tab := 8 * g.width(size); tab > 0 {
						stop := float32(math.Floor(float64(x/tab))+1) * tab
						g.xAdvance = (stop - x) / size
					}
				}
				x += g.width(size)
			}
		}
	}
}

// span shapes line[start:end] at level into words.
func (s *setting) span(start, end int, level int8) span {
	text := s.line[start:end]
	sp := span{level: level}
	rtl := level%2 == 1
	from := 0
	for _, brk := range linebreak.Breaks(text) {
		if brk > 0 && brk < len(text) && s.ligatureAcross(text, start, brk) {
			continue
		}
		blankFrom := brk
		for blankFrom > from {
			r, n := utf8.DecodeLastRuneInString(text[from:blankFrom])
			if !unicode.IsSpace(r) {
				break
			}
			blankFrom -= n
		}
		if from < blankFrom {
			sp.words = append(sp.words, s.word(start+from, start+blankFrom, rtl, false))
		}
		for i, r := range text[blankFrom:brk] {
			at := start + blankFrom + i
			sp.words = append(sp.words, s.word(at, at+utf8.RuneLen(r), rtl, true))
		}
		from = brk
	}
	if s.rtl {
		for i := range sp.words {
			reverse(sp.words[i].glyphs)
		}
	}
	if s.rtl != rtl {
		reverse(sp.words)
	}
	return sp
}

// ligatureAcross keeps two ASCII punctuation marks around a break in one
// word when the font ligates or substitutes them ("->", "!=").
func (s *setting) ligatureAcross(text string, start, brk int) bool {
	pre, _ := utf8.DecodeLastRuneInString(text[:brk])
	post, _ := utf8.DecodeRuneInString(text[brk:])
	if !isASCIIPunct(pre) || !isASCIIPunct(post) {
		return false
	}
	face := s.f.face(s.faceAt(start + brk))
	probe := string([]rune{pre, post})
	glyphs, _ := s.f.shapeWith(face, probe, 0, len(probe), false, 0)
	if len(glyphs) < 2 {
		return true
	}
	if len(glyphs) == 2 {
		font := s.f.fonts[face]
		return glyphs[0].id != font.GlyphIndex(pre) || glyphs[1].id != font.GlyphIndex(post)
	}
	return false
}

func isASCIIPunct(r rune) bool {
	return r < 0x80 && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r)
}

// word shapes line[start:end] (cosmic-text's ShapeWord::build): in runs of
// one face, cut at grapheme boundaries.
func (s *setting) word(start, end int, rtl, blank bool) word {
	w := word{blank: blank}
	text := s.line[start:end]
	simple := true
	for i := 0; i < len(text); i++ {
		if c := text[i]; c >= 0x80 || c < 0x20 && c != '\t' || c == 0x7f {
			simple = false
			break
		}
	}
	if simple {
		for i := start + 1; i < end && simple; i++ {
			simple = s.compatible(s.faceAt(start), s.faceAt(i))
		}
	}
	if simple {
		w.glyphs = s.run(start, end, rtl)
		return w
	}
	runStart, face := start, s.opt.Face
	for at := start; at < end; at += grapheme.Next(s.line[at:end]) {
		if !s.compatible(face, s.faceAt(at)) {
			w.glyphs = append(w.glyphs, s.run(runStart, at, rtl)...)
			runStart, face = at, s.faceAt(at)
		}
	}
	w.glyphs = append(w.glyphs, s.run(runStart, end, rtl)...)
	return w
}

func (s *setting) run(start, end int, rtl bool) []shapedGlyph {
	if start >= end {
		return nil
	}
	glyphs := s.f.shapeRun(s.line, start, end, rtl, s.f.face(s.faceAt(start)), s.opt.Tracking)
	for i := range glyphs {
		g := &glyphs[i]
		g.blank = strings.TrimFunc(s.line[g.start:g.end], unicode.IsSpace) == ""
	}
	return glyphs
}

// ellipsis is the "…" a cut line ends with, in the face of its first byte
// ("..." if no font has it), in the line's direction.
func (s *setting) ellipsis() []shapedGlyph {
	face := s.f.face(s.faceAt(0))
	glyphs := s.f.shapeRun("\u2026", 0, len("\u2026"), s.rtl, face, s.opt.Tracking)
	missing := true
	for _, g := range glyphs {
		missing = missing && g.id == 0
	}
	if missing {
		glyphs = s.f.shapeRun("...", 0, 3, s.rtl, face, s.opt.Tracking)
	}
	if s.rtl {
		reverse(glyphs)
	}
	return glyphs
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
