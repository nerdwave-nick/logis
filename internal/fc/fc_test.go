package fc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/fc"
)

// font is a pinned test font (tests/fonts, made by subset.sh).
func font(name string) string {
	p, err := filepath.Abs(filepath.Join("..", "..", "tests", "fonts", name))
	if err != nil {
		panic(err)
	}
	return p
}

// rules declares aliases as a user's fonts.conf would (tests/text.rs RULES).
const rules = `<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <alias binding="same">
    <family>sans-serif</family>
    <prefer><family>Noto Sans</family></prefer>
  </alias>
  <alias binding="same">
    <family>logis-test-alias</family>
    <prefer><family>Noto Sans Hebrew</family></prefer>
  </alias>
  <alias binding="same"><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias binding="same"><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>
`

var pinnedFiles = []string{
	"NotoSans-Regular.ttf",
	"NotoSansHebrew-Regular.ttf",
	"NotoSansArabic-Regular.ttf",
	"NotoSansCJKjp-Regular.otf",
	"NotoColorEmoji.ttf",
	"NotoSansMono-Regular.ttf",
	"NotoSansMono-SemiBold.ttf",
	"NotoSerif-SemiBold.ttf",
}

func pinned(t *testing.T) *fc.Config {
	t.Helper()
	var files []string
	for _, f := range pinnedFiles {
		files = append(files, font(f))
	}
	c, err := fc.Isolated(rules, files)
	if err != nil {
		t.Fatalf("the pinned fonts load: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestAliasesResolveToTheFilesTheyPrefer(t *testing.T) {
	c := pinned(t)
	for family, want := range map[string]string{
		"logis-test-alias": "NotoSansHebrew-Regular.ttf",
		"sans-serif":       "NotoSans-Regular.ttf",
		"monospace":        "NotoSansMono-Regular.ttf",
	} {
		got, _, ok := c.Best(family)
		if !ok || got.Path != font(want) || got.Index != 0 {
			t.Errorf("%s: %+v (found %v), want %s face 0", family, got, ok, want)
		}
	}
}

func TestAFamilyListResolvesItsFirstInstalledFamilyAtAWeight(t *testing.T) {
	c := pinned(t)
	for _, tc := range []struct {
		families []string
		weight   int
		file     string
		family   string
	}{
		{[]string{"IBM Plex Mono", "monospace"}, 400, "NotoSansMono-Regular.ttf", "Noto Sans Mono"},
		{[]string{"IBM Plex Mono", "monospace"}, 600, "NotoSansMono-SemiBold.ttf", "Noto Sans Mono"},
		{[]string{"Cinzel", "serif"}, 600, "NotoSerif-SemiBold.ttf", "Noto Serif"},
		{[]string{"Noto Serif", "monospace"}, 600, "NotoSerif-SemiBold.ttf", "Noto Serif"},
	} {
		got, family, ok := c.BestOf(tc.families, tc.weight)
		if !ok || got.Path != font(tc.file) || family != tc.family {
			t.Errorf("%v@%d: %s %q (found %v), want %s %q", tc.families, tc.weight, got.Path, family, ok, tc.file, tc.family)
		}
	}
}

func TestANULByteResolvesNothing(t *testing.T) {
	if got, _, ok := pinned(t).Best("\x00"); ok {
		t.Errorf("a NUL family resolved to %+v", got)
	}
}

func TestTheFallbackOrderGivesTheFirstFaceCoveringACharacter(t *testing.T) {
	set := pinned(t).Fallbacks("sans-serif")
	if set == nil {
		t.Fatal("no fallback order")
	}
	defer set.Close()
	for r, want := range map[rune]string{
		'a': "NotoSans-Regular.ttf",
		'機': "NotoSansCJKjp-Regular.otf",
		'ש': "NotoSansHebrew-Regular.ttf",
		'م': "NotoSansArabic-Regular.ttf",
		'🔥': "NotoColorEmoji.ttf",
		'▒': "NotoSansMono-Regular.ttf",
	} {
		got, ok := set.FirstCovering(r)
		if !ok || got.Path != font(want) {
			t.Errorf("%q: %s (found %v), want %s", r, got.Path, ok, want)
		}
	}
	if got, ok := set.FirstCovering('\U0010FFFD'); ok {
		t.Errorf("a private-use character is covered by %s", got.Path)
	}
}

func TestAMissingFontFileIsReported(t *testing.T) {
	_, err := fc.Isolated(rules, []string{font("no-such-font.ttf")})
	if err == nil || !strings.Contains(err.Error(), "no-such-font.ttf") {
		t.Fatalf("error %v, want one naming no-such-font.ttf", err)
	}
}

func TestRulesFontconfigRefusesAreReported(t *testing.T) {
	_, err := fc.Isolated("<fontconfig><alias>", nil)
	if err == nil || err.Error() != "fontconfig rejected the rules" {
		t.Fatalf("error %v", err)
	}
}

func TestTheSystemConfigurationResolvesTheDefaultFamily(t *testing.T) {
	c, err := fc.System()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, _, ok := c.Best("sans-serif")
	if !ok {
		t.Fatal("the system has no font for sans-serif")
	}
	if st, err := os.Stat(got.Path); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("%s is not a file: %v", got.Path, err)
	}
}

// fontconfig's own fc-match is the oracle (spec §4, test-only): the system
// configuration resolves every family as fc-match does.
func TestTheSystemConfigurationAgreesWithFcMatch(t *testing.T) {
	if _, err := exec.LookPath("fc-match"); err != nil {
		t.Skip("fc-match is not installed")
	}
	c, err := fc.System()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, pattern := range []string{"sans-serif", "serif", "monospace", "emoji", "Noto Sans", "IBM Plex Mono", "no such family", "monospace:weight=180"} {
		out, err := exec.Command("fc-match", "-f", "%{file}\n%{index}\n%{family[0]}", pattern).Output()
		if err != nil {
			t.Fatalf("fc-match %s: %v", pattern, err)
		}
		lines := strings.SplitN(string(out), "\n", 3)
		index, _ := strconv.Atoi(lines[1])
		family, weight := pattern, 0
		if f, w, ok := strings.Cut(pattern, ":weight="); ok {
			n, _ := strconv.Atoi(w)
			family, weight = f, fcToOpenType(n)
		}
		got, name, ok := c.BestOf([]string{family}, weight)
		if !ok || got.Path != lines[0] || got.Index != index&0xffff || name != lines[2] {
			t.Errorf("%s: %s %d %q (found %v), fc-match %s %s %q", pattern, got.Path, got.Index, name, ok, lines[0], lines[1], lines[2])
		}
	}
}

// fcToOpenType inverts FcWeightFromOpenType for the weight fc-match was given.
func fcToOpenType(fc int) int {
	if fc == 180 {
		return 600
	}
	return 400
}
