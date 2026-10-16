package text_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/text"
)

// cosmicCase is one line cosmic-text laid out: testdata/cosmic.json,
// written by testdata/cosmic-oracle, the standalone Rust program that sets
// lines with cosmic-text 0.19.0 as Rust logis v1 did. To regenerate (with
// cargo, the crates in its cache): cd testdata/cosmic-oracle &&
// cargo run --release --offline --locked > ../cosmic.json
type cosmicCase struct {
	Set          string   `json:"set"`
	Scale        float32  `json:"scale"`
	Text         string   `json:"text"`
	Size         float32  `json:"size"`
	LineHeight   float32  `json:"line_height"`
	Face         int      `json:"face"`
	Tracking     float32  `json:"tracking"`
	AlignEnd     bool     `json:"align_end"`
	Ellipsis     bool     `json:"ellipsis"`
	Width        *float32 `json:"width"`
	Spans        [][2]int `json:"spans"`
	Emphasis     [][2]int `json:"emphasis"`
	EmphasisFace *int     `json:"emphasis_face"`
	Runs         []struct {
		RTL    bool          `json:"rtl"`
		LineY  float32       `json:"line_y"`
		LineW  float32       `json:"line_w"`
		Glyphs [][10]float32 `json:"glyphs"` // font, id, x, y, w, x offset, y offset, start, end, level
	} `json:"runs"`
}

type cosmicData struct {
	Sets []struct {
		Name  string   `json:"name"`
		Files []string `json:"files"`
	} `json:"sets"`
	Cases []cosmicCase `json:"cases"`
}

// divergences are lines where Go deliberately differs from cosmic-text,
// with the reason.
var divergences = map[string]string{
	// An RTL paragraph that starts with an LTR-level run (digits): cosmic-
	// text 0.19's unwrapped layout gives its first span, being incongruent
	// with the line, an empty word range, so "123" is never drawn and
	// the line is measured without it. Go lays the span out like any other.
	"123 שלום": "cosmic-text drops the leading incongruent span",
	// A match at the start of an ASCII word (feat/logis-go G20): cosmic-
	// text 0.19's ShapeWord fast path shapes a simple ASCII word in the
	// attributes of its first byte when every span set on the word agrees
	// with them, and set_rich_text sets no span for pieces equal to the
	// line's defaults; so the whole word ("of", "off") takes the emphasis
	// face while a match inside a word ("proof") does not. Go sets only the
	// matched letters in the emphasis face, as Rust's list rows describe
	// matches (colours were per letter in both).
	"The tongue of the keys": "cosmic-text sets a word-start match's whole word in its face",
	"off":                    "cosmic-text sets a word-start match's whole word in its face",
}

// The Go line layout equals cosmic-text 0.19's on every oracle case:
// fonts, glyphs, pen positions and whole-pixel advances, byte ranges,
// direction, layout width and baseline.
func TestLinesAreLaidOutLikeCosmicText(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cosmic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d cosmicData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	fonts := map[string]*text.Fonts{}
	for _, set := range d.Sets {
		f := pinned(t, "sans-serif")
		if set.Name == "forge" {
			if err := f.SetFaces(forgeFaces()); err != nil {
				t.Fatal(err)
			}
		}
		f.Ensure("機🔥שم") // the oracle's load order
		var names []string
		for _, file := range f.Loaded() {
			names = append(names, filepath.Base(file.Path))
		}
		if !slices.Equal(names, set.Files) {
			t.Fatalf("%s: loaded %v, the oracle %v", set.Name, names, set.Files)
		}
		fonts[set.Name] = f
	}
	failed := 0
	for _, c := range d.Cases {
		f := fonts[c.Set]
		opt := text.LineOptions{Face: c.Face, Tracking: c.Tracking, AlignEnd: c.AlignEnd}
		if c.Width != nil {
			opt.Column, opt.Width, opt.Ellipsis = true, *c.Width, c.Ellipsis
		}
		opt.Faces = emphasisFaces(c)
		line := f.Layout(strings.NewReplacer("\n", " ", "\r", " ").Replace(c.Text), c.Size, opt)
		got, want := describeLine(line, c), describeCosmic(c)
		if reason, ok := divergences[c.Text]; ok {
			if got == want {
				t.Errorf("%q no longer diverges (%s)", c.Text, reason)
			}
			continue
		}
		if got != want {
			failed++
			if failed <= 12 {
				t.Errorf("%s ×%g %q width %v:\n  got %s\n want %s", c.Set, c.Scale, c.Text, c.Width, got, want)
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d lines differ from cosmic-text", failed, len(d.Cases))
	}
}

// emphasisFaces are the oracle's emphasis ranges in their face.
func emphasisFaces(c cosmicCase) []text.FaceSpan {
	if c.EmphasisFace == nil {
		return nil
	}
	var out []text.FaceSpan
	for _, e := range c.Emphasis {
		out = append(out, text.FaceSpan{Start: e[0], End: e[1], Face: *c.EmphasisFace})
	}
	return out
}

func r3(v float32) string { return fmt.Sprintf("%.3f", math.Round(float64(v)*1000)/1000+0) }

func describeLine(l text.Line, c cosmicCase) string {
	var b strings.Builder
	lineY := float32(0)
	if len(l.Glyphs) > 0 || l.Ascent > 0 {
		lineY = (c.LineHeight-(l.Ascent+l.Descent))/2 + l.Ascent
	} else {
		lineY = c.LineHeight / 2
	}
	fmt.Fprintf(&b, "rtl=%v w=%s y=%s:", l.RTL, r3(l.LayoutWidth), r3(lineY))
	for _, g := range l.Glyphs {
		fmt.Fprintf(&b, " %d/%d@%s,%s+%s[%d,%d]", g.Font, g.ID, r3(g.X+g.XOffset), r3(g.Y), r3(g.Advance), g.Cluster, g.End)
	}
	return b.String()
}

func describeCosmic(c cosmicCase) string {
	var b strings.Builder
	if len(c.Runs) != 1 {
		return fmt.Sprintf("%d runs", len(c.Runs))
	}
	r := c.Runs[0]
	fmt.Fprintf(&b, "rtl=%v w=%s y=%s:", r.RTL, r3(r.LineW), r3(r.LineY))
	for _, g := range r.Glyphs {
		x := g[2] + c.Size*g[5]
		y := g[3] - c.Size*g[6]
		fmt.Fprintf(&b, " %d/%d@%s,%s+%s[%d,%d]", int(g[0]), int(g[1]), r3(x), r3(y), r3(g[4]), int(g[7]), int(g[8]))
	}
	return b.String()
}

// cosmic-text drops the digits of "123 שלום" (see divergences); Go keeps
// them: read first, they stand at the right end of the right-to-left line,
// themselves left to right.
func TestALeadingNumberInARightToLeftLineIsKept(t *testing.T) {
	f := pinned(t, "sans-serif")
	line := f.Layout("123 שלום", 16, text.LineOptions{})
	if !line.RTL {
		t.Fatal("a Hebrew line is right to left")
	}
	digits := map[int]float32{}
	var hebrewRight float32
	for _, g := range line.Glyphs {
		switch {
		case g.Cluster < 3:
			digits[g.Cluster] = g.X
		case g.Cluster >= 4:
			hebrewRight = max(hebrewRight, g.X+g.Advance)
		}
	}
	if len(digits) != 3 || !(hebrewRight < digits[0] && digits[0] < digits[1] && digits[1] < digits[2]) {
		t.Fatalf("digits at %v, Hebrew up to %g: %+v", digits, hebrewRight, line.Glyphs)
	}
	if line.LayoutWidth < 50 {
		t.Fatalf("the line is measured without its digits: %g", line.LayoutWidth)
	}
}

// A match at a word's start sets only its own letters in the emphasis face
// (cosmic-text sets the whole word: see divergences).
func TestAnEmphasisAtAWordsStartKeepsToItsLetters(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces(forgeFaces()); err != nil {
		t.Fatal(err)
	}
	plain := f.Layout("of", 16, text.LineOptions{})
	line := f.Layout("of", 16, text.LineOptions{Faces: []text.FaceSpan{{Start: 0, End: 1, Face: 2}}})
	if len(line.Glyphs) != 2 || len(plain.Glyphs) != 2 {
		t.Fatalf("glyphs %v / %v", line.Glyphs, plain.Glyphs)
	}
	if line.Glyphs[0].Font == plain.Glyphs[0].Font {
		t.Errorf("the matched o is in the row's font %d", line.Glyphs[0].Font)
	}
	if line.Glyphs[1].Font != plain.Glyphs[1].Font {
		t.Errorf("the f is in font %d, want the row's %d", line.Glyphs[1].Font, plain.Glyphs[1].Font)
	}
}
