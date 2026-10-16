package text_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/text"
)

// BenchmarkLayoutCorpus sets the five corpus lines at 20.8 px (16 px at
// scale 1.3), as a frame whose rows all changed would.
func BenchmarkLayoutCorpus(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fonts", "corpus.txt"))
	if err != nil {
		b.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	f := pinned(b, "sans-serif")
	for _, l := range lines {
		f.Layout(l, 20.8, text.LineOptions{}) // load the fallback faces
	}
	b.ResetTimer()
	for b.Loop() {
		for _, l := range lines {
			f.Layout(l, 20.8, text.LineOptions{})
		}
	}
}

// BenchmarkLayoutRowInAColumn sets one list row with a highlighted match
// in a 400 px column with an ellipsis, as each keystroke reshapes rows.
func BenchmarkLayoutRowInAColumn(b *testing.B) {
	f := pinned(b, "sans-serif")
	opt := text.LineOptions{Column: true, Width: 400, Ellipsis: true, Faces: []text.FaceSpan{{Start: 4, End: 8}}}
	row := "firefox — Web Browser: browse the noosphere"
	f.Layout(row, 20.8, opt)
	b.ResetTimer()
	for b.Loop() {
		f.Layout(row, 20.8, opt)
	}
}
