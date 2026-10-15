package theme

// Each element of a theme document resolved (Rust src/theme/resolve/
// parts.rs): lengths and strengths checked, colours and paints looked up,
// parts that may be off turned into flags.

func (r *resolver) window(w node) Window {
	rows := need[uint32](r, w, "rows", "window.rows")
	if _, given := w["rows"]; given && rows == 0 {
		r.fail("window.rows: show at least one row")
	}
	return Window{
		Width:  r.positive(w, "width", "window.width"),
		Height: r.positive(w, "height", "window.height"),
		Rows:   rows,
		Margin: r.length(w, "margin", "window.margin"),
	}
}

func (r *resolver) glow(g node) Glow {
	return Glow{
		Color:  r.colour(g, "color", "glow.color"),
		Radius: r.length(g, "radius", "glow.radius"),
		Pulse:  r.unit(g, "pulse", "glow.pulse"),
		Period: r.millis(g, "period-ms", "glow.period-ms"),
	}
}

func (r *resolver) border(b node) Border {
	var out Border
	if h := need[toggle](r, b, "highlight", "border.highlight"); !h.off && h.on != nil {
		out.HasHighlight = true
		out.Highlight = Highlight{
			Color:  r.colour(h.on, "color", "border.highlight.color"),
			Length: r.length(h.on, "length", "border.highlight.length"),
			Period: r.millis(h.on, "period-ms", "border.highlight.period-ms"),
		}
	}
	out.Width = r.length(b, "width", "border.width")
	out.Paint = r.paint(b, "paint", "border.paint")
	return out
}

func (r *resolver) panel(p node) Panel {
	return Panel{
		Fill:    r.paint(p, "fill", "panel.fill"),
		Radius:  r.length(p, "radius", "panel.radius"),
		Padding: r.length(p, "padding", "panel.padding"),
		Blur:    need[bool](r, p, "blur", "panel.blur"),
	}
}

func (r *resolver) rivets(d node) (bool, Rivets) {
	t := need[toggle](r, d, "rivets", "decorations.rivets")
	if t.off || t.on == nil {
		return false, Rivets{}
	}
	return true, Rivets{
		Size:   r.length(t.on, "size", "decorations.rivets.size"),
		Inset:  r.length(t.on, "inset", "decorations.rivets.inset"),
		Top:    r.paint(t.on, "top", "decorations.rivets.top"),
		Bottom: r.paint(t.on, "bottom", "decorations.rivets.bottom"),
	}
}

func (r *resolver) prompt(p node) Prompt {
	var out Prompt
	if s := need[toggle](r, p, "sigil", "prompt.sigil"); !s.off && s.on != nil {
		out.HasSigil = true
		out.Sigil = Sigil{
			Image: need[string](r, s.on, "image", "prompt.sigil.image"),
			Size:  r.positive(s.on, "size", "prompt.sigil.size"),
			Color: r.colour(s.on, "color", "prompt.sigil.color"),
		}
	}
	out.Height = r.positive(p, "height", "prompt.height")
	out.Label = need[string](r, p, "label", "prompt.label")
	out.Font = r.role(p, "font", "prompt.font")
	out.Color = r.colour(p, "color", "prompt.color")
	out.Gap = r.length(p, "gap", "prompt.gap")
	return out
}

func (r *resolver) query(q node) Query {
	c := need[node](r, q, "caret", "query.caret")
	return Query{
		Font:        r.role(q, "font", "query.font"),
		Color:       r.colour(q, "color", "query.color"),
		Placeholder: r.colour(q, "placeholder", "query.placeholder"),
		Caret: Caret{
			Shape: need[CaretShape](r, c, "shape", "query.caret.shape"),
			Width: r.length(c, "width", "query.caret.width"),
			Color: r.colour(c, "color", "query.caret.color"),
			Glow:  r.colour(c, "glow", "query.caret.glow"),
		},
	}
}

func (r *resolver) counter(c node) Counter {
	return Counter{
		Font:   r.role(c, "font", "counter.font"),
		Color:  r.colour(c, "color", "counter.color"),
		Format: need[string](r, c, "format", "counter.format"),
	}
}

func (r *resolver) rule(ru node) Rule {
	var out Rule
	if d := need[toggle](r, ru, "dash", "rule.dash"); !d.off && d.on == nil && r.err == nil {
		out.Dashed = true
		out.Dash = [2]float32{r.checkPositive(d.pair[0], "rule.dash[0]"), r.checkLength(d.pair[1], "rule.dash[1]")}
	}
	if o := need[toggle](r, ru, "ornament", "rule.ornament"); !o.off && o.on != nil {
		out.HasOrnament = true
		out.Ornament = Ornament{
			Size: r.length(o.on, "size", "rule.ornament.size"),
			Fill: r.colour(o.on, "fill", "rule.ornament.fill"),
			Edge: r.colour(o.on, "edge", "rule.ornament.edge"),
		}
	}
	out.Paint = r.paint(ru, "paint", "rule.paint")
	out.Thickness = r.length(ru, "thickness", "rule.thickness")
	out.Inset = r.length(ru, "inset", "rule.inset")
	return out
}

func (r *resolver) row(ro node, font Font) Row {
	out := Row{
		Font:      font,
		Height:    r.positive(ro, "height", "row.height"),
		Gap:       r.length(ro, "gap", "row.gap"),
		Padding:   r.length(ro, "padding", "row.padding"),
		Columns:   r.colours(ro, "columns", "row.columns"),
		Glyph:     r.colour(ro, "glyph", "row.glyph"),
		ColumnGap: r.length(ro, "column-gap", "row.column-gap"),
	}
	// A share of a whole: more than 0, at most 1.
	out.ColumnMax = need[float32](r, ro, "column-max", "row.column-max")
	if !(out.ColumnMax > 0 && out.ColumnMax <= 1) {
		r.fail("row.column-max: %s is no share of the row (more than 0, at most 1)", show(out.ColumnMax))
	}
	return out
}

func (r *resolver) selection(s node) Selection {
	return Selection{
		Band:    r.paint(s, "band", "selection.band"),
		Outline: r.colour(s, "outline", "selection.outline"),
		Edge:    r.colour(s, "edge", "selection.edge"),
		Glow:    r.colour(s, "glow", "selection.glow"),
		Columns: r.colours(s, "columns", "selection.columns"),
		Inset:   r.length(s, "inset", "selection.inset"),
	}
}

func (r *resolver) badge(b node) Badge {
	return Badge{
		Font:      r.role(b, "font", "badge.font"),
		Style:     need[BadgeStyle](r, b, "style", "badge.style"),
		Ink:       r.colour(b, "ink", "badge.ink"),
		Edge:      r.colour(b, "edge", "badge.edge"),
		Fill:      r.colour(b, "fill", "badge.fill"),
		Uppercase: need[bool](r, b, "uppercase", "badge.uppercase"),
	}
}

func (r *resolver) pending(p node) Pending {
	return Pending{
		Color:   r.colour(p, "color", "pending.color"),
		Opacity: r.unit(p, "opacity", "pending.opacity"),
		Shimmer: r.unit(p, "shimmer", "pending.shimmer"),
		Period:  r.millis(p, "period-ms", "pending.period-ms"),
		Mark:    need[string](r, p, "mark", "pending.mark"),
	}
}

func (r *resolver) motion(m node) Motion {
	return Motion{
		Open:   r.millis(m, "open-ms", "motion.open-ms"),
		Close:  r.millis(m, "close-ms", "motion.close-ms"),
		Select: r.millis(m, "select-ms", "motion.select-ms"),
	}
}
