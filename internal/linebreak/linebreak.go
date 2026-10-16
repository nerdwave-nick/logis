// Package linebreak finds line break opportunities by the Unicode Line
// Breaking Algorithm (UAX #14) of Unicode 17, with the default rules and
// the default resolution of AI, SA, SG, XX and CJ. logis never wraps; it
// splits a line into words where it could break, as cosmic-text does
// before shaping. The classes come from internal/ucd; the breaker passes
// the whole of UCD 17's LineBreakTest.txt.
package linebreak

import (
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/ucd"
)

// Line_Break classes, in internal/ucd's order.
const (
	xx = iota
	bk
	cr
	lf
	cm
	nl
	sg
	wj
	zw
	gl
	sp
	zwj
	b2
	ba
	bb
	hy
	cb
	cl
	cp
	ex
	in
	ns
	op
	qu
	is
	nu
	po
	pr
	sy
	ai
	ak
	al
	ap
	as
	cj
	eb
	em
	h2
	h3
	hl
	id
	jl
	jv
	jt
	ri
	sa
	vf
	vi
	hh
)

// item is a character with the combining marks rule LB9 attaches to it.
type item struct {
	class     uint8
	gc        uint8
	eastAsian bool
	extPictCn bool // Extended_Pictographic and unassigned
	dotted    bool // U+25CC DOTTED CIRCLE
	endsZWJ   bool // its last character is a ZWJ (rule LB8a)
	end       int  // byte offset after it
}

// Breaks lists the byte offsets in s where a line may break: after each
// break opportunity, ending with len(s) for non-empty s.
func Breaks(s string) []int {
	items := itemize(s)
	var out []int
	for j := 1; j < len(items); j++ {
		if breaksBefore(items, j) {
			out = append(out, items[j-1].end)
		}
	}
	if len(items) > 0 {
		out = append(out, len(s))
	}
	return out
}

// resolve is rule LB1.
func resolve(r rune) (uint8, uint8) {
	c, gc := ucd.LineBreak(r), ucd.GeneralCategory(r)
	switch c {
	case ai, sg, xx:
		c = al
	case sa:
		c = al
		if gc == ucd.Mn || gc == ucd.Mc {
			c = cm
		}
	case cj:
		c = ns
	}
	return c, gc
}

// itemize applies rules LB9 and LB10: combining marks and ZWJ join the
// character before them, except after breaks and spaces, where they
// count as alphabetic.
func itemize(s string) []item {
	var items []item
	for i, r := range s {
		end := i + utf8.RuneLen(r)
		if r == utf8.RuneError && end > len(s) {
			end = len(s)
		}
		c, gc := resolve(r)
		if c == cm || c == zwj {
			if n := len(items); n > 0 {
				switch items[n-1].class {
				case bk, cr, lf, nl, sp, zw:
				default:
					items[n-1].end = end
					items[n-1].endsZWJ = c == zwj
					continue
				}
			}
			items = append(items, item{class: al, gc: ucd.Lu, end: end, endsZWJ: c == zwj})
			continue
		}
		w := ucd.EastAsianWidth(r)
		items = append(items, item{
			class:     c,
			gc:        gc,
			eastAsian: w == ucd.EAWFullwidth || w == ucd.EAWWide || w == ucd.EAWHalfwidth,
			extPictCn: gc == ucd.Cn && ucd.ExtendedPictographic(r),
			dotted:    r == 0x25CC,
			end:       end,
		})
	}
	return items
}

func oneOf(c uint8, set ...uint8) bool {
	for _, s := range set {
		if c == s {
			return true
		}
	}
	return false
}

// beforeSpaces is the index of the item before the spaces that end just
// before j, -1 at the start of text.
func beforeSpaces(items []item, j int) int {
	k := j - 1
	for k >= 0 && items[k].class == sp {
		k--
	}
	return k
}

// breaksBefore applies rules LB4–LB31 to the boundary before item j.
func breaksBefore(items []item, j int) bool {
	a, b := &items[j-1], &items[j]
	A, B := a.class, b.class
	switch {
	case A == bk: // LB4
		return true
	case A == cr && B == lf: // LB5
		return false
	case oneOf(A, cr, lf, nl):
		return true
	case oneOf(B, bk, cr, lf, nl): // LB6
		return false
	case B == sp || B == zw: // LB7
		return false
	}
	k := beforeSpaces(items, j)
	if k >= 0 && items[k].class == zw { // LB8
		return true
	}
	if a.endsZWJ { // LB8a
		return false
	}
	return tailored(items, j, k, a, b)
}

// tailored applies the tailorable rules LB11–LB31.
func tailored(items []item, j, k int, a, b *item) bool {
	A, B := a.class, b.class
	var K uint8 = 255 // the class before the spaces
	if k >= 0 {
		K = items[k].class
	}
	var next *item
	if j+1 < len(items) {
		next = &items[j+1]
	}
	switch {
	case A == wj || B == wj: // LB11
		return false
	case A == gl: // LB12
		return false
	case B == gl && !oneOf(A, sp, ba, hy, hh): // LB12a
		return false
	case oneOf(B, cl, cp, ex, sy): // LB13
		return false
	case K == op: // LB14
		return false
	case k >= 0 && K == qu && items[k].gc == ucd.Pi &&
		(k == 0 || oneOf(items[k-1].class, bk, cr, lf, nl, op, qu, gl, sp, zw)): // LB15a
		return false
	case B == qu && b.gc == ucd.Pf &&
		(next == nil || oneOf(next.class, sp, gl, wj, cl, qu, cp, ex, is, sy, bk, cr, lf, nl, zw)): // LB15b
		return false
	case A == sp && B == is && next != nil && next.class == nu: // LB15c
		return true
	case B == is: // LB15d
		return false
	case oneOf(K, cl, cp) && B == ns: // LB16
		return false
	case K == b2 && B == b2: // LB17
		return false
	case A == sp: // LB18
		return true
	}
	return pairs(items, j, a, b, next)
}

// pairs applies rules LB19–LB31, which look at the characters around the
// boundary only.
func pairs(items []item, j int, a, b, next *item) bool {
	A, B := a.class, b.class
	switch {
	case B == qu && b.gc != ucd.Pi, A == qu && a.gc != ucd.Pf: // LB19
		return false
	case B == qu && (!a.eastAsian || next == nil || !next.eastAsian), // LB19a
		A == qu && (!b.eastAsian || j-2 < 0 || !items[j-2].eastAsian):
		return false
	case A == cb || B == cb: // LB20
		return true
	case oneOf(A, hy, hh) && oneOf(B, al, hl) &&
		(j-2 < 0 || oneOf(items[j-2].class, bk, cr, lf, nl, sp, zw, cb, gl)): // LB20a
		return false
	case oneOf(B, ba, hh, hy, ns), A == bb: // LB21
		return false
	case oneOf(A, hy, hh) && j-2 >= 0 && items[j-2].class == hl && B != hl: // LB21a
		return false
	case A == sy && B == hl, B == in: // LB21b, LB22
		return false
	case oneOf(A, al, hl) && B == nu, A == nu && oneOf(B, al, hl): // LB23
		return false
	case A == pr && oneOf(B, id, eb, em), oneOf(A, id, eb, em) && B == po: // LB23a
		return false
	case oneOf(A, pr, po) && oneOf(B, al, hl), oneOf(A, al, hl) && oneOf(B, pr, po): // LB24
		return false
	case number(items, j, a, b, next): // LB25
		return false
	case korean(A, B): // LB26, LB27
		return false
	case oneOf(A, al, hl) && oneOf(B, al, hl): // LB28
		return false
	case brahmic(items, j, a, b, next): // LB28a
		return false
	case A == is && oneOf(B, al, hl): // LB29
		return false
	case oneOf(A, al, hl, nu) && B == op && !b.eastAsian, // LB30
		A == cp && !a.eastAsian && oneOf(B, al, hl, nu):
		return false
	case A == ri && B == ri: // LB30a: break between pairs only
		n := 0
		for k := j - 1; k >= 0 && items[k].class == ri; k-- {
			n++
		}
		return n%2 == 0
	case B == em && (A == eb || a.extPictCn): // LB30b
		return false
	}
	return true // LB31
}

// number is rule LB25.
func number(items []item, j int, a, b, next *item) bool {
	A, B := a.class, b.class
	// NU (SY | IS)* (CL | CP)? × (PO | PR), and NU (SY | IS)* × NU.
	if oneOf(B, po, pr, nu) {
		k := j - 1
		if oneOf(B, po, pr) && oneOf(items[k].class, cl, cp) {
			k--
		}
		for k >= 0 && oneOf(items[k].class, sy, is) {
			k--
		}
		if k >= 0 && items[k].class == nu {
			return true
		}
	}
	switch {
	case oneOf(A, po, pr) && B == op && next != nil:
		if next.class == nu {
			return true
		}
		return next.class == is && j+2 < len(items) && items[j+2].class == nu
	case oneOf(A, po, pr, hy, is) && B == nu:
		return true
	}
	return false
}

// korean is rules LB26 and LB27.
func korean(A, B uint8) bool {
	switch {
	case A == jl && oneOf(B, jl, jv, h2, h3),
		oneOf(A, jv, h2) && oneOf(B, jv, jt),
		oneOf(A, jt, h3) && B == jt,
		oneOf(A, jl, jv, jt, h2, h3) && B == po,
		A == pr && oneOf(B, jl, jv, jt, h2, h3):
		return true
	}
	return false
}

// brahmic is rule LB28a: orthographic syllables of Brahmic scripts.
func brahmic(items []item, j int, a, b, next *item) bool {
	base := func(it *item) bool { return it.class == ak || it.class == as || it.dotted }
	switch {
	case a.class == ap && base(b):
		return true
	case base(a) && (b.class == vf || b.class == vi):
		return true
	case a.class == vi && j-2 >= 0 && base(&items[j-2]) && (b.class == ak || b.dotted):
		return true
	case base(a) && base(b) && next != nil && next.class == vf:
		return true
	}
	return false
}
