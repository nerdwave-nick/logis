package rendertest

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/text"
)

// pinned are the test fonts in tests/fonts (OFL subsets).
var pinned = []string{
	"NotoSans-Regular.ttf",
	"NotoSansMono-Regular.ttf",
	"NotoSansMono-SemiBold.ttf",
	"NotoSerif-SemiBold.ttf",
	"NotoSansHebrew-Regular.ttf",
	"NotoSansArabic-Regular.ttf",
	"NotoSansCJKjp-Regular.otf",
	"NotoColorEmoji.ttf",
}

const pinnedRules = `<fontconfig>
  <alias binding="same"><family>sans-serif</family><prefer><family>Noto Sans</family></prefer></alias>
  <alias binding="same"><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias binding="same"><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>`

// PinnedFonts are the test fonts through an isolated fontconfig that knows
// only them (Rust tests/list.rs pinned_fonts): the themes' own families
// (IBM Plex Mono, Cinzel, VT323) fall back to the pinned monospace and
// serif. The caller owns them (a renderer's SetFonts takes them over).
func PinnedFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "tests", "fonts")
	paths := make([]string, len(pinned))
	for i, f := range pinned {
		paths[i] = filepath.Join(dir, f)
	}
	c, err := text.Isolated(pinnedRules, paths)
	if err != nil {
		t.Fatal(err)
	}
	f, err := text.New(c, "sans-serif")
	if err != nil {
		t.Fatal(err)
	}
	return f
}
