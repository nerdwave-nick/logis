package version_test

import (
	"runtime/debug"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// A build that no -X stamped reports the main module's version that the go
// command recorded, without its "v": the tag in a checkout at it or from
// `go install …@vX.Y.Z`, Go's pseudo-version between tags, "+dirty" with
// uncommitted files. Where none was recorded it is version.Unknown.
func TestTheRecordedModuleVersionIsReportedWithoutItsV(t *testing.T) {
	for recorded, want := range map[string]string{
		"v0.1.1":                               "0.1.1",
		"v0.1.1+dirty":                         "0.1.1+dirty",
		"v0.1.2-0.20261016134858-426240b0fc9d": "0.1.2-0.20261016134858-426240b0fc9d",
		"v0.0.0-20261016134841-637cbc763e2b+dirty": "0.0.0-20261016134841-637cbc763e2b+dirty",
		"(devel)": version.Unknown, // go run, -buildvcs=false, no VCS
		"":        version.Unknown,
	} {
		bi := &debug.BuildInfo{Main: debug.Module{Path: "github.com/nerdwave-nick/logis", Version: recorded}}
		if got := version.FromBuildInfo(bi); got != want {
			t.Errorf("recorded %q: %q, want %q", recorded, got, want)
		}
	}
	if got := version.FromBuildInfo(nil); got != version.Unknown {
		t.Errorf("without build info: %q, want %q", got, version.Unknown)
	}
}

// The test binary was not stamped through -X, so it reports what its build
// info says (cmd/logis's version_test.go builds the real binary both ways).
func TestAnUnstampedBuildReportsItsBuildInfo(t *testing.T) {
	bi, _ := debug.ReadBuildInfo()
	if got, want := version.Version, version.FromBuildInfo(bi); got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
}
