// Package version holds the version logis reports.
package version

// Version is the release this build reports in `logis --version`, and the
// single source of logis's version: the PKGBUILD, its .SRCINFO, the man
// page and the changelog name it too, and cmd/logis/packaging_test.go
// holds them to it.
const Version = "1.1.1"
