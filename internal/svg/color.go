package svg

import (
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/colornames"
)

// parseColor reads a CSS colour: a name (SVG 1.1's keywords from
// x/image/colornames, plus transparent and rebeccapurple), #rgb, #rgba,
// #rrggbb, #rrggbbaa, rgb()/rgba() and hsl()/hsla().
func parseColor(s string) (color, bool) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if lower == "transparent" {
		return color{}, true
	}
	if lower == "rebeccapurple" {
		return color{0x66 / 255.0, 0x33 / 255.0, 0x99 / 255.0, 1}, true
	}
	if c, ok := colornames.Map[lower]; ok {
		return color{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, 1}, true
	}
	if hex, ok := strings.CutPrefix(s, "#"); ok {
		return parseHex(hex)
	}
	open := strings.IndexByte(lower, '(')
	if open < 0 || !strings.HasSuffix(lower, ")") {
		return color{}, false
	}
	fn := strings.TrimSpace(lower[:open])
	args := strings.FieldsFunc(lower[open+1:len(lower)-1], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(args) != 3 && len(args) != 4 {
		return color{}, false
	}
	c := color{a: 1}
	if len(args) == 4 {
		a, ok := parseOpacity(args[3])
		if !ok {
			return color{}, false
		}
		c.a = a
	}
	switch fn {
	case "rgb", "rgba":
		ch := [3]*float64{&c.r, &c.g, &c.b}
		for i, a := range args[:3] {
			pct := strings.HasSuffix(a, "%")
			v, err := strconv.ParseFloat(strings.TrimSuffix(a, "%"), 64)
			if err != nil {
				return color{}, false
			}
			if pct {
				v *= 2.55
			}
			*ch[i] = min(max(v, 0), 255) / 255
		}
		return c, true
	case "hsl", "hsla":
		h, e1 := strconv.ParseFloat(strings.TrimSuffix(args[0], "deg"), 64)
		sat, ok1 := parseOpacity(args[1])
		l, ok2 := parseOpacity(args[2])
		if e1 != nil || !ok1 || !ok2 {
			return color{}, false
		}
		c.r, c.g, c.b = hsl(h, sat, l)
		return c, true
	}
	return color{}, false
}

func parseHex(h string) (color, bool) {
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, r := range h {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		h = b.String()
	}
	if len(h) == 6 {
		h += "ff"
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if len(h) != 8 || err != nil {
		return color{}, false
	}
	return color{float64(v>>24) / 255, float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255}, true
}

func hsl(h, s, l float64) (r, g, b float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	q := l + s - l*s
	if l < 0.5 {
		q = l * (1 + s)
	}
	p := 2*l - q
	hue := func(t float64) float64 {
		t = math.Mod(t+1, 1)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return hue(h + 1.0/3), hue(h), hue(h - 1.0/3)
}
