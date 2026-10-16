package xkb

// expr is an expression node.
type expr struct {
	kind  string // ident num str key call list brace bin unary assign index
	text  string // ident/num/str/key text, call name, operator
	args  []*expr
	left  *expr
	right *expr
}

// stmt is one statement: a head (keywords, names), an optional index, an
// optional assigned value, and an optional braced body of statements.
type stmt struct {
	head  []token
	index *expr
	value *expr
	body  []*stmt
	items []*expr // a key's or modifier_map's body, virtual_modifiers' names
	line  int
}

// parser is a recursive-descent parser; depth bounds its recursion, so a
// hostile keymap cannot exhaust the stack.
type parser struct {
	lex   *lexer
	tok   token // the lookahead
	depth int
}

func newParser(src string) *parser {
	p := &parser{lex: newLexer(src)}
	p.tok = p.lex.scan()
	return p
}

func (p *parser) peek() token { return p.tok }

func (p *parser) next() token {
	t := p.tok
	if t.kind != tEOF {
		p.tok = p.lex.scan()
	}
	return t
}

func (p *parser) is(s string) bool { t := p.peek(); return t.kind == tPunct && t.text == s }

func (p *parser) expect(s string) error {
	if !p.is(s) {
		t := p.peek()
		return errorf(t.line, "expected %q, got %q", s, t.text)
	}
	p.next()
	return nil
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxDepth {
		return errorf(p.peek().line, "nested deeper than %d", maxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

// statements parses statements until a closing brace or EOF.
func (p *parser) statements() ([]*stmt, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	var out []*stmt
	for !p.is("}") && p.peek().kind != tEOF {
		if p.is(";") {
			p.next()
			continue
		}
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *parser) statement() (*stmt, error) {
	s := &stmt{line: p.peek().line}
	for {
		t := p.peek()
		if t.kind == tPunct && (t.text == "{" || t.text == "=" || t.text == ";" || t.text == "[") {
			break
		}
		if t.kind == tEOF || (t.kind == tPunct && t.text == "}") {
			return nil, errorf(t.line, "unexpected %q", t.text)
		}
		if len(s.head) > 0 && s.head[0].text == "interpret" && t.kind == tPunct && t.text == "+" {
			if err := p.interpretMatch(s); err != nil {
				return nil, err
			}
			continue
		}
		if len(s.head) > 0 && s.head[0].text == "virtual_modifiers" {
			return s, p.virtualModifiers(s)
		}
		s.head = append(s.head, p.next())
	}
	if p.is("[") {
		p.next()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if err := p.expect("]"); err != nil {
			return nil, err
		}
		s.index = e
	}
	if p.is("=") {
		p.next()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		s.value = e
		return s, p.expect(";")
	}
	if p.is("{") {
		p.next()
		var err error
		if len(s.head) > 0 && (s.head[0].text == "key" || s.head[0].text == "modifier_map") {
			s.items, err = p.list("}")
		} else {
			s.body, err = p.statements()
		}
		if err != nil {
			return nil, err
		}
		if err := p.expect("}"); err != nil {
			return nil, err
		}
	}
	return s, p.expect(";")
}

// interpretMatch parses an interpret's "+Match(mods)" into s.index.
func (p *parser) interpretMatch(s *stmt) error {
	p.next()
	m := p.next()
	if err := p.expect("("); err != nil {
		return err
	}
	e, err := p.expr()
	if err != nil {
		return err
	}
	if err := p.expect(")"); err != nil {
		return err
	}
	s.index = &expr{kind: "call", text: m.text, args: []*expr{e}}
	return nil
}

// virtualModifiers parses "virtual_modifiers A, B=0x4000, C;" into
// s.items.
func (p *parser) virtualModifiers(s *stmt) error {
	for {
		name := p.next()
		if name.kind != tIdent {
			return errorf(name.line, "expected a modifier name, got %q", name.text)
		}
		d := &expr{kind: "ident", text: name.text}
		if p.is("=") {
			p.next()
			v, err := p.expr()
			if err != nil {
				return err
			}
			d = &expr{kind: "assign", left: d, right: v}
		}
		s.items = append(s.items, d)
		if !p.is(",") {
			break
		}
		p.next()
	}
	return p.expect(";")
}
