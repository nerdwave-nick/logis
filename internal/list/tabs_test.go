package list

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/input"
)

// What plain logis's tabbed launcher asks of a list (feat/logis-go
// 8cba2fc9): the other tab's query set to the one typed, and how many
// items match it.

func TestSetQueryFiltersAsTypingItWould(t *testing.T) {
	o := DefaultOptions()
	o.Lines = lines(3)
	v := viewOf(o, "kitty", "keyboard-layout", "firefox", "theme")
	v.Apply(input.ActNext)
	eq(t, v.SetQuery("ke"), true)
	eq(t, v.Query(), "ke")
	eq(t, rowTexts(v.Look()), []string{"keyboard-layout"})
	selected(t, v, 1) // the selection starts again at the first match
	eq(t, v.Matched(), 1)
	// The cursor stands at the end, so typing goes on from there.
	typed(v, "y")
	eq(t, v.Query(), "key")
	eq(t, v.SetQuery("key"), false) // the same query changes nothing
	eq(t, v.SetQuery(""), true)
	eq(t, v.Matched(), 4)
	eq(t, v.Look().Input, "")
}

func TestMatchedCountsTheItemsTheQueryMatches(t *testing.T) {
	v := viewOf(DefaultOptions(), "alpha", "beta", "gamma")
	eq(t, v.Matched(), 3)
	typed(v, "a")
	eq(t, v.Matched(), 3)
	typed(v, "lp")
	eq(t, v.Matched(), 1)
	typed(v, "z")
	eq(t, v.Matched(), 0)
	eq(t, v.Total(), 3)
}
