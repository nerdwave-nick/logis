package ot

// Context and chained context subtables (GSUB 5/6, GPOS 7/8), formats 1–3.

// rule is a context rule read from a subtable: input values after the
// first glyph, backtrack and lookahead values, and its lookup records.
type rule struct {
	t                     table
	back, input, ahead    int // offsets of the value arrays
	nBack, nInput, nAhead int // nInput counts the first glyph
	records               []sequenceLookup
}

func (r rule) value(off, i int) uint16 { return r.t.u16(off + 2*i) }

func contextRule(t table) rule {
	n := int(t.u16(0))
	records := int(t.u16(2))
	return rule{t: t, input: 4, nInput: n, records: sequenceLookups(t, 4+2*max(n-1, 0), records)}
}

func chainRule(t table) rule {
	r := rule{t: t}
	r.nBack = int(t.u16(0))
	r.back = 2
	o := 2 + 2*r.nBack
	r.nInput = int(t.u16(o))
	r.input = o + 2
	o = r.input + 2*max(r.nInput-1, 0)
	r.nAhead = int(t.u16(o))
	r.ahead = o + 2
	o = r.ahead + 2*r.nAhead
	r.records = sequenceLookups(t, o+2, int(t.u16(o)))
	return r
}

// valueMatch matches a glyph against a value: its id (format 1), or its
// class in a class definition (format 2; a missing one is all class 0).
func valueMatch(byClass bool, classes table) func(info *Info, v uint16) bool {
	if !byClass {
		return func(info *Info, v uint16) bool { return uint16(info.Glyph) == v }
	}
	return func(info *Info, v uint16) bool { return uint16(classes.classOf(uint16(info.Glyph))) == v }
}

func (c *applyContext) context(t table) bool {
	g := uint16(c.b.cur().Glyph)
	switch t.u16(0) {
	case 1, 2:
		if t.sub(2).coverage(g) < 0 {
			return false
		}
		var classes table
		set := t.sub(2).coverage(g)
		sets, nSets := 6, int(t.u16(4))
		if t.u16(0) == 2 {
			classes = t.sub(4)
			set = classes.classOf(g)
			sets, nSets = 8, int(t.u16(6))
		}
		if set >= nSets {
			return false
		}
		rs := t.sub(sets + 2*set)
		match := valueMatch(t.u16(0) == 2, classes)
		for k := range int(rs.u16(0)) {
			if c.applyRule(contextRule(rs.sub(2+2*k)), nil, match, nil) {
				return true
			}
		}
	case 3:
		n := int(t.u16(2))
		if n == 0 || t.sub(6).coverage(g) < 0 {
			return false
		}
		cov := func(info *Info, i int) bool { return t.sub(6+2*(i+1)).coverage(uint16(info.Glyph)) >= 0 }
		end, _, ok := c.matchInput(n, cov)
		if !ok {
			return false
		}
		c.applyRecords(n, end, sequenceLookups(t, 6+2*n, int(t.u16(4))))
		return true
	}
	return false
}

func (c *applyContext) chainContext(t table) bool {
	g := uint16(c.b.cur().Glyph)
	switch t.u16(0) {
	case 1, 2:
		if t.sub(2).coverage(g) < 0 {
			return false
		}
		var back, input, ahead table
		set := t.sub(2).coverage(g)
		sets, nSets := 6, int(t.u16(4))
		if t.u16(0) == 2 {
			back, input, ahead = t.sub(4), t.sub(6), t.sub(8)
			set = input.classOf(g)
			sets, nSets = 12, int(t.u16(10))
		}
		if set >= nSets {
			return false
		}
		rs := t.sub(sets + 2*set)
		byClass := t.u16(0) == 2
		for k := range int(rs.u16(0)) {
			if c.applyRule(chainRule(rs.sub(2+2*k)), valueMatch(byClass, back), valueMatch(byClass, input), valueMatch(byClass, ahead)) {
				return true
			}
		}
	case 3:
		nBack := int(t.u16(2))
		in := 4 + 2*nBack
		nInput := int(t.u16(in))
		ah := in + 2 + 2*nInput
		nAhead := int(t.u16(ah))
		rec := ah + 2 + 2*nAhead
		if nInput == 0 || t.sub(in+2).coverage(g) < 0 {
			return false
		}
		covAt := func(base int) matchFunc {
			return func(info *Info, i int) bool { return t.sub(base+2*i).coverage(uint16(info.Glyph)) >= 0 }
		}
		return c.chainMatch(nInput, nBack, nAhead, covAt(in+4), covAt(4), covAt(ah+2),
			sequenceLookups(t, rec+2, int(t.u16(rec))))
	}
	return false
}

// applyRule matches and applies a context rule; back and ahead are nil
// for a plain context rule.
func (c *applyContext) applyRule(r rule, back, input, ahead func(*Info, uint16) bool) bool {
	if r.nInput == 0 {
		return false
	}
	in := func(info *Info, i int) bool { return input(info, r.value(r.input, i)) }
	if back == nil {
		end, _, ok := c.matchInput(r.nInput, in)
		if !ok {
			return false
		}
		c.applyRecords(r.nInput, end, r.records)
		return true
	}
	return c.chainMatch(r.nInput, r.nBack, r.nAhead, in,
		func(info *Info, i int) bool { return back(info, r.value(r.back, i)) },
		func(info *Info, i int) bool { return ahead(info, r.value(r.ahead, i)) },
		r.records)
}

// chainMatch matches input, then lookahead, then backtrack, and applies
// the records.
func (c *applyContext) chainMatch(nInput, nBack, nAhead int, input, back, ahead matchFunc, records []sequenceLookup) bool {
	end, _, ok := c.matchInput(nInput, input)
	if !ok {
		return false
	}
	if _, ok := c.matchLookahead(nAhead, ahead, end); !ok {
		return false
	}
	if _, ok := c.matchBacktrack(nBack, back); !ok {
		return false
	}
	c.applyRecords(nInput, end, records)
	return true
}
