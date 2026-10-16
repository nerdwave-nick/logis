// Package effects is the controls of logis's post-processing passes as a
// theme sets them and the renderer draws them: the CRT pass (the clean-room
// crt.frag in internal/render) in seven groups of named controls
// (feat/logis-go cd816303). A strength of 0 turns its effect off; every
// other control has a default (Defaults), so a theme may give strengths
// alone.
package effects

// MaskKind is the dot mask's structure.
type MaskKind int

const (
	// MaskDots is a soft round dot grid, the same for every channel.
	MaskDots MaskKind = iota
	// MaskGrille is vertical red, green and blue stripes.
	MaskGrille
	// MaskSlot is those stripes broken into slots, offset like brickwork.
	MaskSlot
)

// MaskWords are the mask kinds' names, in MaskKind order.
var MaskWords = []string{"dots", "grille", "slot"}

// Scanlines are dark lines across the glass. Lengths are logical pixels.
type Scanlines struct {
	// Strength is how dark the lines get; Period the spacing from line to
	// line; Sharpness from a soft wave (0) through the default profile
	// (0.25) to a hard line one pixel thick (1); Roll the drift in px/s
	// (down when positive).
	Strength, Period, Sharpness, Roll float32
}

// Glow is light from bright parts spreading to their surroundings
// (bloom).
type Glow struct {
	// Strength is the bloom's; Threshold the brightness below which nothing
	// glows; Reach the outer sampling ring's radius (px); Bleed how much of
	// the source colour the light keeps (0 grey, 1 its own, above 1
	// saturated); DimBright how much more dim pixels take than bright ones.
	Strength, Threshold, Reach, Bleed, DimBright float32
}

// Aberration is red and blue sampled either side of green.
type Aberration struct {
	// Amount is the offset scale (2 px at 1); EdgesOnly blends towards none
	// at the centre growing outwards; Wobble how much it varies over time,
	// at WobbleRate Hz; Twitch from smooth breathing (0) to calm with
	// spikes (1).
	Amount, EdgesOnly, Wobble, WobbleRate, Twitch float32
}

// Mask is the phosphor structure over the picture.
type Mask struct {
	// Strength is how much it darkens; Size its pitch (px).
	Strength, Size float32
	Kind           MaskKind
}

// Roundness is the curved tube face.
type Roundness struct {
	// Curvature bends the whole panel; Corners rounds the visible glass
	// further; Vignette darkens towards its corners.
	Curvature, Corners, Vignette float32
}

// Grain is film grain.
type Grain struct {
	// Strength of the noise; Rate how often it changes (Hz, 0 frozen).
	Strength, Rate float32
}

// Flicker is the brightness trembling.
type Flicker struct {
	// Strength of the tremble; Frequency its main rate (Hz).
	Strength, Frequency float32
}

// CRT is every control of the CRT pass.
type CRT struct {
	Scanlines  Scanlines
	Glow       Glow
	Aberration Aberration
	Mask       Mask
	Roundness  Roundness
	Grain      Grain
	Flicker    Flicker
}

// Defaults is every control at its default: each effect off, the rest as
// the clean room's table gives them.
func Defaults() CRT {
	var c CRT
	for _, k := range Controls {
		k.Set(&c, k.Default)
	}
	return c
}

// Any reports whether any effect is on, so the pass runs at all.
func (c CRT) Any() bool {
	for _, s := range []float32{
		c.Scanlines.Strength, c.Glow.Strength, c.Aberration.Amount, c.Mask.Strength,
		c.Roundness.Curvature, c.Roundness.Corners, c.Roundness.Vignette, c.Grain.Strength, c.Flicker.Strength,
	} {
		if s > 0 {
			return true
		}
	}
	return false
}

// Moves reports whether the pass changes over time on its own: rolling
// scanlines, a wobbling aberration, flicker or changing grain.
func (c CRT) Moves() bool {
	return c.Scanlines.Strength > 0 && c.Scanlines.Roll != 0 ||
		c.Aberration.Amount > 0 && c.Aberration.Wobble > 0 ||
		c.Flicker.Strength > 0 ||
		c.Grain.Strength > 0 && c.Grain.Rate > 0
}

// Stilled is c with everything that moves stilled (reduce-motion): the
// scanlines stop rolling, the aberration steady, the grain frozen, no
// flicker. The static look stays.
func (c CRT) Stilled() CRT {
	c.Scanlines.Roll = 0
	c.Aberration.Wobble = 0
	c.Grain.Rate = 0
	c.Flicker.Strength = 0
	return c
}
