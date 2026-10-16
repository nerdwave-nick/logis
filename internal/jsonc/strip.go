// Package jsonc reads JSONC as VS Code and servitor (hujson) do: JSON plus
// `//` and `/* */` comments and trailing commas. Strip blanks those out and
// leaves standard JSON for encoding/json; Parse reads one JSONC document into
// values that remember where they stand, so that errors point into the file
// the user wrote by line and character column.
package jsonc

import (
	"bytes"
	"fmt"
)

// UnterminatedError is a block comment that never closes; Line and Column
// (1-based, the column in bytes) are where it begins.
type UnterminatedError struct {
	Line, Column int
}

func (e *UnterminatedError) Error() string {
	return fmt.Sprintf("%d:%d: this comment never closes: '/*' wants its '*/'", e.Line, e.Column)
}

// bom is the UTF-8 byte order mark, blanked like a comment.
var bom = []byte("\xef\xbb\xbf")

// Strip returns a copy of text with comments, a leading byte order mark and
// trailing commas replaced by spaces. Every byte keeps its offset: newlines
// stay where they were, so a decoder's offsets point into text. Only whole
// UTF-8 sequences are blanked (inside comments, or the mark), so valid UTF-8
// stays valid. Anything else malformed is left for the decoder to refuse.
func Strip(text []byte) ([]byte, error) {
	out := bytes.Clone(text)
	i := 0
	if bytes.HasPrefix(text, bom) {
		i = len(bom)
		blank(out[:i])
	}
	// A comma seen since the last value; blanked if a closing bracket follows.
	comma := -1
	for i < len(text) {
		switch c := text[i]; {
		case c == '"':
			comma = -1
			i = stringEnd(text, i+1)
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			end := len(text)
			if n := bytes.IndexByte(text[i:], '\n'); n >= 0 {
				end = i + n
			}
			blank(out[i:end])
			i = end
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			n := bytes.Index(text[i+2:], []byte("*/"))
			if n < 0 {
				line, column := locate(text, i)
				return nil, &UnterminatedError{line, column}
			}
			end := i + 2 + n + 2
			blank(out[i:end])
			i = end
		case c == ',':
			comma = i
			i++
		case c == '}' || c == ']':
			if comma >= 0 {
				out[comma] = ' '
				comma = -1
			}
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
		default:
			comma = -1
			i++
		}
	}
	return out, nil
}

// stringEnd is the offset just past the string whose text starts at i.
func stringEnd(text []byte, i int) int {
	for i < len(text) {
		switch text[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1
		default:
			i++
		}
	}
	return len(text)
}

// blank turns everything but newlines into spaces.
func blank(b []byte) {
	for i, c := range b {
		if c != '\n' {
			b[i] = ' '
		}
	}
}

// locate is the 1-based line and byte column of offset in text.
func locate(text []byte, offset int) (line, column int) {
	before := text[:offset]
	start := bytes.LastIndexByte(before, '\n') + 1
	return bytes.Count(before, []byte("\n")) + 1, offset - start + 1
}
