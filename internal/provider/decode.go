package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
)

// Parse reads one line from a provider: a Message, Unknown for a type
// this version does not know, or why the line breaks the protocol (not
// JSON, no "type", or a known type with a malformed field).
func Parse(line []byte) (Message, error) {
	if !utf8.Valid(line) {
		return nil, errors.New("not a JSON line: invalid UTF-8")
	}
	if !json.Valid(line) {
		var v any
		return nil, fmt.Errorf("not a JSON line: %v", json.Unmarshal(line, &v))
	}
	var tagged struct {
		Type *string `json:"type"`
	}
	if json.Unmarshal(line, &tagged) != nil || tagged.Type == nil {
		return nil, errors.New(`a line without a "type"`)
	}
	kind := *tagged.Type
	decode, known := decoders[kind]
	if !known {
		return Unknown{Type: kind}, nil
	}
	m, err := decode(line)
	if err != nil {
		return nil, fmt.Errorf("a malformed '%s': %v", kind, err)
	}
	return m, nil
}

// decoders read each known type; a required field is refused when missing.
var decoders = map[string]func([]byte) (Message, error){
	"hello": func(b []byte) (Message, error) {
		var w struct {
			Protocol *uint32 `json:"protocol"`
		}
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		if w.Protocol == nil {
			return nil, missing("protocol")
		}
		return Hello{Protocol: *w.Protocol}, nil
	},
	"stage": func(b []byte) (Message, error) {
		var s Stage
		err := json.Unmarshal(b, &s)
		return s, err
	},
	"items": func(b []byte) (Message, error) {
		var w struct {
			Items  []Item  `json:"items"`
			Select *string `json:"select"`
		}
		err := json.Unmarshal(b, &w)
		return Items{Items: w.Items, Select: w.Select}, err
	},
	"remove": func(b []byte) (Message, error) {
		var w struct {
			IDs *[]string `json:"ids"`
		}
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		if w.IDs == nil {
			return nil, missing("ids")
		}
		return Remove{IDs: *w.IDs}, nil
	},
	"toast": func(b []byte) (Message, error) {
		var w struct {
			Text    *string `json:"text"`
			Level   level   `json:"level"`
			Seconds *uint32 `json:"seconds"`
		}
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		if w.Text == nil {
			return nil, missing("text")
		}
		return Toast{Text: *w.Text, Level: list.Level(w.Level), Seconds: w.Seconds}, nil
	},
	"ready": func([]byte) (Message, error) { return Ready{}, nil },
	"close": func([]byte) (Message, error) { return Close{}, nil },
}

func missing(field string) error { return fmt.Errorf("missing field `%s`", field) }

// UnmarshalJSON reads "none", "optional" or "required".
func (i *Input) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "none":
		*i = InputNone
	case "optional":
		*i = InputOptional
	case "required":
		*i = InputRequired
	default:
		return fmt.Errorf("unknown input '%s', expected none, optional or required", s)
	}
	return nil
}

// level is a toast's level on the wire: "info", "success" or "error".
type level list.Level

func (l *level) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "info":
		*l = level(list.Info)
	case "success":
		*l = level(list.Success)
	case "error":
		*l = level(list.Error)
	default:
		return fmt.Errorf("unknown level '%s', expected info, success or error", s)
	}
	return nil
}
