// Package list is the pure logic of the list a session shows (spec §3
// stories 1–4, §4.5): its items, the query filtering them with logis's
// fzf-like matcher (internal/fuzzy), and what of each row to highlight.
// Drawing is not here.
package list

import "strings"

// Entry is one item of a list.
type Entry struct {
	// Text is shown; its tab-separated columns line up with other rows'.
	Text string
	// MatchText is matched instead of Text when HasMatchText (--match-nth,
	// or a provider's match).
	MatchText    string
	HasMatchText bool
	// Decor is what a provider adds; nil for a plain dmenu line, which
	// keeps 100 000 lines small.
	Decor *Decor
}

// Decor is a provider's additions to an item.
type Decor struct {
	// Glyph is a short text drawn in the icon column (an emblem such as
	// ⚙); empty for none.
	Glyph string
	// Icon is a freedesktop icon name or an absolute path, drawn in the
	// icon column in place of the glyph; empty for none.
	Icon string
	// Badges are short labels drawn at the row's end, such as "current".
	Badges []string
	// Pending: the item's data has not settled; it is shown as pending,
	// never accepted and never preselected.
	Pending bool
	// Boost is how much higher its matches score (an app's frecency).
	Boost uint32
}

// Matched is the text the item is matched by.
func (e Entry) Matched() string {
	if e.HasMatchText {
		return e.MatchText
	}
	return e.Text
}

// IsPending reports whether the item is pending.
func (e Entry) IsPending() bool { return e.Decor != nil && e.Decor.Pending }

// HasEmblem reports whether the item has something for the icon column.
func (e Entry) HasEmblem() bool {
	return e.Decor != nil && (e.Decor.Glyph != "" || e.Decor.Icon != "")
}

// Highlightable reports whether matches show on the shown text: it is what
// is matched, or the match text begins with it (matches beyond it are then
// not shown).
func (e Entry) Highlightable() bool {
	return !e.HasMatchText || strings.HasPrefix(e.MatchText, e.Text)
}
