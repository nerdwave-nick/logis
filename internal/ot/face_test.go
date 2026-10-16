package ot

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var pinnedFonts = []string{
	"NotoSans-Regular.ttf",
	"NotoSansHebrew-Regular.ttf",
	"NotoSansArabic-Regular.ttf",
	"NotoSansCJKjp-Regular.otf",
	"NotoColorEmoji.ttf",
	"NotoSansMono-Regular.ttf",
	"NotoSerif-SemiBold.ttf",
}

func pinnedData(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fonts", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func pinned(t testing.TB, name string) *Face {
	t.Helper()
	f, err := Parse(pinnedData(t, name), 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

// The cmap and advances agree with golang.org/x/image/font/sfnt on every
// pinned font, for every character up to U+1FFFF and every glyph.
func TestCmapAndAdvancesAgreeWithSFNT(t *testing.T) {
	for _, name := range pinnedFonts {
		data := pinnedData(t, name)
		face := pinned(t, name)
		ref, err := sfnt.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		var buf sfnt.Buffer
		mapped := 0
		for r := rune(0); r < 0x20000; r++ {
			want, _ := ref.GlyphIndex(&buf, r)
			got, ok := face.Glyph(r)
			if got != uint16(want) || ok != (want != 0) {
				t.Fatalf("%s: U+%04X → %d %v, sfnt %d", name, r, got, ok, want)
			}
			if ok {
				mapped++
			}
		}
		if mapped < 8 {
			t.Fatalf("%s: only %d characters mapped", name, mapped)
		}
		upem := fixed.Int26_6(ref.UnitsPerEm())
		if face.UnitsPerEm() != int(upem) || face.NumGlyphs() != ref.NumGlyphs() {
			t.Fatalf("%s: upem %d glyphs %d, sfnt %d %d", name, face.UnitsPerEm(), face.NumGlyphs(), upem, ref.NumGlyphs())
		}
		for g := range face.NumGlyphs() {
			want, err := ref.GlyphAdvance(&buf, sfnt.GlyphIndex(g), upem, font.HintingNone)
			if err != nil {
				t.Fatal(err)
			}
			if got := face.Advance(uint16(g)); got != int(want) {
				t.Fatalf("%s: glyph %d advance %d, sfnt %d", name, g, got, want)
			}
		}
	}
}

func TestLayoutListsOfThePinnedFonts(t *testing.T) {
	sans := pinned(t, "NotoSans-Regular.ttf")
	gsub := sans.GSUB()
	if gsub == nil || gsub.LookupCount() != 41 || sans.GPOS().LookupCount() != 5 {
		t.Fatalf("Noto Sans layout: %+v", gsub)
	}
	s, ok := gsub.SelectScript([]Tag{MakeTag("latn")})
	if !ok || s.Tag != MakeTag("latn") || s.Fallback {
		t.Fatalf("latn: %+v %v", s, ok)
	}
	liga := gsub.FindFeature(s.Index, -1, MakeTag("liga"))
	if liga < 0 || !slices.Equal(gsub.FeatureLookups(liga), []int{36}) || gsub.LookupKind(36) != 4 {
		t.Fatalf("liga feature %d: lookups %v kind %d", liga, gsub.FeatureLookups(liga), gsub.LookupKind(36))
	}
	if f := gsub.FindFeature(s.Index, -1, MakeTag("kern")); f != -1 {
		t.Fatalf("GSUB has no kern, found %d", f)
	}
	// An unknown script falls back to DFLT.
	if s, ok := gsub.SelectScript([]Tag{MakeTag("thai")}); !ok || !s.Fallback || s.Tag != MakeTag("DFLT") {
		t.Fatalf("thai: %+v %v", s, ok)
	}
	pos := sans.GPOS()
	ps, _ := pos.SelectScript([]Tag{MakeTag("latn")})
	kern := pos.FindFeature(ps.Index, -1, MakeTag("kern"))
	if !slices.Equal(pos.FeatureLookups(kern), []int{1, 0}) || pos.LookupKind(0) != 2 {
		t.Fatalf("kern lookups %v kind %d", pos.FeatureLookups(kern), pos.LookupKind(0))
	}

	arabic := pinned(t, "NotoSansArabic-Regular.ttf").GSUB()
	as, _ := arabic.SelectScript([]Tag{MakeTag("arab")})
	for tag, want := range map[string]int{"init": 2, "medi": 2, "fina": 2, "rlig": 5} {
		f := arabic.FindFeature(as.Index, -1, MakeTag(tag))
		lookups := arabic.FeatureLookups(f)
		if len(lookups) != 1 || arabic.LookupKind(lookups[0]) != want {
			t.Errorf("arab %s: lookups %v", tag, lookups)
		}
	}
	if f := arabic.FindFeature(as.Index, -1, MakeTag("isol")); f != -1 {
		t.Errorf("arab has no isol, found %d", f)
	}
}

func TestGlyphPropsFromGDEF(t *testing.T) {
	hebrew := pinned(t, "NotoSansHebrew-Regular.ttf")
	if !hebrew.HasGlyphClasses() {
		t.Fatal("Noto Sans Hebrew has glyph classes")
	}
	shin, _ := hebrew.Glyph('ש')
	qamats, _ := hebrew.Glyph('ָ')
	if p := hebrew.GlyphProps(shin); p != PropBaseGlyph {
		t.Errorf("shin props %#x", p)
	}
	if p := hebrew.GlyphProps(qamats); p&PropClassMask != PropMark {
		t.Errorf("qamats props %#x", p)
	}
	if pinned(t, "NotoColorEmoji.ttf").HasGlyphClasses() {
		t.Error("the emoji subset has no GDEF")
	}
}

func TestMalformedFontsAreRefusedOrEmpty(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("OTTO"), []byte("ttcf\x00\x01\x00\x00\x00\x00\x00\x05")} {
		if _, err := Parse(data, 0); err == nil {
			t.Errorf("%q parsed", data)
		}
	}
	// A real font cut short anywhere parses without panicking or is refused.
	data := pinnedData(t, "NotoSansArabic-Regular.ttf")
	for n := 0; n < len(data); n += 97 {
		f, err := Parse(data[:n], 0)
		if err != nil {
			continue
		}
		f.Glyph('م')
		f.Advance(3)
		if l := f.GSUB(); l != nil {
			s, _ := l.SelectScript([]Tag{MakeTag("arab")})
			l.FeatureLookups(l.FindFeature(s.Index, -1, MakeTag("init")))
		}
	}
}
