package xkb

import "fmt"

// Limits on what a keymap may hold. A compositor sends the keymap it
// compiled itself, so real keymaps sit far below every limit (the owner's
// live niri keymap: 35.6 KB, 10 types, 54 interprets, 487 keys; a
// name-serialised two-layout keymap: 76.5 KB, 27 types, 124 interprets).
// They exist because the client trusts nothing it parses: a broken or
// hostile keymap must fail quickly and in bounded memory, and the
// keyboard then keeps its last good keymap.
const (
	// MaxKeymapSize is the largest keymap text accepted, in bytes.
	MaxKeymapSize = 2 << 20
	// maxDepth bounds the nesting of blocks and expressions.
	maxDepth = 32
	// maxLevel is the highest shift level (libxkbcommon's
	// XKB_LEVEL_MAX_IMPL).
	maxLevel = 2048
	// maxGroups is the number of layouts text v1 can name.
	maxGroups = 32
	// maxTypes, maxTypeEntries and maxInterprets bound the work of
	// compiling keys against them.
	maxTypes       = 1024
	maxTypeEntries = 256
	maxInterprets  = 4096
	// maxWork bounds the interprets tried against keysyms while compiling.
	maxWork = 1 << 20
	// maxMods is the width of a modifier mask.
	maxMods = 32
)

// Error is a keymap that cannot be used: malformed text, or a limit
// exceeded.
type Error struct {
	Line   int // 0: not tied to a line
	Reason string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("keymap line %d: %s", e.Line, e.Reason)
	}
	return "keymap: " + e.Reason
}

func errorf(line int, format string, args ...any) *Error {
	return &Error{Line: line, Reason: fmt.Sprintf(format, args...)}
}
