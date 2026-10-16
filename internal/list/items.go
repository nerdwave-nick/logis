package list

import "strings"

// A View's items as they arrive, change and go, and the preselection that
// waits for its item.

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

func (v *View) emblem(i int) bool { return v.decor[i].emblem() }

func (v *View) pending(index uint32) bool {
	return int(index) < len(v.decor) && v.decor[index] != nil && v.decor[index].Pending
}

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
		for i := from; i < to; i++ {
			if !v.removed[i] && strings.EqualFold(v.texts[i], p.Text) {
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
