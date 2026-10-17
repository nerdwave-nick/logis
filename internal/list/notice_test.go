package list

import (
	"strings"
	"testing"
	"time"
)

// A notice shows for as long as it is asked to, and at least as long as
// its words take to read: a second, and a second for every fifteen
// characters, ten seconds at most.
func TestANoticeShowsLongEnoughToRead(t *testing.T) {
	for _, tc := range []struct {
		text  string
		asked time.Duration
		want  time.Duration
	}{
		{"the rite is begun", 3 * time.Second, 3 * time.Second},
		{"the rite is begun", 8 * time.Second, 8 * time.Second},
		{strings.Repeat("x", 60), 3 * time.Second, 5 * time.Second},
		{strings.Repeat("é", 60), 3 * time.Second, 5 * time.Second}, // characters, not bytes
		{strings.Repeat("x", 150), 5 * time.Second, 10 * time.Second},
		{strings.Repeat("x", 600), 5 * time.Second, 10 * time.Second},
		{strings.Repeat("x", 600), 30 * time.Second, 30 * time.Second},
	} {
		if got := (Notice{Text: tc.text}).ReadFor(tc.asked); got != tc.want {
			t.Errorf("%d characters asked for %v: %v, want %v", len([]rune(tc.text)), tc.asked, got, tc.want)
		}
	}
}
