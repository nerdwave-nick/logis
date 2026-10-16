package list

import (
	"strings"

	"github.com/nerdwave-nick/logis/internal/fuzzy"
)

// Match is one matching item: its index (in the order the items arrived)
// and its score (higher is better; 0 for an empty query).
type Match struct {
	Index uint32
	Score uint32
}

// Boosted is an item's match text and how much higher its matches score.
type Boosted struct {
	Text  string
	Boost uint32
}

// Filter is the items, the query and what matches it: best scores first,
// equal scores in input order. Incremental, so 100 000 streamed lines stay
// responsive:
//   - items arriving later are matched alone and merged in;
//   - a query that only grows (typing on) re-matches only the last matches;
//   - large sets are matched on several goroutines, one matcher each.
//
// A Filter is not safe for concurrent use.
type Filter struct {
	texts []fuzzy.Text
	// removed: per item, whether it was removed; shorter than texts when
	// the later items never were (a dmenu list never removes any).
	removed []bool
	// boosts: per item, how much its matches score higher; shorter than
	// texts when the later items have none.
	boosts   []uint32
	query    string
	pattern  fuzzy.Pattern
	matches  []Match
	matchers []*fuzzy.Matcher
	// Scratch memory reused from keystroke to keystroke.
	raw        []Match
	counts     []int
	candidates []uint32
	marks      []bool
}

// NewFilter is a filter with no items and an empty query.
func NewFilter() *Filter { return &Filter{} }

// Len is how many items there are, removed ones included.
func (f *Filter) Len() int { return len(f.texts) }

// Matches are the matching items, best first. The slice is the filter's
// own, valid until the next change.
func (f *Filter) Matches() []Match { return f.matches }

// Query is the query in force.
func (f *Filter) Query() string { return f.query }

// Push adds items (the texts they are matched by) and matches them.
func (f *Filter) Push(texts ...string) {
	first := len(f.texts)
	for _, s := range texts {
		f.texts = append(f.texts, fuzzy.NewText(s))
	}
	f.matchFresh(first)
}

// PushBoosted adds items whose matches score higher by their boost
// (frecency), and matches them.
func (f *Filter) PushBoosted(items []Boosted) {
	first := len(f.texts)
	for _, it := range items {
		if it.Boost > 0 {
			f.boosts = append(f.boosts, make([]uint32, first-len(f.boosts))...)
			for _, it := range items {
				f.boosts = append(f.boosts, it.Boost)
			}
			break
		}
	}
	for _, it := range items {
		f.texts = append(f.texts, fuzzy.NewText(it.Text))
	}
	f.matchFresh(first)
}

// matchFresh matches the items from first on and merges them in.
func (f *Filter) matchFresh(first int) {
	fresh := make([]uint32, 0, len(f.texts)-first)
	for i := first; i < len(f.texts); i++ {
		fresh = append(fresh, uint32(i))
	}
	if f.pattern.Empty() {
		for _, i := range fresh {
			f.matches = append(f.matches, Match{Index: i})
		}
		return
	}
	f.matches = merge(f.matches, f.score(fresh, nil))
}

// Remove removes item index for good: it never matches again, and the
// other items keep their indices.
func (f *Filter) Remove(index uint32) {
	if !f.alive(index) {
		return
	}
	f.removed = append(f.removed, make([]bool, len(f.texts)-len(f.removed))...)
	f.removed[index] = true
	f.texts[index] = fuzzy.Text{}
	f.drop(index)
}

// Update gives item index a new text to be matched by, in its place.
func (f *Filter) Update(index uint32, text string) {
	if !f.alive(index) {
		return
	}
	f.texts[index] = fuzzy.NewText(text)
	f.drop(index)
	found := []Match{{Index: index}}
	if !f.pattern.Empty() {
		found = f.score([]uint32{index}, nil)
	}
	f.matches = merge(f.matches, found)
}

func (f *Filter) drop(index uint32) {
	for i, m := range f.matches {
		if m.Index == index {
			f.matches = append(f.matches[:i], f.matches[i+1:]...)
			return
		}
	}
}

// alive reports whether item index exists and was not removed.
func (f *Filter) alive(index uint32) bool {
	i := int(index)
	return i < len(f.texts) && (i >= len(f.removed) || !f.removed[i])
}

// every is every item that was not removed, ascending.
func (f *Filter) every() []uint32 {
	out := f.candidates[:0]
	for i := range f.texts {
		if f.alive(uint32(i)) {
			out = append(out, uint32(i))
		}
	}
	f.candidates = out
	return out
}

// previous are the items that match now, ascending.
func (f *Filter) previous() []uint32 {
	marks := grow(f.marks, len(f.texts))
	clear(marks)
	for _, m := range f.matches {
		marks[m.Index] = true
	}
	out := f.candidates[:0]
	for i, marked := range marks {
		if marked {
			out = append(out, uint32(i))
		}
	}
	f.marks, f.candidates = marks, out
	return out
}

// SetQuery matches every item against query; nothing happens if it is the
// query in force.
func (f *Filter) SetQuery(query string) {
	if query == f.query {
		return
	}
	// Typing on only ever removes matches, unless what is typed can widen
	// them (a negation, or an escape that changes a word's kind).
	narrows := strings.HasPrefix(query, f.query) && !f.pattern.Empty() &&
		!strings.ContainsAny(query, "!\\")
	f.query = query
	f.pattern = fuzzy.Parse(query)
	if f.pattern.Empty() {
		every := f.every()
		f.matches = grow(f.matches, len(every))
		for n, i := range every {
			f.matches[n] = Match{Index: i}
		}
		return
	}
	// The new matches replace the old, whose memory they reuse.
	candidates := f.every
	if narrows {
		candidates = f.previous
	}
	f.matches = f.score(candidates(), f.matches)
}

// Positions are the positions in item index that the query matched, as
// graphemes counted from 0, ascending (empty for an empty query, no such
// item or no match).
func (f *Filter) Positions(index uint32) []uint32 {
	if f.pattern.Empty() || int(index) >= len(f.texts) {
		return nil
	}
	return f.pattern.Positions(f.matcher(0), f.texts[index], nil)
}

// matcher is the i-th matcher (each holds its own scratch memory).
func (f *Filter) matcher(i int) *fuzzy.Matcher {
	for len(f.matchers) <= i {
		f.matchers = append(f.matchers, fuzzy.NewMatcher())
	}
	return f.matchers[i]
}
