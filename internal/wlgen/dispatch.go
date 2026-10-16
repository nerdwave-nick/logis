package wlgen

import (
	"fmt"
	"strings"
)

// dispatch writes the method wlproto's dispatcher calls with an event's
// opcode and arguments.
func (g *gen) dispatch(i Interface, typ string) {
	g.line("")
	if len(i.Events) == 0 {
		g.line("func (o *%s) dispatch(uint32, []argument) {}", typ)
		return
	}
	g.line("func (o *%s) dispatch(opcode uint32, a []argument) {", typ)
	g.line("switch opcode {")
	for opcode, e := range i.Events {
		g.line("case %d:", opcode)
		g.event(e)
	}
	g.line("}")
	g.line("}")
}

// event decodes one event's arguments and calls its handler. Without a
// handler, what the event handed over (fds, new objects) is released, so
// nothing leaks; a destructor event forgets the proxy after its handler.
func (g *gen) event(e Message) {
	var values, cleanup []string
	for k, a := range e.Args {
		v := lowerCamel(a.Name)
		switch a.Type {
		case "int":
			values = append(values, fmt.Sprintf("a[%d].int()", k))
		case "uint":
			values = append(values, fmt.Sprintf("a[%d].uint()", k))
		case "fixed":
			values = append(values, fmt.Sprintf("a[%d].fixed()", k))
		case "string":
			values = append(values, fmt.Sprintf("a[%d].str()", k))
		case "array":
			values = append(values, fmt.Sprintf("a[%d].array()", k))
		case "fd":
			g.line("%s := a[%d].fd()", v, k)
			values = append(values, v)
			cleanup = append(cleanup, fmt.Sprintf("closeFd(%s)", v))
		case "object":
			values = append(values, fmt.Sprintf("eventObject[%s](a[%d])", goType(a), k))
		case "new_id":
			g.line("%s, _ := o.wrap(a[%d], %sInterface).(%s)", v, k, camel(a.Interface), goType(a))
			values = append(values, v)
			cleanup = append(cleanup, fmt.Sprintf("if %s != nil {\n%s.Destroy()\n}", v, v))
		}
	}
	handler := "o.On" + camel(e.Name)
	g.line("if %s != nil {", handler)
	g.line("%s(%s)", handler, strings.Join(values, ", "))
	if len(cleanup) > 0 {
		g.line("} else {")
		for _, c := range cleanup {
			g.line("%s", c)
		}
	}
	g.line("}")
	if e.Destructor() {
		g.line("o.forget()")
	}
}
