package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
)

// An open's options are the list's options (list.Options) in the shape
// Rust's serde gave them: kebab-case keys, every one optional (a missing
// key keeps its default), lines and width left out when unset, password a
// one-character string, select {"text": T} or {"index": N}.

type wireOptions struct {
	Prompt      *string        `json:"prompt,omitempty"`
	Placeholder string         `json:"placeholder"`
	Password    *string        `json:"password"`
	Lines       *uint32        `json:"lines,omitempty"`
	Width       *uint32        `json:"width,omitempty"`
	Counter     bool           `json:"counter"`
	Search      string         `json:"search"`
	Select      *wirePreselect `json:"select"`
	OnlyMatch   bool           `json:"only-match"`
	Required    bool           `json:"required"`
}

type wirePreselect struct {
	Text  *string `json:"text,omitempty"`
	Index *uint64 `json:"index,omitempty"`
}

// EncodeOptions is o as an open's options.
func EncodeOptions(o list.Options) json.RawMessage {
	w := wireOptions{
		Prompt: &o.Prompt, Placeholder: o.Placeholder, Lines: o.Lines, Width: o.Width,
		Counter: o.Counter, Search: o.Search, OnlyMatch: o.OnlyMatch, Required: o.Required,
	}
	if o.Password != 0 {
		mask := string(o.Password)
		w.Password = &mask
	}
	if p := o.Select; p != nil {
		if p.ByIndex {
			w.Select = &wirePreselect{Index: &p.Index}
		} else {
			w.Select = &wirePreselect{Text: &p.Text}
		}
	}
	raw, err := json.Marshal(w)
	if err != nil {
		panic(err) // strings, numbers and bools always marshal
	}
	return raw
}

// DecodeOptions reads an open's options; nothing (or null) is the
// defaults. An option of the wrong shape is refused.
func DecodeOptions(raw json.RawMessage) (list.Options, error) {
	o := list.DefaultOptions()
	if len(raw) == 0 {
		return o, nil
	}
	var w wireOptions
	if err := json.Unmarshal(raw, &w); err != nil {
		return o, optionsError(err)
	}
	if w.Prompt != nil {
		o.Prompt = *w.Prompt
	}
	o.Placeholder, o.Lines, o.Width, o.Counter = w.Placeholder, w.Lines, w.Width, w.Counter
	o.Search, o.OnlyMatch, o.Required = w.Search, w.OnlyMatch, w.Required
	if w.Password != nil {
		mask, size := utf8.DecodeRuneInString(*w.Password)
		if size == 0 || size != len(*w.Password) {
			return o, optionsError(fmt.Errorf("password '%s' is not one character", *w.Password))
		}
		o.Password = mask
	}
	if s := w.Select; s != nil {
		switch {
		case s.Text != nil && s.Index == nil:
			o.Select = &list.Preselect{Text: *s.Text}
		case s.Index != nil && s.Text == nil:
			o.Select = &list.Preselect{Index: *s.Index, ByIndex: true}
		default:
			return o, optionsError(errors.New("select wants one of text or index"))
		}
	}
	return o, nil
}

func optionsError(err error) error {
	return fmt.Errorf("the list's options are malformed: %w", err)
}
