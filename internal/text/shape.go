package text

// Glyph is one positioned glyph of a line, in buffer pixels: X is its
// origin's distance from the line's start (offsets included), Y its
// offset below the baseline. Glyphs are in visual order.
type Glyph struct {
	Font    FontID
	ID      uint16
	X, Y    float32
	Advance float32
	// Cluster and End are the byte range of the text the glyph shows; an
	// ellipsis glyph has an empty range at the cut.
	Cluster, End int
	// Level is the glyph's bidi embedding level (odd: right to left).
	Level int8
	// Ellipsis marks the "…" that ends a cut line.
	Ellipsis bool
}

// Line is a shaped line: its glyphs, its width (the sum of their
// whole-pixel advances), LayoutWidth (the unrounded width cosmic-text lays
// the line out and measures it with), the largest ascent and descent of
// its glyphs' fonts, in pixels, and whether its paragraph is right to
// left.
type Line struct {
	Glyphs                 []Glyph
	Width, Ascent, Descent float32
	LayoutWidth            float32
	RTL                    bool
}

// FaceSpan sets bytes Start..End of a line in another face (emphasis).
type FaceSpan struct {
	Start, End int
	Face       int
}

// LineOptions say how to set a line: its face (an index as for Face),
// letter spacing in ems, other faces for some of its bytes (ascending, not
// overlapping), and a column Width in pixels the line is confined to,
// ended with "…" when it is too wide if Ellipsis, ending at the column's
// end edge if AlignEnd.
type LineOptions struct {
	Face     int
	Tracking float32
	Faces    []FaceSpan
	Column   bool
	Width    float32
	Ellipsis bool
	AlignEnd bool
}

// Shape sets s on one line in face at size pixels per em with tracking
// ems of letter spacing (Layout without a column or other faces).
func (f *Fonts) Shape(s string, face int, size, tracking float32) Line {
	return f.Layout(s, size, LineOptions{Face: face, Tracking: tracking})
}
