package list

import (
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/grapheme"
)

// Row is one row in view.
type Row struct {
	// Text is the item's text.
	Text string
	// Highlights are the bytes of Text the query matched (none when the
	// item is matched by another text, --match-nth).
	Highlights []Range
	Selected   bool
	// Glyph is the emblem for the icon column; Icon the icon drawn there
	// instead when found ("" for none).
	Glyph, Icon string
	// Badges are labels for the row's end.
	Badges []string
	// Pending: the item is still pending.
	Pending bool
}

// Look is everything the scene shows of a View at one moment.
type Look struct {
	// Prompt is shown before the query.
	Prompt string
	// Input is the query as shown (masked for a password), or the
	// placeholder.
	Input string
	// Cursor is the cursor's byte offset in Input; HasCursor is false
	// while the placeholder shows.
	Cursor    int
	HasCursor bool
	// Rows are the rows in view, top first.
	Rows []Row
	// Lines is how many rows the list has room for.
	Lines uint32
	// Counter: show Matched of Total items.
	Counter        bool
	Matched, Total int
	// Glyphs: rows keep an icon column (some item has an emblem).
	Glyphs bool
	// Columns are, per column but the last, how wide its text is in
	// logical pixels; empty until measured, when the scene estimates them
	// from the rows in view.
	Columns []float32
	// Toast is a notice over the list; the view leaves it to its owner.
	Toast *Notice
	// ToastBreaks are where the toast's lines after its first start, as
	// byte offsets in Toast.Text, once fitted to the panel's width
	// (ToastFitted: the scene's ToastFit); unfitted, the scene estimates
	// them.
	ToastBreaks []int
	ToastFitted bool
}

// Look is what to draw now.
func (v *View) Look() Look {
	lines := int(v.options.Rows())
	matches := v.filter.Matches()
	shown := matches[min(v.top, len(matches)):]
	shown = shown[:min(lines, len(shown))]
	rows := make([]Row, len(shown))
	for i, m := range shown {
		text := v.texts[m.Index]
		r := Row{Text: text, Selected: v.top+i == v.selected}
		if v.highlightable[m.Index] {
			r.Highlights = ByteRanges(text, v.filter.Positions(m.Index))
		}
		if d := v.decor[m.Index]; d != nil {
			r.Glyph, r.Icon, r.Badges, r.Pending = d.Glyph, d.Icon, d.Badges, d.Pending
		}
		rows[i] = r
	}
	look := Look{
		Prompt: v.options.Prompt,
		Rows:   rows,
		Lines:  v.options.Rows(),
		Glyphs: v.emblems > 0,
	}
	look.Input, look.Cursor, look.HasCursor = v.input()
	if v.options.Counter {
		look.Counter, look.Matched, look.Total = true, len(matches), v.filter.Len()
	}
	return look
}

// input is the query as shown and its cursor, or the placeholder.
func (v *View) input() (string, int, bool) {
	text := v.query.Text()
	if text == "" && v.options.Placeholder != "" {
		return v.options.Placeholder, 0, false
	}
	mask := v.options.Password
	if mask == 0 {
		return text, v.query.Cursor(), true
	}
	before, all := graphemes(text[:v.query.Cursor()]), graphemes(text)
	return strings.Repeat(string(mask), all), before * utf8.RuneLen(mask), true
}

// graphemes counts the extended grapheme clusters of s.
func graphemes(s string) int {
	n := 0
	for s != "" {
		s = s[max(grapheme.Next(s), 1):]
		n++
	}
	return n
}
