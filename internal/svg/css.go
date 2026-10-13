package svg

import (
	"slices"
	"strings"
)

// The cascade: presentation attributes, then <style> rules by specificity
// and order, then the style attribute. Selectors are compounds of a type,
// classes and an id, joined by descendant or child combinators; rules with
// anything else are skipped and reported.

// properties are the presentation attributes logis reads.
var properties = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-opacity": true,
	"stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true,
	"opacity": true, "color": true, "display": true, "visibility": true,
	"stop-color": true, "stop-opacity": true, "clip-path": true, "clip-rule": true,
	"mask": true, "filter": true, "marker-start": true, "marker-mid": true,
	"marker-end": true, "marker": true, "font-size": true, "paint-order": true,
	"mix-blend-mode": true, "vector-effect": true,
}

type declaration struct{ name, value string }

type compound struct {
	tag, id string
	classes []string
	// child: the next compound to the left must be the parent, not any
	// ancestor.
	child bool
}

type rule struct {
	// selector is right to left: the subject first.
	selector    []compound
	specificity [3]int
	order       int
	decls       []declaration
}

func parseDeclarations(s string) []declaration {
	var out []declaration
	for _, part := range strings.Split(s, ";") {
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
		out = append(out, declaration{strings.ToLower(strings.TrimSpace(name)), value})
	}
	return out
}

// parseStylesheet reads rules; !ok when a rule was skipped.
func parseStylesheet(s string) ([]rule, bool) {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+2+j+2:]
	}
	s = strings.NewReplacer("<![CDATA[", " ", "]]>", " ").Replace(s)
	var rules []rule
	ok := true
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return rules, ok
		}
		open := strings.IndexByte(s, '{')
		if open < 0 {
			return rules, ok
		}
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			end = len(s) - open - 1
		}
		head, body := strings.TrimSpace(s[:open]), s[open+1:open+end]
		s = s[min(open+end+1, len(s)):]
		if strings.HasPrefix(head, "@") {
			ok = false // @media and friends: skipped with their first block
			continue
		}
		decls := parseDeclarations(body)
		for _, sel := range strings.Split(head, ",") {
			r, good := parseSelector(strings.TrimSpace(sel))
			if !good {
				ok = false
				continue
			}
			r.decls, r.order = decls, len(rules)
			rules = append(rules, r)
		}
	}
}

func parseSelector(s string) (rule, bool) {
	var r rule
	child := false
	for _, word := range strings.Fields(strings.ReplaceAll(s, ">", " > ")) {
		if word == ">" {
			child = true
			continue
		}
		c, ok := parseCompound(word)
		if !ok {
			return r, false
		}
		if len(r.selector) > 0 {
			r.selector[len(r.selector)-1].child = child
		}
		child = false
		r.selector = append(r.selector, c)
		r.specificity[1] += len(c.classes)
		if c.id != "" {
			r.specificity[0]++
		}
		if c.tag != "" && c.tag != "*" {
			r.specificity[2]++
		}
	}
	slices.Reverse(r.selector)
	return r, len(r.selector) > 0
}

func parseCompound(s string) (compound, bool) {
	var c compound
	i := strings.IndexAny(s, ".#")
	if i < 0 {
		i = len(s)
	}
	c.tag = s[:i]
	for s = s[i:]; s != ""; {
		kind := s[0]
		j := strings.IndexAny(s[1:], ".#") + 1
		if j == 0 {
			j = len(s)
		}
		name := s[1:j]
		if name == "" {
			return c, false
		}
		if kind == '.' {
			c.classes = append(c.classes, name)
		} else {
			c.id = name
		}
		s = s[j:]
	}
	for _, r := range c.tag + strings.Join(c.classes, "") + c.id {
		if !identRune(r) {
			return c, false // attribute selectors, pseudo-classes, ...
		}
	}
	return c, true
}

func (c compound) matches(n *node) bool {
	if c.tag != "" && c.tag != "*" && c.tag != n.name {
		return false
	}
	if c.id != "" && n.attr("id") != c.id {
		return false
	}
	classes := strings.Fields(n.attr("class"))
	for _, want := range c.classes {
		if !slices.Contains(classes, want) {
			return false
		}
	}
	return true
}

func (r *rule) matches(n *node) bool {
	if !r.selector[0].matches(n) {
		return false
	}
	at := n
	for i := 1; i < len(r.selector); i++ {
		direct := r.selector[i-1].child
		at = at.parent
		for at != nil && !r.selector[i].matches(at) {
			if direct {
				return false
			}
			at = at.parent
		}
		if at == nil {
			return false
		}
	}
	return true
}

// cascade sets every node's props.
func cascade(root *node, sheet []rule) {
	walk(root, func(n *node) {
		n.props = map[string]string{}
		for name, v := range n.attrs {
			if properties[name] {
				n.props[name] = strings.TrimSpace(v)
			}
		}
		var matched []*rule
		for i := range sheet {
			if sheet[i].matches(n) {
				matched = append(matched, &sheet[i])
			}
		}
		slices.SortStableFunc(matched, func(a, b *rule) int {
			if c := slices.Compare(a.specificity[:], b.specificity[:]); c != 0 {
				return c
			}
			return a.order - b.order
		})
		for _, r := range matched {
			for _, d := range r.decls {
				n.props[d.name] = d.value
			}
		}
		for _, d := range parseDeclarations(n.attr("style")) {
			n.props[d.name] = d.value
		}
	})
}

// identRune: a rune selectors may name things with.
func identRune(r rune) bool {
	return r == '-' || r == '_' || r == '*' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127
}
