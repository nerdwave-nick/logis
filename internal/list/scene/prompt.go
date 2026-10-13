package scene

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The prompt line (Rust src/list/scene/prompt.rs): the sigil, the theme's
// label, the session's prompt and the query (or its placeholder) with the
// caret, and the counter at the end.

// promptLine draws the prompt line across the top of body; a sigil image
// comes from icons.
func promptLine(sc *render.Scene, l *list.Look, body [4]float32, s Style, icons Icons) {
	t := s.Theme
	p := t.Prompt
	x, y, w := body[0], body[1], body[2]
	centre := y + p.Height/2
	left, right := x+t.Panel.Padding, x+w-t.Panel.Padding
	if p.HasSigil {
		sigil := p.Sigil
		switch {
		case sigil.Image == "gear":
			gear(sc, left+sigil.Size/2, centre, sigil.Size, sigil.Color)
		case filepath.IsAbs(sigil.Image) && icons != nil:
			// A personal theme's relative path is the theme engine's (G18).
			side := uint32(max(math.Round(float64(sigil.Size)), 1))
			if id, ok := icons.Image(sigil.Image, side); ok {
				sc.Images = append(sc.Images, render.ImageQuad{Image: id,
					Origin: [2]float32{left, centre - float32(side)/2}, Opacity: 1})
			}
		}
		left += sigil.Size + p.Gap
	}
	if p.Label != "" {
		sc.Texts = append(sc.Texts, text(p.Label, left, centre, p.Font, p.Color))
		left += float32(utf8.RuneCountInString(p.Label))*advance(p.Font) + p.Gap
	}
	room := right - left
	if l.Counter {
		c := t.Counter
		words := strings.NewReplacer("{matched}", strconv.Itoa(l.Matched), "{total}", strconv.Itoa(l.Total)).Replace(c.Format)
		room -= float32(utf8.RuneCountInString(words)+2) * advance(c.Font)
		counter := column(text(words, left, centre, c.Font, c.Color), right-left, render.Clip)
		counter.AlignEnd = true
		sc.Texts = append(sc.Texts, counter)
	}
	sc.Texts = append(sc.Texts, query(l, left, centre, room, t))
}

// query is the session's prompt and the query (or the placeholder), one
// line in the query's font, with the caret.
func query(l *list.Look, left, centre, room float32, t *theme.Theme) render.Text {
	q := t.Query
	// A label stands for the default prompt: "INVOKE", not "INVOKE > ".
	prompt := l.Prompt
	if t.Prompt.Label != "" && prompt == list.DefaultOptions().Prompt {
		prompt = ""
	}
	line := prompt + l.Input
	input := q.Color
	if !l.HasCursor {
		input = q.Placeholder
	}
	var spans []render.Span
	for _, sp := range []render.Span{{Start: 0, End: len(prompt), Color: t.Prompt.Color}, {Start: len(prompt), End: len(line), Color: input}} {
		if sp.End > sp.Start {
			spans = append(spans, sp)
		}
	}
	out := column(text(line, left, centre, q.Font, q.Color), room, render.Ellipsis)
	out.Spans = spans
	if l.HasCursor {
		out.Caret = &render.Caret{
			At: len(prompt) + min(l.Cursor, len(l.Input)), Shape: caretShape(q.Caret.Shape),
			Width: q.Caret.Width, Color: q.Caret.Color, Glow: q.Caret.Glow,
		}
	}
	return out
}

func caretShape(s theme.CaretShape) render.CaretShape {
	switch s {
	case theme.CaretBlock:
		return render.CaretBlock
	case theme.CaretUnderscore:
		return render.CaretUnderscore
	}
	return render.CaretBar
}
