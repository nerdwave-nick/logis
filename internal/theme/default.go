package theme

import (
	"time"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
)

// The palette of forge-crimson-brass (themes/forge-crimson-brass.jsonc).
var (
	soot0     = render.Hex(0x170e0b)
	soot1     = render.Hex(0x0b0807)
	brass     = render.Hex(0xd4a017)
	gold      = render.Hex(0xf2c14e)
	ember     = render.Hex(0xc4651a)
	crimson   = render.Hex(0xb30f0d)
	bone      = render.Hex(0xf1e6cc)
	parchment = render.Hex(0xd8c9ae)
	ash       = render.Hex(0x94836c)
	smoke     = render.Hex(0x6e5f4c)
	umber     = render.Hex(0xc9b894)
	verdigris = render.Hex(0x8a6a14)
	flare     = render.Hex(0xffe296)
	band0     = render.Hex(0x54140e)
	band1     = render.Hex(0x2e0d0a)
	boss      = render.Hex(0x0f0a08)
	errorRed  = render.Hex(0xeb2919)
)

// with is c at alpha times its own (a theme's "name/alpha").
func with(c render.Color, alpha float32) render.Color { return c.WithAlpha(c.A * alpha) }

// Default is the bundled default theme, forge-crimson-brass, resolved as
// Rust's resolver resolves it (faces interned in its order: the body's,
// the row's at the match weight, the label's).
func Default() Theme {
	plex := []string{"IBM Plex Mono", "monospace"}
	body := Font{Face: 0, Size: 16}
	label := Font{Face: 2, Size: 13, Tracking: 0.2}
	return Theme{
		Name: "forge-crimson-brass",
		Faces: []text.Face{
			{Families: plex, Weight: 400},
			{Families: plex, Weight: 600},
			{Families: []string{"Cinzel", "serif"}, Weight: 600},
		},
		Window: Window{Width: 820, Height: 520, Rows: 9, Margin: 32},
		Glow:   Glow{Color: with(crimson, 0.55), Radius: 26, Pulse: 0.15, Period: 3000 * time.Millisecond},
		Border: Border{
			Width: 2, Paint: []render.Color{brass, ember, crimson}, HasHighlight: true,
			Highlight: Highlight{Color: with(flare, 0.9), Length: 70, Period: 7000 * time.Millisecond},
		},
		Panel:     Panel{Fill: []render.Color{with(soot0, 0.9), with(soot1, 0.92)}, Radius: 14, Padding: 26, Blur: true},
		HasRivets: true,
		Rivets: Rivets{
			Size: 6, Inset: 10,
			Top:    []render.Color{render.Hex(0xf2d27a), render.Hex(0x6b4a0c)},
			Bottom: []render.Color{render.Hex(0xc86a4a), render.Hex(0x4a0c08)},
		},
		Prompt: Prompt{
			Height: 54, Label: "INVOKE", Font: label, Color: brass, Gap: 12,
			HasSigil: true, Sigil: Sigil{Image: "gear", Size: 20, Color: brass},
		},
		Query: Query{
			Font: Font{Face: 0, Size: 19}, Color: bone, Placeholder: smoke,
			Caret: Caret{Shape: CaretBar, Width: 2, Color: gold, Glow: with(gold, 0.8)},
		},
		Counter: Counter{Font: Font{Face: 0, Size: 13, Tracking: 0.06}, Color: ash, Format: "{matched}/{total}"},
		Rule: Rule{
			Paint:     []render.Color{with(brass, 0), with(brass, 0.85), with(brass, 0.85), with(brass, 0)},
			Thickness: 1, Inset: 20,
			HasOrnament: true, Ornament: Ornament{Size: 9, Fill: boss, Edge: brass},
		},
		Row: Row{
			Font: body, Height: 38, Gap: 12, Padding: 26,
			Columns: []render.Color{parchment, brass, ash}, Glyph: brass, ColumnGap: 32, ColumnMax: 0.4,
		},
		Selection: Selection{
			Band:    []render.Color{with(band0, 0.92), with(band1, 0.5)},
			Outline: with(crimson, 0.55), Edge: gold, Glow: render.Transparent,
			Columns: []render.Color{bone, gold, umber}, Inset: 2,
		},
		Match: Match{Color: gold, Selected: gold, Face: 1},
		Badge: Badge{
			Font: Font{Face: 0, Size: 11, Tracking: 0.12}, Style: BadgeOutline,
			Ink: brass, Edge: verdigris, Fill: with(soot1, 0.9), Uppercase: true,
		},
		Pending: Pending{Color: ash, Opacity: 0.72, Shimmer: 0.3, Period: 1600 * time.Millisecond, Mark: "divining…"},
		Effects: Effects{Scanlines: 0.16, Grain: 0.08, Flicker: 0.015, Vignette: 0.42},
		Motion:  Motion{Open: 150 * time.Millisecond, Close: 110 * time.Millisecond, Select: 90 * time.Millisecond},
		Error:   errorRed,
	}
}
