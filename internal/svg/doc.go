package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	svgNS   = "http://www.w3.org/2000/svg"
	xlinkNS = "http://www.w3.org/1999/xlink"
)

// node is one SVG element. Elements of other namespaces (editor metadata)
// are dropped while parsing.
type node struct {
	name     string
	attrs    map[string]string
	children []*node
	parent   *node
	// text is the character data of a <style> element.
	text string
	// props are the element's declared presentation properties after the
	// cascade: attributes, then stylesheet rules, then its style attribute.
	props map[string]string
}

func (n *node) attr(name string) string { return n.attrs[name] }

// Doc is a parsed SVG document, ready to render any number of times.
type Doc struct {
	root *node
	ids  map[string]*node
	// width and height are the document's size in px; view maps user
	// space onto it.
	width, height float64
	view          Matrix
	// unsupported collects what parsing met but logis does not draw.
	unsupported map[string]bool
}

// Parse reads an SVG document. It fails for documents that are not
// well-formed XML, whose root is not an svg element, or whose size is not
// positive.
func Parse(data []byte) (*Doc, error) {
	root, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	d := &Doc{root: root, ids: map[string]*node{}, unsupported: map[string]bool{}}
	var sheet []rule
	walk(root, func(n *node) {
		if id := n.attr("id"); id != "" {
			if _, dup := d.ids[id]; !dup {
				d.ids[id] = n
			}
		}
		if n.name == "style" {
			if t := n.attr("type"); t == "" || t == "text/css" {
				rules, ok := parseStylesheet(n.text)
				if !ok {
					d.unsupported["css selector"] = true
				}
				sheet = append(sheet, rules...)
			}
		}
	})
	cascade(root, sheet)
	if err := d.size(); err != nil {
		return nil, err
	}
	return d, nil
}

func walk(n *node, f func(*node)) {
	f(n)
	for _, c := range n.children {
		walk(c, f)
	}
}

var entityDecl = regexp.MustCompile(`<!ENTITY\s+([A-Za-z_][\w.-]*)\s+(?:"([^"]*)"|'([^']*)')\s*>`)

func parseTree(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Entity = map[string]string{}
	var root *node
	var stack []*node
	skip := 0 // depth inside an element of another namespace
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the SVG is not well-formed XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			for _, m := range entityDecl.FindAllStringSubmatch(string(t), -1) {
				dec.Entity[m[1]] = m[2] + m[3]
			}
		case xml.StartElement:
			if skip > 0 || (t.Name.Space != svgNS && t.Name.Space != "") {
				if root == nil {
					return nil, fmt.Errorf("the document's root is <%s>, not an SVG <svg>", t.Name.Local)
				}
				skip++
				continue
			}
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				switch a.Name.Space {
				case "", svgNS:
					n.attrs[a.Name.Local] = a.Value
				case xlinkNS:
					if a.Name.Local == "href" {
						if _, set := n.attrs["href"]; !set {
							n.attrs["href"] = a.Value
						}
					}
				}
			}
			if root == nil {
				if n.name != "svg" {
					return nil, fmt.Errorf("the document's root is <%s>, not an SVG <svg>", n.name)
				}
				root = n
			} else if len(stack) > 0 {
				n.parent = stack[len(stack)-1]
				n.parent.children = append(n.parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if skip > 0 {
				skip--
			} else if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if skip == 0 && len(stack) > 0 && stack[len(stack)-1].name == "style" {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("the document holds no SVG <svg> element")
	}
	return root, nil
}

// size sets the document size and the view box mapping (usvg's rules:
// width and height default to the view box's, percentages are of it, and
// without either the document is 100×100).
func (d *Doc) size() error {
	vb, hasVB := parseViewBox(d.root.attr("viewBox"))
	if d.root.attr("viewBox") != "" && !hasVB {
		return errors.New("the SVG's viewBox is not a positive rectangle")
	}
	dim := func(attr string, vbSide float64) (float64, error) {
		s := strings.TrimSpace(d.root.attr(attr))
		ref := 100.0
		if hasVB {
			ref = vbSide
		}
		if s == "" {
			return ref, nil
		}
		v, ok := parseLength(s, ref, defaultFontSize)
		if !ok || !(v > 0) {
			return 0, fmt.Errorf("the SVG's %s %q is not a positive length", attr, s)
		}
		return v, nil
	}
	var err error
	if d.width, err = dim("width", vb[2]); err != nil {
		return err
	}
	if d.height, err = dim("height", vb[3]); err != nil {
		return err
	}
	d.view = Identity
	if hasVB {
		d.view = viewBoxTransform(vb, d.root.attr("preserveAspectRatio"), d.width, d.height)
	}
	return nil
}

// Size is the document's width and height in px.
func (d *Doc) Size() (w, h float64) { return d.width, d.height }

func parseViewBox(s string) ([4]float64, bool) {
	v, ok := numbers(s)
	if !ok || len(v) != 4 || !(v[2] > 0) || !(v[3] > 0) {
		return [4]float64{}, false
	}
	return [4]float64(v), true
}

// viewBoxTransform maps the view box vb onto a w×h viewport following
// preserveAspectRatio (default xMidYMid meet).
func viewBoxTransform(vb [4]float64, par string, w, h float64) Matrix {
	sx, sy := w/vb[2], h/vb[3]
	fields := strings.Fields(par)
	align, slice := "xMidYMid", false
	if len(fields) > 0 && fields[0] == "defer" {
		fields = fields[1:]
	}
	if len(fields) > 0 {
		align = fields[0]
	}
	if len(fields) > 1 && fields[1] == "slice" {
		slice = true
	}
	if align == "none" {
		return Scale(sx, sy).Mul(Translate(-vb[0], -vb[1]))
	}
	s := min(sx, sy)
	if slice {
		s = max(sx, sy)
	}
	tx, ty := -vb[0]*s, -vb[1]*s
	free := func(axis string, room float64) float64 {
		switch {
		case strings.Contains(align, axis+"Mid"):
			return room / 2
		case strings.Contains(align, axis+"Max"):
			return room
		}
		return 0
	}
	tx += free("x", w-vb[2]*s)
	ty += free("Y", h-vb[3]*s)
	return Matrix{s, 0, 0, s, tx, ty}
}
