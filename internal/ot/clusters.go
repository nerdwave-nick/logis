package ot

// Cluster bookkeeping at HarfBuzz's default level (monotone graphemes):
// glyphs that ligate, reorder or delete merge their clusters so that a
// cluster stays one contiguous, non-decreasing run.

func (b *Buffer) spendOps(n int) {
	b.maxOps -= n
	if b.maxOps < 0 {
		b.successful = false
	}
}

// MergeClusters gives glyphs start..end the smallest of their clusters,
// extended over neighbours sharing their edge clusters.
func (b *Buffer) MergeClusters(start, end int) {
	if end-start < 2 {
		return
	}
	b.spendOps(end - start)
	cluster := b.Info[start].Cluster
	for _, info := range b.Info[start+1 : end] {
		cluster = min(cluster, info.Cluster)
	}
	if cluster != b.Info[end-1].Cluster {
		for end < len(b.Info) && b.Info[end-1].Cluster == b.Info[end].Cluster {
			end++
		}
	}
	if cluster != b.Info[start].Cluster {
		for b.idx < start && b.Info[start-1].Cluster == b.Info[start].Cluster {
			start--
		}
	}
	// At the start of the input, continue into the output.
	if b.idx == start && b.Info[start].Cluster != cluster {
		out := b.outInfo()
		for i := b.outLen; i > 0 && out[i-1].Cluster == b.Info[start].Cluster; i-- {
			out[i-1].Cluster = cluster
		}
	}
	for i := start; i < end; i++ {
		b.Info[i].Cluster = cluster
	}
}

// mergeOutClusters is MergeClusters on output glyphs start..end.
func (b *Buffer) mergeOutClusters(start, end int) {
	if end-start < 2 {
		return
	}
	b.spendOps(end - start)
	out := b.outInfo()
	cluster := out[start].Cluster
	for _, info := range out[start+1 : end] {
		cluster = min(cluster, info.Cluster)
	}
	for start > 0 && out[start-1].Cluster == out[start].Cluster {
		start--
	}
	for end < b.outLen && out[end-1].Cluster == out[end].Cluster {
		end++
	}
	// At the end of the output, continue into the input.
	if end == b.outLen {
		for i := b.idx; i < len(b.Info) && b.Info[i].Cluster == out[end-1].Cluster; i++ {
			b.Info[i].Cluster = cluster
		}
	}
	for i := start; i < end; i++ {
		out[i].Cluster = cluster
	}
}

// deleteGlyph drops the current glyph, merging its cluster into a
// neighbour's unless another glyph keeps it.
func (b *Buffer) deleteGlyph() {
	cluster := b.Info[b.idx].Cluster
	out := b.outInfo()
	if b.idx+1 < len(b.Info) && cluster == b.Info[b.idx+1].Cluster ||
		b.outLen != 0 && cluster == out[b.outLen-1].Cluster {
		b.skipGlyph()
		return
	}
	if b.outLen != 0 {
		if old := out[b.outLen-1].Cluster; cluster < old {
			for i := b.outLen; i > 0 && out[i-1].Cluster == old; i-- {
				out[i-1].Cluster = cluster
			}
		}
		b.skipGlyph()
		return
	}
	if b.idx+1 < len(b.Info) {
		b.MergeClusters(b.idx, b.idx+2)
	}
	b.skipGlyph()
}

// DeleteGlyphsInPlace drops glyphs (with their positions) that drop
// reports, merging clusters as deleteGlyph does.
func (b *Buffer) DeleteGlyphsInPlace(drop func(*Info) bool) {
	j := 0
	n := len(b.Info)
	for i := range n {
		if drop(&b.Info[i]) {
			cluster := b.Info[i].Cluster
			if i+1 < n && cluster == b.Info[i+1].Cluster {
				continue
			}
			if j != 0 {
				if old := b.Info[j-1].Cluster; cluster < old {
					for k := j; k > 0 && b.Info[k-1].Cluster == old; k-- {
						b.Info[k-1].Cluster = cluster
					}
				}
				continue
			}
			if i+1 < n {
				b.MergeClusters(i, i+2)
			}
			continue
		}
		if j != i {
			b.Info[j] = b.Info[i]
			if len(b.Pos) == n {
				b.Pos[j] = b.Pos[i]
			}
		}
		j++
	}
	b.Info = b.Info[:j]
	if len(b.Pos) == n {
		b.Pos = b.Pos[:j]
	}
}

// Sort is a stable insertion sort of glyphs start..end by after (a
// "greater than" test), merging the clusters of moved glyphs.
func (b *Buffer) Sort(start, end int, after func(a, b *Info) bool) {
	for i := start + 1; i < end; i++ {
		j := i
		for j > start && after(&b.Info[j-1], &b.Info[i]) {
			j--
		}
		if i == j {
			continue
		}
		b.MergeClusters(j, i+1)
		t := b.Info[i]
		copy(b.Info[j+1:i+1], b.Info[j:i])
		b.Info[j] = t
	}
}

// SetMasks sets value in mask for every glyph.
func (b *Buffer) SetMasks(value, mask uint32) {
	if mask == 0 {
		return
	}
	value &= mask
	for i := range b.Info {
		b.Info[i].Mask = b.Info[i].Mask&^mask | value
	}
}

// GroupEnd is the end of the group starting at start: glyphs for which
// same(previous, glyph) holds.
func (b *Buffer) GroupEnd(start int, same func(a, b *Info) bool) int {
	start++
	for start < len(b.Info) && same(&b.Info[start-1], &b.Info[start]) {
		start++
	}
	return start
}

// ReverseGroups reverses the order of groups but keeps each group's
// glyphs in order, merging each group's clusters if asked.
func (b *Buffer) ReverseGroups(same func(a, b *Info) bool, merge bool) {
	if len(b.Info) == 0 {
		return
	}
	start := 0
	for i := 1; i <= len(b.Info); i++ {
		if i == len(b.Info) || !same(&b.Info[i-1], &b.Info[i]) {
			if merge {
				b.MergeClusters(start, i)
			}
			b.ReverseRange(start, i)
			start = i
		}
	}
	b.Reverse()
}

// SameCluster groups glyphs of one cluster.
func SameCluster(a, b *Info) bool { return a.Cluster == b.Cluster }

// SameGrapheme groups a grapheme's continuations with it.
func SameGrapheme(_, b *Info) bool { return b.IsContinuation() }
