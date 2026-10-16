package ot

// Scratch flags a shaping pass records on its buffer.
const (
	ScratchHasFractionSlash = 1 << iota
	ScratchHasDefaultIgnorables
	ScratchHasSpaceFallback
	ScratchHasGPOSAttachment
	ScratchHasCGJ
	ScratchHasBrokenSyllable
	ScratchHasVSFallback
	ScratchHasContinuations
)

// Buffer is the glyph string lookups work on, with HarfBuzz's in-place
// output: GSUB reads Info at idx and writes output glyphs behind it (or
// into a separate array once output outgrows input), Sync swaps them.
type Buffer struct {
	Info []Info
	Pos  []Pos
	// RTL is the buffer's direction; glyphs are in logical order until
	// positioning reverses them.
	RTL          bool
	ScratchFlags uint32

	out         []Info
	idx, outLen int
	haveOutput  bool
	separate    bool
	serial      uint8
	successful  bool
	maxOps      int
	maxLen      int
}

// NewBuffer holds infos (characters and their clusters, in logical order).
func NewBuffer(infos []Info, rtl bool) *Buffer {
	b := &Buffer{Info: infos, RTL: rtl}
	b.Reset()
	return b
}

// Reset prepares the buffer for a shaping pass over its current glyphs.
func (b *Buffer) Reset() {
	n := len(b.Info)
	b.successful = true
	b.maxOps = max(n*4096, 65536)
	b.maxLen = max(n*256, 65536)
	b.idx, b.outLen, b.haveOutput, b.separate = 0, 0, false, false
	b.serial = 0
}

// Len is the number of glyphs.
func (b *Buffer) Len() int { return len(b.Info) }

// Successful reports that no limit was exceeded.
func (b *Buffer) Successful() bool { return b.successful }

func (b *Buffer) cur() *Info { return &b.Info[b.idx] }

func (b *Buffer) outInfo() []Info {
	if b.separate {
		return b.out
	}
	return b.Info
}

// ClearOutput starts writing output glyphs.
func (b *Buffer) ClearOutput() {
	b.haveOutput = true
	b.idx, b.outLen = 0, 0
	b.separate = false
}

// ClearPositions starts positioning: every glyph at zero.
func (b *Buffer) ClearPositions() {
	b.haveOutput = false
	b.outLen = 0
	b.separate = false
	if cap(b.Pos) >= len(b.Info) {
		b.Pos = b.Pos[:len(b.Info)]
		clear(b.Pos)
	} else {
		b.Pos = make([]Pos, len(b.Info))
	}
}

// Sync makes the output the buffer's glyphs.
func (b *Buffer) Sync() {
	if !b.successful {
		b.haveOutput, b.outLen, b.idx = false, 0, 0
		return
	}
	b.nextGlyphs(len(b.Info) - b.idx)
	if b.separate {
		b.Info, b.out = b.out, b.Info
		b.separate = false
	}
	b.Info = b.Info[:b.outLen]
	b.haveOutput, b.outLen, b.idx = false, 0, 0
}

func (b *Buffer) backtrackLen() int {
	if b.haveOutput {
		return b.outLen
	}
	return b.idx
}

func (b *Buffer) lookaheadLen() int { return len(b.Info) - b.idx }

// makeRoomFor ensures num_out output slots for num_in consumed inputs,
// switching to a separate output array when output would overrun input.
func (b *Buffer) makeRoomFor(numIn, numOut int) bool {
	if b.outLen+numOut > b.maxLen {
		b.successful = false
		return false
	}
	if !b.separate && b.outLen+numOut > b.idx+numIn {
		b.separate = true
		b.out = append(b.out[:0], b.Info[:b.outLen]...)
	}
	return true
}

func (b *Buffer) setOut(i int, info Info) {
	if b.separate {
		for len(b.out) <= i {
			b.out = append(b.out, Info{})
		}
		b.out[i] = info
		return
	}
	b.Info[i] = info
}

// nextGlyph copies the current glyph to the output and moves on.
func (b *Buffer) nextGlyph() {
	if b.haveOutput {
		if b.separate || b.outLen != b.idx {
			b.setOut(b.outLen, b.Info[b.idx])
		}
		b.outLen++
	}
	b.idx++
}

func (b *Buffer) nextGlyphs(n int) {
	if b.haveOutput {
		if b.separate || b.outLen != b.idx {
			for i := range n {
				b.setOut(b.outLen+i, b.Info[b.idx+i])
			}
		}
		b.outLen += n
	}
	b.idx += n
}

func (b *Buffer) skipGlyph() { b.idx++ }

// replaceGlyph outputs the current glyph as g and moves on.
func (b *Buffer) replaceGlyph(g uint32) {
	if b.separate || b.outLen != b.idx {
		if !b.makeRoomFor(1, 1) {
			return
		}
		b.setOut(b.outLen, b.Info[b.idx])
	}
	b.outInfo()[b.outLen].Glyph = g
	b.idx++
	b.outLen++
}

// outputGlyph outputs a copy of the current glyph as g without moving on.
func (b *Buffer) outputGlyph(g uint32) {
	if !b.makeRoomFor(0, 1) {
		return
	}
	if b.idx == len(b.Info) && b.outLen == 0 {
		return
	}
	var info Info
	if b.idx < len(b.Info) {
		info = b.Info[b.idx]
	} else {
		info = b.outInfo()[b.outLen-1]
	}
	info.Glyph = g
	b.setOut(b.outLen, info)
	b.outLen++
}

// OutputInfo appends info to the output.
func (b *Buffer) OutputInfo(info Info) {
	if !b.makeRoomFor(0, 1) {
		return
	}
	b.setOut(b.outLen, info)
	b.outLen++
}

// moveTo moves the input position to output index i, copying glyphs
// forward or rewinding output back into the input.
func (b *Buffer) moveTo(i int) bool {
	if !b.haveOutput {
		b.idx = i
		return true
	}
	if !b.successful {
		return false
	}
	switch {
	case b.outLen < i:
		count := i - b.outLen
		if !b.makeRoomFor(count, count) {
			return false
		}
		for j := range count {
			b.setOut(b.outLen+j, b.Info[b.idx+j])
		}
		b.idx += count
		b.outLen += count
	case b.outLen > i:
		count := b.outLen - i
		if b.idx < count && !b.shiftForward(count-b.idx) {
			return false
		}
		b.idx -= count
		b.outLen -= count
		out := b.outInfo()
		for j := range count {
			b.Info[b.idx+j] = out[b.outLen+j]
		}
	}
	return true
}

func (b *Buffer) shiftForward(count int) bool {
	if len(b.Info)+count > b.maxLen {
		b.successful = false
		return false
	}
	b.maxOps -= len(b.Info) - b.idx
	if b.maxOps < 0 {
		b.successful = false
		return false
	}
	n := len(b.Info)
	b.Info = append(b.Info, make([]Info, count)...)
	copy(b.Info[b.idx+count:], b.Info[b.idx:n])
	if b.idx+count > n {
		clear(b.Info[n : b.idx+count])
	}
	b.idx += count
	return true
}

// allocateLigID is a fresh three-bit ligature id.
func (b *Buffer) allocateLigID() uint8 {
	for {
		b.serial++
		if b.serial == 0 {
			b.serial = 1
		}
		if id := b.serial & 7; id != 0 {
			return id
		}
	}
}

// Reverse reverses the glyphs (and positions, if any).
func (b *Buffer) Reverse() { b.ReverseRange(0, len(b.Info)) }

// ReverseRange reverses glyphs start..end.
func (b *Buffer) ReverseRange(start, end int) {
	for i, j := start, end-1; i < j; i, j = i+1, j-1 {
		b.Info[i], b.Info[j] = b.Info[j], b.Info[i]
		if len(b.Pos) == len(b.Info) {
			b.Pos[i], b.Pos[j] = b.Pos[j], b.Pos[i]
		}
	}
}

// The operations below let a shaper rewrite characters with output, as
// the normaliser does: read at Idx, then copy, replace, output or skip.

// Idx is the input position.
func (b *Buffer) Idx() int { return b.idx }

// Cur is the glyph i places after the input position.
func (b *Buffer) Cur(i int) *Info { return &b.Info[b.idx+i] }

// NextGlyph copies the current glyph to the output and moves on.
func (b *Buffer) NextGlyph() { b.nextGlyph() }

// NextGlyphs copies n glyphs.
func (b *Buffer) NextGlyphs(n int) { b.nextGlyphs(n) }

// SkipGlyph drops the current glyph.
func (b *Buffer) SkipGlyph() { b.skipGlyph() }

// OutputGlyph outputs a copy of the current glyph as g, staying put.
func (b *Buffer) OutputGlyph(g uint32) { b.outputGlyph(g) }

// ReplaceGlyphs replaces numIn glyphs by copies of the first as glyphs,
// merging their clusters.
func (b *Buffer) ReplaceGlyphs(numIn int, glyphs ...uint32) {
	if !b.makeRoomFor(numIn, len(glyphs)) {
		return
	}
	b.MergeClusters(b.idx, b.idx+numIn)
	orig := b.Info[b.idx]
	for i, g := range glyphs {
		info := orig
		info.Glyph = g
		b.setOut(b.outLen+i, info)
	}
	b.idx += numIn
	b.outLen += len(glyphs)
}

// OutLen is the number of output glyphs; SetOutLen drops output glyphs.
func (b *Buffer) OutLen() int { return b.outLen }

// SetOutLen: see OutLen.
func (b *Buffer) SetOutLen(n int) { b.outLen = n }

// Out is output glyph i.
func (b *Buffer) Out(i int) *Info { return &b.outInfo()[i] }

// MergeOutClusters merges the clusters of output glyphs start..end.
func (b *Buffer) MergeOutClusters(start, end int) { b.mergeOutClusters(start, end) }
