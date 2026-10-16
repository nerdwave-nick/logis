package provider

import (
	"fmt"
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/list"
)

// A provider's stages as the window takes them: the provider names items by
// id, the list by index (the order the ids first arrived in its stage).
// Stages maps one onto the other, turns wire items into list entries, and
// turns the user's answers back into lines for the provider.

const (
	// ToastSeconds is how long a toast shows unless the provider says
	// otherwise.
	ToastSeconds = 3
	// MaxToastSeconds is the longest a toast may show.
	MaxToastSeconds = 60
)

// Stages is the current stage of one provider session.
type Stages struct {
	current *current
}

type current struct {
	id string
	// index maps item id → index; ids index → item id.
	index map[string]uint32
	ids   []string
	// selectID is a select whose item has not arrived yet.
	selectID *string
}

// Apply is what a provider's message asks of the window. Hello and Close
// are the session's business, not the stages'; Unknown asks nothing.
func (s *Stages) Apply(m Message) ([]Step, error) {
	switch m := m.(type) {
	case Stage:
		return []Step{s.stage(m)}, nil
	case Items:
		c, err := s.currentFor("items")
		if err != nil {
			return nil, err
		}
		if m.Select != nil {
			c.selectID = m.Select
		}
		return c.upsert(m.Items), nil
	case Remove:
		c, err := s.currentFor("remove")
		if err != nil {
			return nil, err
		}
		var gone []uint32
		for _, id := range m.IDs {
			if i, ok := c.index[id]; ok {
				gone = append(gone, i)
			}
		}
		if len(gone) == 0 {
			return nil, nil
		}
		return []Step{{Kind: StepRemove, Removed: gone}}, nil
	case Toast:
		seconds := uint32(ToastSeconds)
		if m.Seconds != nil {
			seconds = min(max(*m.Seconds, 1), MaxToastSeconds)
		}
		return []Step{{Kind: StepToast, Notice: list.Notice{Text: m.Text, Level: m.Level},
			Span: time.Duration(seconds) * time.Second}}, nil
	case Ready:
		return []Step{{Kind: StepReady}}, nil
	}
	return nil, nil
}

// Answer is the line an answer sends the provider; false for a dismissal,
// or an index no item has.
func (s *Stages) Answer(a Answer) (ToProvider, bool) {
	c := s.current
	if c == nil {
		return nil, false
	}
	switch a.Kind {
	case AnswerItem, AnswerDescend:
		if a.Index < 0 || a.Index >= len(c.ids) {
			return nil, false
		}
		return Activate{Stage: c.id, ID: c.ids[a.Index]}, true
	case AnswerTyped:
		return Submit{Stage: c.id, Text: a.Text}, true
	}
	return nil, false
}

func (s *Stages) currentFor(what string) (*current, error) {
	if s.current == nil {
		return nil, fmt.Errorf("'%s' came before any 'stage'", what)
	}
	return s.current, nil
}

func (s *Stages) stage(stage Stage) Step {
	o := list.DefaultOptions()
	if stage.Prompt != nil {
		o.Prompt = *stage.Prompt
	}
	o.Placeholder, o.Search = stage.Placeholder, stage.Query
	o.OnlyMatch, o.Required = stage.Input == InputNone, stage.Input == InputRequired
	o.Lines, o.Width = stage.Lines, stage.Width
	c := &current{id: stage.ID, index: map[string]uint32{}}
	var entries []list.Entry
	for _, it := range stage.Items {
		if i, ok := c.index[it.ID]; ok {
			entries[i] = entry(it)
			continue
		}
		c.index[it.ID] = uint32(len(c.ids))
		c.ids = append(c.ids, it.ID)
		entries = append(entries, entry(it))
	}
	if stage.Select != nil {
		if i, ok := c.index[*stage.Select]; ok {
			o.Select = &list.Preselect{Index: uint64(i), ByIndex: true}
		} else {
			c.selectID = stage.Select
		}
	}
	s.current = c
	return Step{Kind: StepStage, Options: o, Entries: entries}
}

// upsert appends new ids and replaces known ones in their place.
func (c *current) upsert(items []Item) []Step {
	var fresh []list.Entry
	var changed []Update
	for _, it := range items {
		if i, ok := c.index[it.ID]; ok {
			changed = append(changed, Update{Index: i, Entry: entry(it)})
			continue
		}
		c.index[it.ID] = uint32(len(c.ids))
		c.ids = append(c.ids, it.ID)
		fresh = append(fresh, entry(it))
	}
	var steps []Step
	if len(fresh) > 0 {
		steps = append(steps, Step{Kind: StepAppend, Entries: fresh})
	}
	if len(changed) > 0 {
		steps = append(steps, Step{Kind: StepUpdate, Updates: changed})
	}
	if c.selectID != nil {
		if i, ok := c.index[*c.selectID]; ok {
			c.selectID = nil
			steps = append(steps, Step{Kind: StepPreselect, Index: i})
		}
	}
	return steps
}

// columnBreaks: a tab or line break inside a column becomes a space, so
// columns stay lined up.
var columnBreaks = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// entry is a wire item as a list entry: its columns joined by tabs.
func entry(it Item) list.Entry {
	columns := make([]string, len(it.Columns))
	for i, c := range it.Columns {
		columns[i] = columnBreaks.Replace(c)
	}
	e := list.Entry{Text: strings.Join(columns, "\t")}
	if it.Match != nil {
		e.MatchText, e.HasMatchText = *it.Match, true
	}
	if it.Glyph == nil && it.Icon == nil && len(it.Badges) == 0 && !it.Pending {
		return e
	}
	d := &list.Decor{Badges: it.Badges, Pending: it.Pending}
	if it.Glyph != nil {
		d.Glyph, d.HasGlyph = *it.Glyph, true
	}
	if it.Icon != nil {
		d.Icon, d.HasIcon = *it.Icon, true
	}
	e.Decor = d
	return e
}
