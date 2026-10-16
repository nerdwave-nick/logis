// Package themes holds the bundled themes' files, compiled into logis. It
// lives beside the files because go:embed reaches no parent directory; the
// theme engine is internal/theme.
package themes

import _ "embed" // the files below

//go:embed forge-crimson-brass.jsonc
var forgeCrimsonBrass string

//go:embed cogitator-phosphor.jsonc
var cogitatorPhosphor string

// Bundled are the bundled themes, name and text, the default first.
var Bundled = [...]struct{ Name, Text string }{
	{"forge-crimson-brass", forgeCrimsonBrass},
	{"cogitator-phosphor", cogitatorPhosphor},
}
