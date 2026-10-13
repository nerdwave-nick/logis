package theme

import "testing"

// The daemon reads its theme at start; the window wears it on every open.
func BenchmarkLoadTheDefault(b *testing.B) {
	for b.Loop() {
		if _, err := (Themes{}).Load(DefaultName); err != nil {
			b.Fatal(err)
		}
	}
}
