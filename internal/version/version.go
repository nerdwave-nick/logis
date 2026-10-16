// Package version holds the version logis reports.
//
// The single source of logis's version is the git tag vX.Y.Z of a release;
// no file in the tree names the number except the changelog's headings. A
// build learns its version one of two ways:
//
//   - stamped: go build -ldflags "-X github.com/nerdwave-nick/logis/internal/version.Version=V".
//     The PKGBUILD stamps the commit's `git describe --tags --match 'v[0-9]*'`
//     without its "v": 0.1.1 at the tag, 0.1.1-3-gabc1234 three commits after
//     it;
//   - recorded: otherwise the main module's version the go command wrote into
//     the binary's build info: the tag (v0.1.1) when built in a checkout at it
//     or by `go install …@v0.1.1`, Go's pseudo-version
//     (v0.1.2-0.<time>-<commit>) between tags, with "+dirty" when the
//     checkout has uncommitted or untracked files. Its "v" is dropped.
//
// A build with neither (go run, -buildvcs=false, a tree without git)
// reports Unknown.
package version

import (
	"runtime/debug"
	"strings"
)

// Unknown is the version of a build that neither -X nor its build info
// names.
const Unknown = "dev"

// Version is the version this build reports in `logis --version`: the one
// -X stamped, else the one its build info records (FromBuildInfo).
var Version string

func init() {
	if Version == "" {
		bi, _ := debug.ReadBuildInfo()
		Version = FromBuildInfo(bi)
	}
}

// FromBuildInfo is the version a build with build info bi reports when -X
// stamped none: its main module's version without the "v", or Unknown
// where none was recorded.
func FromBuildInfo(bi *debug.BuildInfo) string {
	if bi == nil {
		return Unknown
	}
	switch v := bi.Main.Version; v {
	case "", "(devel)":
		return Unknown
	default:
		return strings.TrimPrefix(v, "v")
	}
}
