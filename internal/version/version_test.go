package version_test

import (
	"regexp"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// The version is a Semantic Versioning 2.0.0 release, MAJOR.MINOR.PATCH
// without leading zeros (the CHANGELOG keeps to SemVer); the PKGBUILD's
// prepare() greps version.go for exactly `const Version = "<pkgver>"`.
func TestVersionIsMajorMinorPatch(t *testing.T) {
	semver := regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	if !semver.MatchString(version.Version) {
		t.Errorf("version.Version = %q, not MAJOR.MINOR.PATCH", version.Version)
	}
}
