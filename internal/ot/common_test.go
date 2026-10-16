package ot

import (
	"encoding/binary"
	"testing"
)

// be builds big-endian table bytes from 16-bit words.
func be(words ...int) []byte {
	b := make([]byte, 2*len(words))
	for i, w := range words {
		binary.BigEndian.PutUint16(b[2*i:], uint16(w))
	}
	return b
}

func TestCoverageFindsGlyphsInBothFormats(t *testing.T) {
	list := table(be(1, 3, 5, 9, 40))
	ranges := table(be(2, 2, 10, 12, 0, 20, 21, 3))
	cases := []struct {
		name string
		t    table
		g    uint16
		want int
	}{
		{"list first", list, 5, 0},
		{"list last", list, 40, 2},
		{"list missing", list, 6, -1},
		{"range start", ranges, 10, 0},
		{"range inside", ranges, 12, 2},
		{"second range", ranges, 21, 4},
		{"between ranges", ranges, 15, -1},
		{"unknown format", table(be(3, 1, 5)), 5, -1},
		{"truncated list", table(be(1, 9, 5)), 40, -1},
		{"null table", nil, 5, -1},
	}
	for _, c := range cases {
		if got := c.t.coverage(c.g); got != c.want {
			t.Errorf("%s: coverage(%d) = %d, want %d", c.name, c.g, got, c.want)
		}
	}
}

func TestClassDefGivesClassesAndZeroOutside(t *testing.T) {
	array := table(be(1, 10, 3, 4, 0, 7))
	ranges := table(be(2, 2, 3, 5, 1, 8, 8, 2))
	cases := []struct {
		name string
		t    table
		g    uint16
		want int
	}{
		{"array", array, 10, 4},
		{"array zero", array, 11, 0},
		{"array end", array, 12, 7},
		{"array past", array, 13, 0},
		{"array before", array, 9, 0},
		{"range", ranges, 4, 1},
		{"single range", ranges, 8, 2},
		{"gap", ranges, 6, 0},
		{"null", nil, 4, 0},
	}
	for _, c := range cases {
		if got := c.t.classOf(c.g); got != c.want {
			t.Errorf("%s: classOf(%d) = %d, want %d", c.name, c.g, got, c.want)
		}
	}
}

func TestDeviceDeltasPerPpem(t *testing.T) {
	// Format 1 (2-bit): ppem 11..14 → +1, -1, 0, -2.
	two := table(be(11, 14, 1, 0b01_11_00_10<<8))
	// Format 2 (4-bit): ppem 12..13 → +7, -8.
	four := table(be(12, 13, 2, 0x78<<8))
	// Format 3 (8-bit): ppem 20..21 → -3, +100.
	eight := table(be(20, 21, 3, 0xfd64))
	variation := table(be(0, 5, 0x8000))
	cases := []struct {
		name string
		t    table
		ppem int
		want int
	}{
		{"2-bit first", two, 11, 1},
		{"2-bit negative", two, 12, -1},
		{"2-bit zero", two, 13, 0},
		{"2-bit last", two, 14, -2},
		{"below range", two, 10, 0},
		{"above range", two, 15, 0},
		{"no ppem", two, 0, 0},
		{"4-bit", four, 12, 7},
		{"4-bit negative", four, 13, -8},
		{"8-bit negative", eight, 20, -3},
		{"8-bit", eight, 21, 100},
		{"variation index", variation, 12, 0},
	}
	for _, c := range cases {
		if got := c.t.deviceDelta(c.ppem); got != c.want {
			t.Errorf("%s: deviceDelta(%d) = %d, want %d", c.name, c.ppem, got, c.want)
		}
	}
}

func TestReadsOutOfRangeAreZero(t *testing.T) {
	tb := table(be(0x1234))
	if tb.u16(0) != 0x1234 || tb.u16(1) != 0 || tb.u16(-2) != 0 || tb.u32(0) != 0 {
		t.Fatal("reads past the end must be zero")
	}
	if tb.sub(0) != nil || tb.sub(4) != nil {
		t.Fatal("a null or out-of-range offset is a nil table")
	}
	// The word at byte 2 is an offset to byte 4.
	if got := table(be(0, 4, 0x5678)).sub(2).u16(0); got != 0x5678 {
		t.Fatalf("sub(2).u16(0) = %#x", got)
	}
}
