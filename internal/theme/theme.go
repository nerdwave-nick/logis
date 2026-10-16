// Package theme is a theme resolved for drawing (feat/logis §4.9): every
// colour a render.Color, every font a face and a size, every part that may
// be off a flag. Lengths are logical pixels; the renderer multiplies by the
// output scale.
//
// The engine (feat/logis §4.9, the theme model of three layers): a theme
// file is a palette of named colours with alpha, the elements of the
// cogitator, which refer to palette names, and effects and motion. A theme
// is themes/<name>.jsonc beside the config, or one of the bundled themes
// compiled in (package themes); a user file overrides a bundled theme of
// the same name. It may "extends" one other theme by name; whatever it does
// not give comes from the bundled default (DefaultName). Its JSON Schema is
// schema/theme.schema.json. Themes.Load reads one into this model; Live
// keeps the one in force while the daemon runs.
package theme

import (
	"time"

	"github.com/nerdwave-nick/logis/internal/custom"
	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
)

// Font is a font as an element uses it.
type Font struct {
	// Face indexes Theme.Faces.
	Face int
	// Size is the em in logical pixels.
	Size float32
	// Tracking is letter spacing in ems.
	Tracking float32
}

// Theme is every look a cogitator can wear, resolved.
type Theme struct {
	// Name is forge-crimson-brass, or a user file's stem.
	Name string
	// Dir is where its relative image paths start: the user file's
	// directory; "" for a bundled theme.
	Dir string
	// Faces are the faces its fonts use, each once; the first is the body's.
	Faces     []text.Face
	Window    Window
	Glow      Glow
	Border    Border
	Panel     Panel
	HasRivets bool
	Rivets    Rivets
	Prompt    Prompt
	Query     Query
	Counter   Counter
	Rule      Rule
	Row       Row
	Selection Selection
	Match     Match
	Badge     Badge
	// Tab is plain logis's tab plates' (8cba2fc9).
	Tab     Tab
	Pending Pending
	Effects Effects
	// Glitch is the glitch pass's controls and bursts (effects.glitch).
	Glitch effects.Glitch
	// Custom is the custom pass's shader (effects.custom-shader, a path
	// from the theme's file; the config's custom-shader outranks it).
	Custom custom.Shader
	Motion Motion
	// Error is the palette's error, for error toasts.
	Error render.Color
}

// Window is the window's default width and how much list it shows; its
// height follows the rows (scene.FitIn).
type Window struct {
	// Width is the panel's (its border box) when no --width is given;
	// Rows how many rows it shows when no --lines is given.
	Width float32
	Rows  uint32
	// Margin is room around the panel for its glow.
	Margin float32
}

// Glow is the glow outside the panel.
type Glow struct {
	Color  render.Color
	Radius float32
	// Pulse is how far its strength swings, 0 (steady) to 1, once per
	// Period.
	Pulse  float32
	Period time.Duration
}

// Border is the panel's edge: its paint top to bottom, evenly spaced, and
// a highlight travelling around it when HasHighlight.
type Border struct {
	Width        float32
	Paint        []render.Color
	HasHighlight bool
	Highlight    Highlight
}

// Highlight is a bright spot travelling around the border.
type Highlight struct {
	Color render.Color
	// Length is its reach along the border, either side of its centre.
	Length float32
	// Period is one round of the border.
	Period time.Duration
}

// Panel is the panel's body.
type Panel struct {
	// Fill is top to bottom, evenly spaced.
	Fill   []render.Color
	Radius float32
	// Padding is left and right of the prompt line and the rows.
	Padding float32
	// Blur behind the body, when the config allows it and the compositor
	// offers it.
	Blur bool
}

// Tab is the plates of plain logis's tab strip (feat/logis-go 8cba2fc9);
// the rest of their look echoes the selection, the panel, the prompt's
// label and the badges (scene/strip.go).
type Tab struct {
	// Radius is their corners'.
	Radius float32
}

// Rivets are bosses in the body's corners.
type Rivets struct {
	Size float32
	// Inset is from the body's edges.
	Inset float32
	// Top and Bottom are the upper and lower pairs' paint, top-left to
	// bottom-right.
	Top, Bottom []render.Color
}

// Prompt is the prompt line's start.
type Prompt struct {
	// Height is the prompt line's, from the body's top to the rule.
	Height float32
	// Label is shown before the session's prompt, empty for none.
	Label string
	Font  Font
	Color render.Color
	// Gap is between the sigil, the label and the query.
	Gap      float32
	HasSigil bool
	Sigil    Sigil
}

// Sigil is the image before the prompt.
type Sigil struct {
	// Image is "gear" (drawn by logis) or an image file.
	Image string
	Size  float32
	// Color is that of a sigil logis draws.
	Color render.Color
}

// CaretShape is a caret's form.
type CaretShape int

const (
	CaretBar CaretShape = iota
	CaretBlock
	CaretUnderscore
)

// Query is the query.
type Query struct {
	Font        Font
	Color       render.Color
	Placeholder render.Color
	Caret       Caret
}

// Caret is where the next character goes.
type Caret struct {
	Shape CaretShape
	// Width is a bar's (a block or underscore is a character wide).
	Width float32
	Color render.Color
	Glow  render.Color
}

// Counter is the match counter; {matched} and {total} in Format are
// replaced.
type Counter struct {
	Font   Font
	Color  render.Color
	Format string
}

// Rule is the rule under the prompt line.
type Rule struct {
	// Paint is left to right, evenly spaced.
	Paint     []render.Color
	Thickness float32
	// Inset is from the body's left and right edges.
	Inset float32
	// Dash and Gap lengths when Dashed.
	Dashed      bool
	Dash        [2]float32
	HasOrnament bool
	Ornament    Ornament
}

// Ornament is a boss at the rule's middle.
type Ornament struct {
	Size       float32
	Fill, Edge render.Color
}

// Row is a row of the list.
type Row struct {
	Font Font
	// Height is from one row's top to the next; Gap above the first row
	// and below the last; Padding from the body's left edge to the text.
	Height, Gap, Padding float32
	// Columns are per tab-separated column; the last repeats.
	Columns []render.Color
	// Glyph is an emblem's colour.
	Glyph render.Color
	// ColumnGap is between one column's widest text and the next column;
	// ColumnMax the most of the row's width a column other than the last
	// may take (0 to 1).
	ColumnGap, ColumnMax float32
}

// Selection is the selected row.
type Selection struct {
	// Band is left to right, evenly spaced.
	Band []render.Color
	// Outline lines the band; Edge is the bar at its start.
	Outline, Edge, Glow render.Color
	// Columns are the selected row's column colours.
	Columns []render.Color
	// Inset is from the body's left and right edges.
	Inset float32
}

// Match is matched characters.
type Match struct {
	Color render.Color
	// Selected is on the selected row.
	Selected render.Color
	// Face indexes Theme.Faces: the row's families at the match weight.
	Face      int
	Underline bool
}

// BadgeStyle is how a badge is drawn.
type BadgeStyle int

const (
	BadgeOutline BadgeStyle = iota
	BadgeFill
	BadgeBracket
)

// Badge is a badge at a row's end.
type Badge struct {
	Font            Font
	Style           BadgeStyle
	Ink, Edge, Fill render.Color
	Uppercase       bool
}

// Pending is a row whose data has not settled.
type Pending struct {
	Color render.Color
	// Opacity is the whole row's; the mark's swings by Shimmer once per
	// Period.
	Opacity, Shimmer float32
	Period           time.Duration
	// Mark is what the row says at its end.
	Mark string
}

// Effects are the CRT pass's controls over the body: a group per effect,
// each with its strength (0 is off).
type Effects = effects.CRT

// Motion are the transition timings.
type Motion struct {
	Open, Close, Select time.Duration
}

// IsContinuous reports whether the theme moves on its own (glow pulse,
// travelling highlight, and effects that move: rolling scanlines, a
// wobbling aberration, flicker, changing grain, a glitch that moves at
// rest, a custom pass, which may use its time), so a visible window
// keeps drawing. The glitch's episodes and
// bursts move only while they last (internal/glitch). A pending row's shimmer counts only while one is shown
// (Shimmers).
func (t Theme) IsContinuous() bool {
	return t.Glow.Pulse > 0 && t.Glow.Period > 0 ||
		t.Border.HasHighlight && t.Border.Highlight.Period > 0 ||
		t.Effects.Moves() || t.Glitch.Moves() || t.Custom.Set() && !t.Custom.Frozen
}

// Shimmers reports whether a shown pending row shimmers, so the window
// keeps drawing.
func (t Theme) Shimmers() bool { return t.Pending.Shimmer > 0 && t.Pending.Period > 0 }

// Stilled is the theme with every continuous effect stilled (the config's
// reduce-motion): no pulse, no travelling highlight (it rests at the top),
// no flicker, no shimmer; the scanlines stop rolling, the aberration
// steady, the grain frozen (effects.CRT.Stilled); the glitch is off; a
// custom pass runs with its time frozen at 0.
func (t Theme) Stilled() Theme {
	t.Glow.Pulse = 0
	t.Border.Highlight.Period = 0
	t.Effects = t.Effects.Stilled()
	t.Glitch = t.Glitch.Stilled()
	t.Custom.Frozen = true
	t.Pending.Shimmer = 0
	return t
}
