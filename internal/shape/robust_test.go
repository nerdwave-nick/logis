package shape

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/nerdwave-nick/logis/internal/ot"
)

// layoutTables are the byte ranges of a font's GDEF, GSUB, GPOS and cmap.
func layoutTables(data []byte) [][2]int {
	var out [][2]int
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := range n {
		rec := data[12+16*i:]
		switch string(rec[:4]) {
		case "GDEF", "GSUB", "GPOS", "cmap":
			off, length := int(binary.BigEndian.Uint32(rec[8:])), int(binary.BigEndian.Uint32(rec[12:]))
			out = append(out, [2]int{off, off + length})
		}
	}
	return out
}

// Fonts are external data: shaping with corrupted layout tables gives
// some glyphs, never a panic or a hang.
func TestCorruptLayoutTablesNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 13))
	texts := []string{"office fit AVATAR é", "مرحبا بالعالم لا", "שָׁלוֹם", "👍🏽🧑‍💻", "ab cd xef g ihj pq mn tu klz f́ị"}
	for _, name := range []string{"NotoSans-Full.ttf", "NotoSansArabic-Full.ttf", "NotoNastaliqUrdu-Subset.ttf", "Synthetic-Layout.ttf", "NotoColorEmoji-Subset.ttf"} {
		orig := fontData(t, name)
		ranges := layoutTables(orig)
		for round := range 150 {
			data := append([]byte(nil), orig...)
			for range 1 + rng.IntN(40) {
				r := ranges[rng.IntN(len(ranges))]
				i := r[0] + rng.IntN(r[1]-r[0])
				data[i] = byte(rng.IntN(256))
			}
			face, err := ot.Parse(data, 0)
			if err != nil {
				continue
			}
			f := NewFont(face)
			for range 2 {
				s, rtl := texts[rng.IntN(len(texts))], rng.IntN(2) == 1
				func() {
					defer func() {
						if p := recover(); p != nil {
							t.Fatalf("%s round %d %q rtl=%v: panic %v", name, round, s, rtl, p)
						}
					}()
					f.Shape(s, Options{RTL: rtl, PPEM: 12})
				}()
			}
		}
	}
}
