//go:build race

package latency

// build names how the tests were built, for reports: the race detector
// slows everything down.
const build = "go test -race"
