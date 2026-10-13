package bidi

import xbidi "golang.org/x/text/unicode/bidi"

// Class is a Unicode bidi class, numbered as golang.org/x/text/unicode/bidi
// numbers them; its tables (Unicode 17) give each rune's.
type Class xbidi.Class

// The bidi classes the forked core refers to.
const (
	L       = Class(xbidi.L)
	R       = Class(xbidi.R)
	EN      = Class(xbidi.EN)
	ES      = Class(xbidi.ES)
	ET      = Class(xbidi.ET)
	AN      = Class(xbidi.AN)
	CS      = Class(xbidi.CS)
	B       = Class(xbidi.B)
	S       = Class(xbidi.S)
	WS      = Class(xbidi.WS)
	ON      = Class(xbidi.ON)
	BN      = Class(xbidi.BN)
	NSM     = Class(xbidi.NSM)
	AL      = Class(xbidi.AL)
	Control = Class(xbidi.Control)
	LRO     = Class(xbidi.LRO)
	RLO     = Class(xbidi.RLO)
	LRE     = Class(xbidi.LRE)
	RLE     = Class(xbidi.RLE)
	PDF     = Class(xbidi.PDF)
	LRI     = Class(xbidi.LRI)
	RLI     = Class(xbidi.RLI)
	FSI     = Class(xbidi.FSI)
	PDI     = Class(xbidi.PDI)

	unknownClass = ^Class(0)
)
