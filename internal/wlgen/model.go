// Package wlgen generates the Go-side wrappers of package wlproto from a
// Wayland protocol XML file (spec §5, D5): one Go type per interface with a
// method per request and a handler field per event, the enums as constants,
// and the event decoding that wlproto's dispatcher calls. The C side of each
// protocol (interface tables and client header) comes from wayland-scanner,
// run on the same XML; libwayland-client carries the core protocol's tables.
//
// cmd/wlgen is the command `go generate` runs; tests in wlproto check that
// the committed wrappers are what this package generates.
package wlgen

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Protocol is one parsed protocol XML file.
type Protocol struct {
	Name       string      `xml:"name,attr"`
	Interfaces []Interface `xml:"interface"`
}

// Interface is one protocol interface.
type Interface struct {
	Name     string    `xml:"name,attr"`
	Version  int       `xml:"version,attr"`
	Summary  string    `xml:"description>summary"`
	Requests []Message `xml:"request"`
	Events   []Message `xml:"event"`
	Enums    []Enum    `xml:"enum"`
}

// Message is a request or an event.
type Message struct {
	Name  string `xml:"name,attr"`
	Type  string `xml:"type,attr"`
	Since int    `xml:"since,attr"`
	Args  []Arg  `xml:"arg"`
}

// Destructor reports whether the message ends the object's life.
func (m Message) Destructor() bool { return m.Type == "destructor" }

// Arg is one message argument.
type Arg struct {
	Name      string `xml:"name,attr"`
	Type      string `xml:"type,attr"`
	Interface string `xml:"interface,attr"`
	AllowNull bool   `xml:"allow-null,attr"`
	Summary   string `xml:"summary,attr"`
}

// Enum is a set of named values.
type Enum struct {
	Name     string  `xml:"name,attr"`
	Bitfield bool    `xml:"bitfield,attr"`
	Entries  []Entry `xml:"entry"`
}

// Entry is one enum value.
type Entry struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Parse reads a protocol XML document and checks what the generator relies
// on: every argument type is known, typed where it must be, and no request
// carries an array (none of the protocols logis uses has one).
func Parse(data []byte) (*Protocol, error) {
	var p Protocol
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("protocol XML: %w", err)
	}
	if p.Name == "" || len(p.Interfaces) == 0 {
		return nil, fmt.Errorf("protocol XML: no protocol with interfaces")
	}
	for _, iface := range p.Interfaces {
		for _, m := range iface.Requests {
			if err := checkArgs(iface.Name, m, false); err != nil {
				return nil, err
			}
		}
		for _, m := range iface.Events {
			if err := checkArgs(iface.Name, m, true); err != nil {
				return nil, err
			}
		}
	}
	return &p, nil
}

func checkArgs(iface string, m Message, event bool) error {
	where := iface + "." + m.Name
	creates := 0
	for _, a := range m.Args {
		switch a.Type {
		case "int", "uint", "fixed", "string", "object", "fd":
		case "array":
			if !event {
				return fmt.Errorf("%s: array arguments in requests are not supported", where)
			}
		case "new_id":
			creates++
			if event && a.Interface == "" {
				return fmt.Errorf("%s: an event cannot create an object of unknown interface", where)
			}
		default:
			return fmt.Errorf("%s: unknown argument type %q", where, a.Type)
		}
	}
	if creates > 1 {
		return fmt.Errorf("%s: more than one new_id argument", where)
	}
	return nil
}

// camel turns a snake_case protocol name into a Go identifier part:
// wl_surface → WlSurface, set_size → SetSize, 90 → 90.
func camel(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

// goKeywords are reserved words an argument name must not become, plus
// names the generated methods use for themselves.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"a": true, "o": true, "p": true, "C": true, "string": true, "len": true,
}

// lowerCamel makes an argument name: app_id → appID-like appId; keywords
// get a trailing underscore.
func lowerCamel(name string) string {
	c := camel(name)
	if c == "" {
		return "arg_"
	}
	c = strings.ToLower(c[:1]) + c[1:]
	if goKeywords[c] {
		return c + "_"
	}
	return c
}
