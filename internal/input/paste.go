package input

import (
	"strings"
	"unicode/utf8"
)

// Paste (spec feat/logis §4.5): which selection, which of its offered MIME
// types to read, and what of the bytes read reaches the query. The window
// reads the selection's pipe without blocking its frame loop and hands the
// bytes here once the owner closes it (or the limit is reached); the text
// then becomes an ordinary Insert edit at the cursor.

// Selection is which selection a paste reads.
type Selection int

const (
	// Clipboard (Ctrl+V, Shift+Insert): wl_data_device.
	Clipboard Selection = iota + 1
	// Primary is the primary selection (the middle button):
	// zwp_primary_selection_device_v1.
	Primary
)

// Name is the selection's name in the test trace.
func (s Selection) Name() string {
	if s == Primary {
		return "primary"
	}
	return "clipboard"
}

// textMIME are the text MIME types logis reads, best first: UTF-8
// declared, then the X11 names for UTF-8, then text without a charset
// (read as UTF-8 too).
var textMIME = []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "TEXT", "STRING"}

// PasteLimit is the most bytes a paste reads; the query is one line.
const PasteLimit = 64 * 1024

// ChooseMIME is the offered MIME type to read, as the owner spelled it:
// the best text type on offer. The charset parameter and its value are
// compared without regard to case or spaces (`text/plain; charset=UTF-8`
// is UTF-8 too).
func ChooseMIME(offered []string) (string, bool) {
	normal := func(m string) string { return strings.ReplaceAll(strings.ToLower(m), " ", "") }
	for _, want := range textMIME {
		for _, m := range offered {
			if normal(m) == strings.ToLower(want) {
				return m, true
			}
		}
	}
	return "", false
}

// PasteBytes is the bytes of a paste in progress, at most PasteLimit.
type PasteBytes struct{ bytes []byte }

// Push adds what was read; it reports whether the limit is reached (stop
// reading).
func (b *PasteBytes) Push(chunk []byte) bool {
	room := PasteLimit - len(b.bytes)
	b.bytes = append(b.bytes, chunk[:min(len(chunk), room)]...)
	return len(b.bytes) >= PasteLimit
}

// Text is the text this paste inserts (see PasteText).
func (b *PasteBytes) Text() string { return PasteText(b.bytes) }

// PasteText is what pasted bytes insert into the one-line query: the first
// line that is not empty, decoded as UTF-8 (invalid bytes become U+FFFD, a
// sequence cut off by the limit is dropped), tabs as spaces and no other
// control characters. This is dmenu's rule (a paste ends at its first
// newline), except that leading line breaks are skipped.
func PasteText(b []byte) string {
	text := lossy(b)
	text = strings.TrimLeft(text, "\n\r")
	if i := strings.IndexAny(text, "\n\r"); i >= 0 {
		text = text[:i]
	}
	var out strings.Builder
	for _, r := range text {
		switch {
		case r == '\t':
			out.WriteByte(' ')
		case isControl(r):
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// lossy decodes b as UTF-8, after dropping a sequence cut off at the end
// when that is the first error: each maximal invalid subpart becomes one
// U+FFFD (Unicode's recommended substitution; Go's own conversions
// replace per byte).
func lossy(b []byte) string {
	var out strings.Builder
	first := true
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r != utf8.RuneError || size > 1 {
			out.Write(b[:size])
			b = b[size:]
			continue
		}
		n, cut := subpart(b)
		if cut && first {
			break // cut off by the limit: dropped
		}
		first = false
		out.WriteRune(utf8.RuneError)
		b = b[n:]
	}
	return out.String()
}

// subpart is the length of the maximal invalid subpart at the start of b
// (at least 1), and whether it is a valid sequence's prefix cut off by
// the end of b.
func subpart(b []byte) (n int, cut bool) {
	lo, hi, need := byte(0x80), byte(0xbf), 0
	switch c := b[0]; {
	case c >= 0xc2 && c <= 0xdf:
		need = 1
	case c == 0xe0:
		lo, need = 0xa0, 2
	case c == 0xed:
		hi, need = 0x9f, 2
	case c >= 0xe1 && c <= 0xef:
		need = 2
	case c == 0xf0:
		lo, need = 0x90, 3
	case c == 0xf4:
		hi, need = 0x8f, 3
	case c >= 0xf1 && c <= 0xf3:
		need = 3
	default:
		return 1, false
	}
	n = 1
	for ; n <= need; n++ {
		if n == len(b) {
			return n, true
		}
		if c := b[n]; c < lo || c > hi {
			return n, false
		}
		lo, hi = 0x80, 0xbf
	}
	return n, false // a whole valid sequence; DecodeRune took it
}
