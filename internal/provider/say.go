package provider

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/nerdwave-nick/logis/internal/list"
)

// Say is one line a provider says to logis, newline included: the "type"
// first, every field present, null for what is absent, strings as they
// are (no HTML escapes). logis's
// own built-in providers (servitor mode) speak through it.
func Say(m Message) []byte {
	var w any
	switch m := m.(type) {
	case Hello:
		w = struct {
			Type     string `json:"type"`
			Protocol uint32 `json:"protocol"`
		}{"hello", m.Protocol}
	case Stage:
		w = struct {
			Type string `json:"type"`
			stageWire
		}{"stage", stageWireOf(m)}
	case Items:
		w = struct {
			Type   string     `json:"type"`
			Items  []itemWire `json:"items"`
			Select *string    `json:"select"`
		}{"items", itemsWireOf(m.Items), m.Select}
	case Remove:
		w = struct {
			Type string   `json:"type"`
			IDs  []string `json:"ids"`
		}{"remove", orEmpty(m.IDs)}
	case Toast:
		w = struct {
			Type    string  `json:"type"`
			Text    string  `json:"text"`
			Level   string  `json:"level"`
			Seconds *uint32 `json:"seconds"`
		}{"toast", m.Text, levelName(m.Level), m.Seconds}
	case Ready:
		w = struct {
			Type string `json:"type"`
		}{"ready"}
	case Close:
		w = struct {
			Type string `json:"type"`
		}{"close"}
	default:
		panic(fmt.Sprintf("a provider cannot say %#v", m))
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(w); err != nil {
		panic(err) // strings and numbers always encode
	}
	return b.Bytes()
}

type stageWire struct {
	ID          string     `json:"id"`
	Prompt      *string    `json:"prompt"`
	Placeholder string     `json:"placeholder"`
	Query       string     `json:"query"`
	Input       string     `json:"input"`
	Select      *string    `json:"select"`
	Lines       *uint32    `json:"lines"`
	Width       *uint32    `json:"width"`
	Items       []itemWire `json:"items"`
}

type itemWire struct {
	ID      string   `json:"id"`
	Columns []string `json:"columns"`
	Match   *string  `json:"match"`
	Icon    *string  `json:"icon"`
	Glyph   *string  `json:"glyph"`
	Badges  []string `json:"badges"`
	Pending bool     `json:"pending"`
}

func stageWireOf(s Stage) stageWire {
	return stageWire{
		ID: s.ID, Prompt: s.Prompt, Placeholder: s.Placeholder, Query: s.Query,
		Input: [...]string{"none", "optional", "required"}[s.Input], Select: s.Select,
		Lines: s.Lines, Width: s.Width, Items: itemsWireOf(s.Items),
	}
}

func itemsWireOf(items []Item) []itemWire {
	out := make([]itemWire, 0, len(items))
	for _, i := range items {
		out = append(out, itemWire{
			ID: i.ID, Columns: orEmpty(i.Columns), Match: i.Match, Icon: i.Icon, Glyph: i.Glyph,
			Badges: orEmpty(i.Badges), Pending: i.Pending,
		})
	}
	return out
}

func levelName(l list.Level) string {
	switch l {
	case list.Success:
		return "success"
	case list.Error:
		return "error"
	}
	return "info"
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
