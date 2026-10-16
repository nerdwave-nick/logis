package render

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// A frame's new glyphs reach the atlas texture in one upload per shelf they
// fill, not one per glyph: each upload is a driver call that costs tens of
// microseconds, and a first frame of a panel full of text has a hundred
// glyphs or more.
func TestAFramesNewGlyphsAreUploadedOncePerShelf(t *testing.T) {
	h := headless(t)
	f, err := text.ResolveSystem().Wait()
	if err != nil {
		t.Skipf("no system fonts: %v", err)
	}
	h.SetFonts(f)
	line := func(s string, y float32, size float32) Text {
		return NewText(s, [2]float32{4, y}, size, Hex(0xffffff))
	}
	first := &Scene{Texts: []Text{
		line("The machine spirit wakes: 0123456789 abcdefghijklmnop", 4, 15),
		line("QRSTUVWXYZ qrstuvwxyz !?#%&*+-=<>[]{}", 30, 15),
		line("INVOKE the rite", 56, 13),
		line("rite of oil", 80, 19),
	}}
	if _, err := h.Offscreen(first, scale.Size{W: 640, H: 120}, scale.From120ths(120)); err != nil {
		t.Fatal(err)
	}
	p := h.glyphs
	shelves := len(p.packer.shelves)
	if p.rasterised < 60 {
		t.Fatalf("only %d glyphs rasterised", p.rasterised)
	}
	if p.uploads > shelves {
		t.Fatalf("%d uploads for %d glyphs on %d shelves", p.uploads, p.rasterised, shelves)
	}
	// A later frame uploads only its new glyphs.
	p.uploads = 0
	second := &Scene{Texts: append(first.Texts, line("ÆØÅ æøå ÄÖÜ", 100, 15))}
	if _, err := h.Offscreen(second, scale.Size{W: 640, H: 120}, scale.From120ths(120)); err != nil {
		t.Fatal(err)
	}
	if p.uploads == 0 || p.uploads > len(p.packer.shelves) {
		t.Fatalf("%d uploads for the new glyphs on %d shelves", p.uploads, len(p.packer.shelves))
	}
}

// Glyphs placed before the atlas grows within one frame survive the
// growth: they are uploaded before the GPU copies the atlas.
func TestGlyphsPlacedBeforeTheAtlasGrowsInAFrameSurviveIt(t *testing.T) {
	h := headless(t)
	f, err := text.ResolveSystem().Wait()
	if err != nil {
		t.Skipf("no system fonts: %v", err)
	}
	h.SetFonts(f)
	sc := &Scene{Clear: Hex(0)}
	for i, s := range []string{"The machine spirit wakes: 0123456789", "ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz", "ÆØÅ æøå ÄÖÜ ß !?#%&*+-=<>[]{}"} {
		sc.Texts = append(sc.Texts, NewText(s, [2]float32{4, 4 + 30*float32(i)}, 22, Hex(0xffffff)))
	}
	size, s := scale.Size{W: 640, H: 100}, scale.From120ths(120)
	want, err := h.Offscreen(sc, size, s)
	if err != nil {
		t.Fatal(err)
	}
	h.glyphs.reset(32) // a tiny atlas: it grows several times in the next frame
	got, err := h.Offscreen(sc, size, s)
	if err != nil {
		t.Fatal(err)
	}
	if side, _ := h.glyphs.packer.size(); side <= 32 {
		t.Fatalf("the atlas did not grow (side %d)", side)
	}
	if string(got.Pixels) != string(want.Pixels) {
		t.Fatal("glyphs placed before the growth were lost")
	}
}
