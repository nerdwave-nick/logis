package xkb

// list parses comma-separated expressions until close (not consumed).
func (p *parser) list(close string) ([]*expr, error) {
	var out []*expr
	for !p.is(close) {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.is("=") { // field[idx] = value inside a key body or call
			p.next()
			v, err := p.expr()
			if err != nil {
				return nil, err
			}
			e = &expr{kind: "assign", left: e, right: v}
		}
		out = append(out, e)
		if !p.is(",") {
			break
		}
		p.next()
	}
	return out, nil
}

func (p *parser) expr() (*expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.is("+") || p.is("-") || p.is("*") || p.is("/") {
		op := p.next().text
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = &expr{kind: "bin", text: op, left: l, right: r}
	}
	return l, nil
}

func (p *parser) unary() (*expr, error) {
	if p.is("!") || p.is("-") || p.is("+") || p.is("~") {
		if err := p.enter(); err != nil {
			return nil, err
		}
		defer p.leave()
		op := p.next().text
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &expr{kind: "unary", text: op, left: x}, nil
	}
	return p.term()
}

func (p *parser) term() (*expr, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return &expr{kind: "num", text: t.text}, nil
	case tStr:
		return &expr{kind: "str", text: t.text}, nil
	case tKeyName:
		return &expr{kind: "key", text: t.text}, nil
	case tIdent:
		return p.identTerm(t)
	case tPunct:
		switch t.text {
		case "[":
			items, err := p.list("]")
			if err != nil {
				return nil, err
			}
			return &expr{kind: "list", args: items}, p.expect("]")
		case "{":
			items, err := p.list("}")
			if err != nil {
				return nil, err
			}
			return &expr{kind: "brace", args: items}, p.expect("}")
		case "(":
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			return e, p.expect(")")
		}
	}
	return nil, errorf(t.line, "unexpected %q in an expression", t.text)
}

// identTerm is a (dotted) name, a call or an indexed field.
func (p *parser) identTerm(t token) (*expr, error) {
	e := &expr{kind: "ident", text: t.text}
	for p.is(".") {
		p.next()
		e.text += "." + p.next().text
	}
	if p.is("(") {
		p.next()
		args, err := p.list(")")
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return &expr{kind: "call", text: e.text, args: args}, nil
	}
	if p.is("[") {
		p.next()
		i, err := p.expr()
		if err != nil {
			return nil, err
		}
		if err := p.expect("]"); err != nil {
			return nil, err
		}
		return &expr{kind: "index", left: e, right: i}, nil
	}
	return e, nil
}
