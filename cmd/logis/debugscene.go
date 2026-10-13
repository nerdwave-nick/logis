package main

import (
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// debugMargin is the room around the debug panel's body for its glow.
const debugMargin = 24

// debugSize is the surface the debug panel requests: Rust's 640×360 body
// plus the glow margin on every side (DEBUG_SIZE).
var debugSize = scale.Size{W: 640 + 2*debugMargin, H: 360 + 2*debugMargin}

// debugPanelBody is the panel body in logical pixels.
var debugPanelBody = [2]float32{640, 360}

// Forge crimson & brass (Rust src/window/debug/mod.rs).
var (
	brass      = render.RGBA(0.83, 0.63, 0.09, 1)
	crimson    = render.RGBA(0.70, 0.06, 0.05, 1)
	sootTop    = render.RGBA(0.085, 0.055, 0.045, 0.86)
	sootBottom = render.RGBA(0.045, 0.030, 0.026, 0.86)
	ash        = render.RGBA(0.54, 0.48, 0.40, 0.28)
	bone       = render.Hex(0xeadfc4)
)

// debugRows are the panel's rows: every script and feature the text
// slices cover, the eighth too long for its column (Rust ROWS).
var debugRows = [...]string{
	"The machine spirit wakes: 0123456789",
	"Ça déjà vu, naïve Ærø — Œuvre, Šťastný",
	"機械の精霊 万机之神 기계 정령",
	"logis: שָׁלוֹם and مرحبا بالعالم (42) ok",
	"Praise 🔥⚙\ufe0f💀🤖 the Omnissiah 👍🏽 🧑\u200d💻",
	"fi fl ffi — ligatures, kerning: AVATAR Tolkien",
	"Ἐν ἀρχῇ ἦν ὁ λόγος · Съешь же ещё",
	"Ellipsis rite: this line is far too long for its column, so the cogitator cuts it short and seals it",
	"Scale is taken from the compositor (1.0, 1.3, 2.0)",
}

// queryBlock is the room at the bottom of the body for the query rule and
// line.
const queryBlock = 44

// debugScene is the debug panel's scene for a surface of logical size, as
// Rust's debug_scene draws its shapes: a soot body with a brass-to-crimson
// border and a crimson glow, the title rune, a rule, the selected row's
// band and marker, and the query rule; then its title, the rows that fit
// above the query rule and the query line. The body fills the surface up
// to 640×360, less the glow margin, so a smaller configure than requested
// still shows the whole panel. Rust confines the rows to a column with an
// ellipsis; columns come with the shaping slice, so until then the long
// row runs past the body.
func debugScene(logical scale.Size) *render.Scene {
	fit := func(side uint32, most float32) float32 {
		return min(max(float32(side)-2*debugMargin, 64), most)
	}
	pw, ph := fit(logical.W, debugPanelBody[0]), fit(logical.H, debugPanelBody[1])
	x, y := (float32(logical.W)-pw)/2, (float32(logical.H)-ph)/2
	rect := func(dx, dy, w, h, radius float32, fill render.Paint) render.RoundedRect {
		return render.RoundedRect{Rect: [4]float32{x + dx, y + dy, w, h}, Radius: radius, Fill: fill}
	}
	vertical := func(from, to render.Color) render.Paint {
		return render.Linear([2]float32{0, y}, [2]float32{0, y + ph}, from, to)
	}

	body := rect(0, 0, pw, ph, 14, vertical(sootTop, sootBottom))
	body.Border = render.Border{Width: 2, Paint: vertical(brass, crimson)}
	body.Glow = render.Glow{Radius: debugMargin - 2, Color: crimson.WithAlpha(0.55)}
	rune := rect(20, 18, 16, 16, 3, render.Solid(brass))
	rule := rect(18, 52, pw-36, 1.5, 0.75, render.Solid(brass.WithAlpha(0.8)))
	selected := rect(2, 66, pw-4, 26, 2, render.Linear([2]float32{x, 0}, [2]float32{x + pw, 0},
		render.RGBA(0.30, 0.07, 0.05, 0.9), render.RGBA(0.18, 0.05, 0.04, 0.6)))
	selected.Border = render.Border{Width: 1, Paint: render.Solid(crimson.WithAlpha(0.6))}
	marker := rect(10, 70, 3, 18, 1.5, render.Solid(brass))
	marker.Glow = render.Glow{Radius: 6, Color: brass.WithAlpha(0.7)}
	queryRule := rect(18, ph-queryBlock, pw-36, 1.5, 0.75, render.Solid(brass.WithAlpha(0.8)))
	texts := []render.Text{render.NewText("COGITATOR // RITE OF TESTING", [2]float32{x + 46, y + 12}, 18, brass)}
	for i, row := range debugRows {
		dy := 68 + debugRowPitch*float32(i)
		if dy+22 > ph-queryBlock {
			break
		}
		color := ash.WithAlpha(0.9)
		if i == 0 {
			color = bone
		}
		texts = append(texts, render.NewText(row, [2]float32{x + 22, y + dy}, 15, color))
	}
	texts = append(texts, render.NewText("> |", [2]float32{x + 22, y + ph - queryBlock + 10}, 15, brass))
	return &render.Scene{Shapes: []render.RoundedRect{body, rune, rule, selected, marker, queryRule}, Texts: texts}
}

// debugSelectable is the number of rows the selection can rest on (Rust's
// SELECTABLE: the text rows); debugRowPitch is the distance between them.
const (
	debugSelectable = len(debugRows)
	debugRowPitch   = 26
)

// Indices of the selection band and its marker in debugScene's shapes.
const (
	debugSelected = 3
	debugMarker   = 4
)

// debugSceneAt is debugScene with row selected (Rust's debug_scene_at,
// without the query line): the band and its marker move down one row pitch
// per row and stop at the last row above the query block.
func debugSceneAt(logical scale.Size, row int) *render.Scene {
	sc := debugScene(logical)
	body, band := sc.Shapes[0].Rect, sc.Shapes[debugSelected].Rect
	room := body[1] + body[3] - queryBlock - (band[1] + band[3])
	fits := max(int(room/debugRowPitch), 0)
	dy := debugRowPitch * float32(min(row, fits))
	for _, i := range []int{debugSelected, debugMarker} {
		sc.Shapes[i].Rect[1] += dy
	}
	return sc
}
