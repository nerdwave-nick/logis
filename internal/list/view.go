package list

import (
	"strings"

	"github.com/nerdwave-nick/logis/internal/input"
)

// A View is a session's list as the user works it (Rust src/list/view.rs):
// the query, what it matches, the selection and the rows in view. Keys
// arrive as input.Actions; the result is a Reaction: redraw, accept a
// Choice, or dismiss.
//
// Accepting follows fuzzel's dmenu rules: the selected match if any;
// otherwise the typed text, unless OnlyMatch forbids it (then Return does
// nothing). A preselection (--select, --select-index) applies when its item
// arrives, until the user moves or edits.

// ReactionKind is what an action did to a View.
type ReactionKind int

const (
	// Unchanged: nothing visible changed.
	Unchanged ReactionKind = iota
	// Changed: the list must be drawn again.
	Changed
	// Accept: the session ends with Reaction.Choice.
	Accept
	// Dismiss: the session ends without a choice.
	Dismiss
)

// Reaction is what an action did.
type Reaction struct {
	Kind   ReactionKind
	Choice Choice // for Accept
}

// ChoiceKind is what the user chose.
type ChoiceKind int

const (
	// ChoseItem: the item at Index (input order).
	ChoseItem ChoiceKind = iota
	// ChoseDescend: the item at Index, asking for its next stage
	// (Shift+Return).
	ChoseDescend
	// ChoseTyped: the typed Text, which matched nothing.
	ChoseTyped
)

// Choice is what the user chose.
type Choice struct {
	Kind  ChoiceKind
	Index uint32
	Text  string
}

// View is the list of one session. Not safe for concurrent use.
type View struct {
	options Options
	query   input.Query
	filter  *Filter
	texts   []string
	// highlightable: per item, whether matches show on its text.
	highlightable []bool
	// decor: per item, a provider's additions (nil for plain items).
	decor []*Decor
	// removed: per item, whether it was removed.
	removed []bool
	// emblems: how many items have something for the icon column.
	emblems   int
	selected  int
	top       int
	preselect *Preselect
	// follow is the item the user moved the selection to; it stays
	// selected while more items stream in.
	follow    uint32
	following bool
	// touched: the user moved the selection or edited the query; no
	// preselection applies after that.
	touched bool
	// matched is reused by Push.
	matched []Boosted
}

// NewView is an empty list opening with o.Search as its query.
func NewView(o Options) *View {
	v := &View{options: o, filter: NewFilter()}
	v.query.Apply(input.Edit{Kind: input.InsertText, Text: o.Search})
	v.filter.SetQuery(v.query.Text())
	if o.Select != nil {
		p := *o.Select
		v.preselect = &p
	}
	return v
}

// Push adds entries in arrival order and reports whether the rows in view
// may have changed.
func (v *View) Push(entries []Entry) bool {
	if len(entries) == 0 {
		return false
	}
	first := len(v.texts)
	v.matched = v.matched[:0]
	for _, e := range entries {
		if e.HasEmblem() {
			v.emblems++
		}
		v.highlightable = append(v.highlightable, e.Highlightable())
		var boost uint32
		if e.Decor != nil {
			boost = e.Decor.Boost
		}
		v.matched = append(v.matched, Boosted{Text: e.Matched(), Boost: boost})
		v.texts = append(v.texts, e.Text)
		v.decor = append(v.decor, e.Decor)
		v.removed = append(v.removed, false)
	}
	v.filter.PushBoosted(v.matched)
	v.settle(first, len(v.texts))
	return true
}

// Update replaces item index in its place; whether it existed.
func (v *View) Update(index uint32, e Entry) bool {
	if !v.exists(index) {
		return false
	}
	i := int(index)
	if v.emblem(i) {
		v.emblems--
	}
	if e.HasEmblem() {
		v.emblems++
	}
	v.filter.Update(index, e.Matched())
	v.highlightable[i] = e.Highlightable()
	v.texts[i], v.decor[i] = e.Text, e.Decor
	v.settle(i, i+1)
	return true
}

// Remove removes item index for good; whether it was there.
func (v *View) Remove(index uint32) bool {
	if !v.exists(index) {
		return false
	}
	i := int(index)
	if v.emblem(i) {
		v.emblems--
	}
	v.removed[i], v.decor[i] = true, nil
	v.filter.Remove(index)
	if v.following && v.follow == index {
		v.following = false
	}
	v.clamp()
	return true
}

// Preselect selects item index once it is there and settled, unless the
// user has moved the selection or edited the query; whether it is selected
// now.
func (v *View) Preselect(index uint32) bool {
	if v.touched {
		return false
	}
	v.preselect = &Preselect{Index: uint64(index), ByIndex: true}
	if i := int(index); i < len(v.texts) {
		v.settle(i, i+1)
	}
	return v.preselect == nil
}

// settle runs after the items in [from, to) arrived or changed: it keeps
// the item the user moved to selected, or applies the preselection once
// its item is there and settled.
func (v *View) settle(from, to int) {
	if v.following {
		v.selectItem(v.follow)
	} else if item, ok := v.preselected(from, to); ok {
		v.preselect = nil
		v.selectItem(item)
	}
	v.clamp()
}

func (v *View) exists(index uint32) bool {
	return int(index) < len(v.removed) && !v.removed[index]
}

func (v *View) emblem(i int) bool {
	d := v.decor[i]
	return d != nil && (d.Glyph != "" || d.Icon != "")
}

func (v *View) pending(index uint32) bool {
	return int(index) < len(v.decor) && v.decor[index] != nil && v.decor[index].Pending
}

// Apply applies a key's action.
func (v *View) Apply(a input.Action) Reaction {
	switch a.Kind {
	case input.Dismiss:
		return Reaction{Kind: Dismiss}
	case input.Accept, input.Descend:
		return v.accept(a.Kind == input.Descend)
	case input.Select:
		return v.step(a.Step)
	case input.EditQuery:
		return v.edit(a.Edit)
	}
	return Reaction{}
}

func (v *View) accept(descend bool) Reaction {
	item, ok := v.Selected()
	switch {
	case ok && v.pending(item):
		return Reaction{}
	case ok && descend:
		return Reaction{Kind: Accept, Choice: Choice{Kind: ChoseDescend, Index: item}}
	case ok:
		return Reaction{Kind: Accept, Choice: Choice{Kind: ChoseItem, Index: item}}
	case v.options.OnlyMatch, v.options.Required && v.query.Text() == "":
		return Reaction{}
	}
	return Reaction{Kind: Accept, Choice: Choice{Kind: ChoseTyped, Text: v.query.Text()}}
}

func (v *View) step(s input.Step) Reaction {
	row := v.selected
	switch {
	case s == input.Previous && row > 0:
		row--
	case s == input.Next && row+1 < v.matchedCount():
		row++
	default:
		return Reaction{}
	}
	v.preselect, v.touched = nil, true
	v.selected = row
	v.follow, v.following = v.Selected()
	v.clamp()
	return Reaction{Kind: Changed}
}

func (v *View) edit(e input.Edit) Reaction {
	if !v.query.Apply(e) {
		return Reaction{}
	}
	if v.query.Text() != v.filter.Query() {
		v.preselect, v.touched, v.following = nil, true, false
		v.filter.SetQuery(v.query.Text())
		v.selected, v.top = 0, 0
	}
	return Reaction{Kind: Changed}
}

// SetLines shows at most n rows from now on (the window was sized anew).
func (v *View) SetLines(n uint32) {
	v.options.Lines = &n
	v.clamp()
}

// Query is the query typed so far (unmasked).
func (v *View) Query() string { return v.query.Text() }

// Selected is the selected item, if any matches.
func (v *View) Selected() (uint32, bool) {
	if m := v.filter.Matches(); v.selected < len(m) {
		return m[v.selected].Index, true
	}
	return 0, false
}

func (v *View) matchedCount() int { return len(v.filter.Matches()) }

// preselected is the preselected item if it is among the items in
// [from, to), there and settled (a pending item is never preselected).
func (v *View) preselected(from, to int) (uint32, bool) {
	p := v.preselect
	if p == nil {
		return 0, false
	}
	found := -1
	if p.ByIndex {
		if p.Index >= uint64(from) && p.Index < uint64(to) {
			found = int(p.Index)
		}
	} else {
		want := strings.ToLower(p.Text)
		for i := from; i < to; i++ {
			if !v.removed[i] && strings.ToLower(v.texts[i]) == want {
				found = i
				break
			}
		}
	}
	if found < 0 || !v.exists(uint32(found)) || v.pending(uint32(found)) {
		return 0, false
	}
	return uint32(found), true
}

// selectItem selects item if it matches.
func (v *View) selectItem(item uint32) {
	for row, m := range v.filter.Matches() {
		if m.Index == item {
			v.selected = row
			return
		}
	}
}

// clamp keeps the selection on a match and in view.
func (v *View) clamp() {
	lines := max(int(v.options.Rows()), 1)
	v.selected = max(min(v.selected, v.matchedCount()-1), 0)
	if v.selected < v.top {
		v.top = v.selected
	} else if v.selected >= v.top+lines {
		v.top = v.selected + 1 - lines
	}
}
