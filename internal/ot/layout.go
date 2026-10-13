package ot

// Lookup flags.
const (
	FlagRightToLeft       = 0x0001
	FlagIgnoreBaseGlyphs  = 0x0002
	FlagIgnoreLigatures   = 0x0004
	FlagIgnoreMarks       = 0x0008
	FlagIgnoreFlags       = 0x000E
	FlagUseMarkFilterSet  = 0x0010
	FlagMarkAttachTypeMsk = 0xFF00
)

// Layout is a GSUB or GPOS table.
type Layout struct {
	t, scripts, features table
	gsub                 bool
	lookups              []lookup
}

// lookup is one lookup with extension subtables resolved: Props holds
// its flags and, above bit 16, its mark filtering set.
type lookup struct {
	kind      int
	props     uint32
	subtables []table
	extension bool
}

func parseLayout(t table, gsub bool) *Layout {
	if t.u16(0) != 1 {
		return nil
	}
	l := &Layout{t: t, scripts: t.sub(4), features: t.sub(6), gsub: gsub}
	list := t.sub(8)
	n := int(list.u16(0))
	l.lookups = make([]lookup, 0, min(n, len(list)/2))
	extension := 9
	if gsub {
		extension = 7
	}
	for i := range n {
		lt := list.sub(2 + 2*i)
		lk := lookup{kind: int(lt.u16(0)), props: uint32(lt.u16(2))}
		count := int(lt.u16(4))
		if lk.props&FlagUseMarkFilterSet != 0 {
			lk.props |= uint32(lt.u16(6+2*count)) << 16
		}
		isExtension := lk.kind == extension
		lk.extension = isExtension
		for j := range count {
			st := lt.sub(6 + 2*j)
			if isExtension {
				// Each extension subtable wraps one of the real type,
				// which the first names for the whole lookup.
				if j == 0 {
					lk.kind = int(st.u16(2))
				}
				st = st.sub32(4)
			}
			if st != nil {
				lk.subtables = append(lk.subtables, st)
			}
		}
		if lk.kind == extension {
			lk.kind = 0 // an extension of extensions
		}
		l.lookups = append(l.lookups, lk)
	}
	return l
}

// LookupCount is the number of lookups.
func (l *Layout) LookupCount() int { return len(l.lookups) }

// LookupKind is lookup i's type (extensions resolved), 0 when unknown.
func (l *Layout) LookupKind(i int) int {
	if i < 0 || i >= len(l.lookups) {
		return 0
	}
	return l.lookups[i].kind
}

// Script is a script selected for shaping: its index in the script list,
// its tag, and whether it was found or is a fallback (DFLT, dflt, latn).
type Script struct {
	Index    int
	Tag      Tag
	Fallback bool
}

// SelectScript picks the first of tags the table has, else its DFLT,
// dflt or latn script; false when the table has none of them.
func (l *Layout) SelectScript(tags []Tag) (Script, bool) {
	for _, tag := range tags {
		if i := l.scriptIndex(tag); i >= 0 {
			return Script{Index: i, Tag: tag}, true
		}
	}
	for _, s := range []string{"DFLT", "dflt", "latn"} {
		if i := l.scriptIndex(MakeTag(s)); i >= 0 {
			return Script{Index: i, Tag: MakeTag(s), Fallback: true}, true
		}
	}
	return Script{}, false
}

// scriptIndex finds tag among the script records (sorted by tag).
func (l *Layout) scriptIndex(tag Tag) int {
	return findTag(l.scripts, 2, int(l.scripts.u16(0)), 6, tag)
}

func findTag(t table, first, n, size int, tag Tag) int {
	lo, hi := 0, min(n, max(0, len(t)-first)/size)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch v := Tag(t.u32(first + size*m)); {
		case v < tag:
			lo = m + 1
		case v > tag:
			hi = m
		default:
			return m
		}
	}
	return -1
}

// LangSys is a language system of a script: -1 for the script's default.
func (l *Layout) langSys(script, lang int) table {
	s := l.scripts.sub(2 + 6*script + 4)
	if lang < 0 {
		return s.sub(0)
	}
	return s.sub(4 + 6*lang + 4)
}

// SelectLanguage is the index of the first of tags the script has, else
// its dflt language system, else -1 (the default).
func (l *Layout) SelectLanguage(script int, tags []Tag) int {
	s := l.scripts.sub(2 + 6*script + 4)
	for _, tag := range tags {
		if i := findTag(s, 4, int(s.u16(2)), 6, tag); i >= 0 {
			return i
		}
	}
	return findTag(s, 4, int(s.u16(2)), 6, MakeTag("dflt"))
}

// RequiredFeature is the required feature of a language system and its
// tag, -1 for none.
func (l *Layout) RequiredFeature(script, lang int) (int, Tag) {
	i := l.langSys(script, lang).u16(2)
	if i == 0xFFFF || int(i) >= l.FeatureCount() {
		return -1, 0
	}
	return int(i), l.FeatureTag(int(i))
}

// FindFeature is the index of the language system's feature tag, -1
// when it has none.
func (l *Layout) FindFeature(script, lang int, tag Tag) int {
	ls := l.langSys(script, lang)
	for i := range int(ls.u16(4)) {
		f := int(ls.u16(6 + 2*i))
		if f < l.FeatureCount() && l.FeatureTag(f) == tag {
			return f
		}
	}
	return -1
}

// FeatureIndex is the first feature with tag in the whole feature list.
func (l *Layout) FeatureIndex(tag Tag) int {
	for i := range l.FeatureCount() {
		if l.FeatureTag(i) == tag {
			return i
		}
	}
	return -1
}

// FeatureCount is the number of feature records.
func (l *Layout) FeatureCount() int {
	return min(int(l.features.u16(0)), max(0, len(l.features)-2)/6)
}

// FeatureTag is feature i's tag.
func (l *Layout) FeatureTag(i int) Tag { return Tag(l.features.u32(2 + 6*i)) }

// FeatureLookups lists feature i's lookup indices that exist.
func (l *Layout) FeatureLookups(i int) []int {
	f := l.features.sub(2 + 6*i + 4)
	n := int(f.u16(2))
	out := make([]int, 0, n)
	for j := range n {
		if k := int(f.u16(4 + 2*j)); k < len(l.lookups) {
			out = append(out, k)
		}
	}
	return out
}
