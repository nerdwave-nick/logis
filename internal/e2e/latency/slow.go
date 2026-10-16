package latency

import (
	"fmt"
	"regexp"
	"strings"
)

// SlowOpen is what to say of a warm open of ms that was over SlowOpenMS,
// given the longest time the test compositor itself did not answer
// meanwhile (stalled: whether it stalled at all). The compositor is named
// only if its stall alone made the open slow.
func SlowOpen(ms, stallMS float64, stalled bool) string {
	if stalled && stallMS >= ms-SlowOpenMS {
		return fmt.Sprintf("a warm open took %.1f ms, and the test compositor itself answered nobody "+
			"for %.1f ms of it. A nested niri does that when the session does not draw its window "+
			"(another workspace, out of view, screen off): it gets frame callbacks about once a "+
			"second and blocks. Keep it in view while measuring.", ms, stallMS)
	}
	compositor := "answered throughout"
	if stalled {
		compositor = fmt.Sprintf("stalled only %.1f ms", stallMS)
	}
	return fmt.Sprintf("a warm open took %.1f ms (over %v ms) while the test compositor %s: "+
		"logis, or the machine under load, was slow", ms, SlowOpenMS, compositor)
}

// colour is an ANSI colour sequence (ESC [ … m), as libwayland may print.
var colour = regexp.MustCompile("\x1b\\[[0-9;]*m")

// RoundTrips is how many round trips (wl_display.sync requests) a
// WAYLAND_DEBUG log holds; its colours are skipped.
func RoundTrips(lines []string) int {
	n := 0
	for _, l := range lines {
		l = colour.ReplaceAllString(l, "")
		if strings.Contains(l, "-> wl_display#") && strings.Contains(l, ".sync(") {
			n++
		}
	}
	return n
}
