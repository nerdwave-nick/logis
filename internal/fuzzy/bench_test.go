package fuzzy

import (
	"fmt"
	"testing"
)

// BenchmarkScore scores path-like lines (Rust's latency many_lines) with
// the queries of typing "rite": a single character, then the optimal
// alignment over the line.
func BenchmarkScore(b *testing.B) {
	words := []string{"rite", "aspect", "cogitator", "noosphere", "spirit", "litany", "forge"}
	var texts []Text
	for i := range 1000 {
		texts = append(texts, NewText(fmt.Sprintf("/%s/%s/%s-%d\t%s\t%d",
			words[i%7], words[(i/7)%7], words[(i/49)%7], i, words[(i/343)%7], i*7919%100_003)))
	}
	m := NewMatcher()
	for _, q := range []string{"r", "rit", "rite", "rite oil", "zzz"} {
		p := Parse(q)
		b.Run(q, func(b *testing.B) {
			for b.Loop() {
				for _, t := range texts {
					p.Score(m, t)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(texts)), "ns/text")
		})
	}
}
