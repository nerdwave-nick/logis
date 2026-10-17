package scene

import (
	"math"
	"slices"
	"strings"

	"github.com/nerdwave-nick/logis/internal/linebreak"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	fonts "github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The toast: a strip across the bottom of the body, edged in the colour of
// its level, holding its whole text. The words wrap at line break
// opportunities (UAX #14) and at line breaks onto as many lines as they
// need, at most toastLines, and never so many that the strip rises above
// the rule under the prompt; the last line holds the rest and ends in "…"
// when it does not fit. The strip grows upward from where a one-line toast
// lies, and the rows it covers are not drawn while it shows.
//
// Where the lines break is measured with the renderer's fonts (ToastFit),
// once per text, panel width and font, never on every frame; unmeasured,
// the breaks are estimated from the font's average advance.

const (
	// toastInset is the toast strip's distance from the body's edges.
	toastInset = 8
	// toastLines is the most lines a toast takes.
	toastLines = 4
	// toastPad is the strip's padding: 6 above and below its lines, 12
	// before and after them.
	toastPad = 6
)

// toastPitch is the height of a toast's line in theme t.
func toastPitch(t *theme.Theme) float32 { return t.Row.Font.Size * 1.3 }

// toastHeight is the height of a toast strip of lines lines in theme t.
func toastHeight(t *theme.Theme, lines int) float32 {
	return float32(lines)*toastPitch(t) + 2*toastPad
}

// toastTop is the top of a toast strip of lines lines across the bottom
// of body: rows reaching below it are not drawn while it shows (rows).
func toastTop(body [4]float32, t *theme.Theme, lines int) float32 {
	return body[1] + body[3] - toastHeight(t, lines) - toastInset
}

// toastWidth is how wide a toast's lines may be in a body width wide.
func toastWidth(width float32) float32 { return width - 2*toastInset - 4*toastPad }

// toastRoom is how many lines a toast may take in body: toastLines, fewer
// when the strip would rise above the list's top, at least one.
func toastRoom(body [4]float32, t *theme.Theme) int {
	room := body[3] - listTop(t) - toastInset - 2*toastPad
	return min(max(int(math.Floor(float64(room/toastPitch(t)))), 1), toastLines)
}

// ToastFit is where a toast's lines break, remembered for its text, the
// panel's width and the font. The zero value has measured nothing.
type ToastFit struct {
	text     string
	width    float32
	font     theme.Font
	face     fonts.Face
	breaks   []int
	measured bool
}

// Fit gives l's toast its line breaks in a panel of logical size, measured
// with m, or estimated where m cannot measure (no fonts yet: measured on
// a later frame). Without a toast, l has none.
func (f *ToastFit) Fit(l *list.Look, logical scale.Size, t *theme.Theme, m Measure) {
	l.ToastBreaks, l.ToastFitted = nil, false
	if l.Toast == nil {
		return
	}
	body, _ := Body(logical, t)
	width := toastWidth(body[2])
	font := t.Row.Font
	var face fonts.Face
	if font.Face >= 0 && font.Face < len(t.Faces) {
		face = t.Faces[font.Face]
	}
	if !f.measured || f.text != l.Toast.Text || f.width != width || f.font != font || !sameFace(f.face, face) {
		breaks, measured := toastBreaks(l.Toast.Text, width, func(line string) (float32, bool) {
			return m.Width(text(line, 0, 0, font, render.Color{}))
		}, font)
		*f = ToastFit{text: l.Toast.Text, width: width, font: font,
			face:   fonts.Face{Families: slices.Clone(face.Families), Weight: face.Weight},
			breaks: breaks, measured: measured}
	}
	l.ToastBreaks, l.ToastFitted = slices.Clone(f.breaks), true
}

// toastBreaks are where words' lines after the first start, at most
// toastLines-1 of them, as wide as width at most where a break allows
// (a word wider than a line takes a line of its own); and whether every
// width came from widthOf (else some were estimated in font).
func toastBreaks(words string, width float32, widthOf func(string) (float32, bool), font theme.Font) ([]int, bool) {
	measured := true
	wide := func(start, end int) float32 {
		line := strings.TrimRight(words[start:end], " \t\r\n")
		w, ok := widthOf(line)
		if !ok {
			measured = false
			w = estimate(line, font)
		}
		return w
	}
	var breaks []int
	start, at := 0, 0 // the line so far is words[start:at]
	for _, b := range linebreak.Breaks(words) {
		if len(breaks) == toastLines-1 {
			break
		}
		if at > start && wide(start, b) > width {
			breaks = append(breaks, at)
			start = at
			if len(breaks) == toastLines-1 {
				break
			}
		}
		at = b
		if strings.HasSuffix(words[:b], "\n") && b < len(words) {
			breaks = append(breaks, b)
			start = b
		}
	}
	return breaks, measured
}

// toastLinesOf are the lines l's toast shows in body: its fitted breaks,
// else estimated ones, as many as there is room for; the last holds the
// rest of the words.
func toastLinesOf(l *list.Look, body [4]float32, t *theme.Theme) []string {
	words := l.Toast.Text
	breaks := l.ToastBreaks
	if !l.ToastFitted {
		font := t.Row.Font
		breaks, _ = toastBreaks(words, toastWidth(body[2]), func(string) (float32, bool) { return 0, false }, font)
	}
	breaks = breaks[:min(len(breaks), toastRoom(body, t)-1)]
	starts := append([]int{0}, breaks...)
	lines := make([]string, len(starts))
	for i, s := range starts {
		end := len(words)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		line := strings.TrimRight(words[s:end], " \t\r\n")
		lines[i] = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(line)
	}
	return lines
}

// toast draws a toast's strip across the bottom of body, edged in the
// colour of its level, and its lines.
func toast(sc *render.Scene, n list.Notice, lines []string, body [4]float32, t *theme.Theme) {
	height := toastHeight(t, len(lines))
	edge := t.Prompt.Color
	switch n.Level {
	case list.Success:
		edge = t.Match.Color
	case list.Error:
		edge = t.Error
	}
	rect := [4]float32{body[0] + toastInset, toastTop(body, t, len(lines)), body[2] - 2*toastInset, height}
	base := render.Hex(0)
	if len(t.Panel.Fill) > 0 {
		base = t.Panel.Fill[len(t.Panel.Fill)-1]
	}
	sc.Shapes = append(sc.Shapes, render.RoundedRect{
		Rect: rect, Radius: 6, Fill: render.Solid(base.WithAlpha(0.97)),
		Border: render.Border{Width: 1.5, Paint: render.Solid(edge)},
		Glow:   render.Glow{Radius: 8, Color: edge.WithAlpha(0.35)},
	})
	pitch := toastPitch(t)
	for i, line := range lines {
		centre := rect[1] + toastPad + pitch*float32(i) + pitch/2
		words := text(line, rect[0]+2*toastPad, centre, t.Row.Font, t.Row.Columns[0])
		sc.Texts = append(sc.Texts, column(words, rect[2]-4*toastPad, render.Ellipsis))
	}
}
