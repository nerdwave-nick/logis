package text_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/fc"
	"github.com/nerdwave-nick/logis/internal/text"
)

func font(name string) string {
	p, err := filepath.Abs(filepath.Join("..", "..", "tests", "fonts", name))
	if err != nil {
		panic(err)
	}
	return p
}

// The pinned fonts of tests/text.rs and tests/faces.rs together, with
// their aliases.
var files = []string{
	"NotoSans-Regular.ttf",
	"NotoSansHebrew-Regular.ttf",
	"NotoSansArabic-Regular.ttf",
	"NotoSansCJKjp-Regular.otf",
	"NotoColorEmoji.ttf",
	"NotoSansMono-Regular.ttf",
	"NotoSansMono-SemiBold.ttf",
	"NotoSerif-SemiBold.ttf",
}

const rules = `<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <alias binding="same"><family>sans-serif</family><prefer><family>Noto Sans</family></prefer></alias>
  <alias binding="same"><family>logis-test-alias</family><prefer><family>Noto Sans Hebrew</family></prefer></alias>
  <alias binding="same"><family>monospace</family><prefer><family>Noto Sans Mono</family></prefer></alias>
  <alias binding="same"><family>serif</family><prefer><family>Noto Serif</family></prefer></alias>
</fontconfig>
`

func pinnedConfig() (*fc.Config, error) {
	var paths []string
	for _, f := range files {
		paths = append(paths, font(f))
	}
	return text.Isolated(rules, paths)
}

func pinned(t *testing.T, family string) *text.Fonts {
	t.Helper()
	c, err := pinnedConfig()
	if err != nil {
		t.Fatal(err)
	}
	f, err := text.New(c, family)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func loaded(f *text.Fonts) []string {
	var out []string
	for _, file := range f.Loaded() {
		out = append(out, filepath.Base(file.Path))
	}
	return out
}

func TestOnlyTheDefaultFaceIsLoadedAtFirst(t *testing.T) {
	f := pinned(t, "sans-serif")
	if f.Family() != "Noto Sans" {
		t.Errorf("family %q", f.Family())
	}
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf"}) {
		t.Errorf("loaded %v", got)
	}
}

func TestAnAliasSelectsTheDefaultFamily(t *testing.T) {
	if got := pinned(t, "logis-test-alias").Family(); got != "Noto Sans Hebrew" {
		t.Errorf("family %q", got)
	}
}

func TestFallbackFacesLoadPerCharacterOnlyWhenNeeded(t *testing.T) {
	f := pinned(t, "sans-serif")
	f.Ensure("The machine spirit wakes: Ça déjà vu")
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf"}) {
		t.Fatalf("after Latin: %v", got)
	}
	f.Ensure("機械の精霊")
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf"}) {
		t.Fatalf("after CJK: %v", got)
	}
	f.Ensure("🔥 שָׁלוֹם 機械")
	want := []string{"NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf", "NotoColorEmoji.ttf", "NotoSansHebrew-Regular.ttf"}
	if got := loaded(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("after emoji and Hebrew: %v", got)
	}
}

func TestJoinersAndSelectorsLoadNoFace(t *testing.T) {
	// The Hebrew and Arabic subsets both list U+200D; it must not pull either in.
	f := pinned(t, "sans-serif")
	f.Ensure("a\u200Db\uFE0F\n")
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf"}) {
		t.Errorf("loaded %v", got)
	}
}

func TestJoinersSelectorsAndControlsAreIgnorable(t *testing.T) {
	for _, r := range []rune{'\u200D', '\uFE0F', '\n', '\t', '\U000E0067', '\u202E'} {
		if !text.Ignorable(r) {
			t.Errorf("%U is not ignorable", r)
		}
	}
	for _, r := range []rune{'a', ' ', '機', '🔥', 'ש', '\U0001F3FD'} {
		if text.Ignorable(r) {
			t.Errorf("%U is ignorable", r)
		}
	}
}

func TestAMissingFontFileIsReported(t *testing.T) {
	_, err := text.Isolated(rules, []string{font("no-such-font.ttf")})
	var config *text.ConfigError
	if !errors.As(err, &config) || !strings.Contains(err.Error(), "no-such-font.ttf") ||
		!strings.HasPrefix(err.Error(), "the font litanies are corrupt: ") {
		t.Fatalf("error %v", err)
	}
}

func TestAFamilyNothingAnswersIsNoFace(t *testing.T) {
	c, err := pinnedConfig()
	if err != nil {
		t.Fatal(err)
	}
	_, err = text.New(c, "\x00")
	want := "no font answers the rite: fontconfig found no face for '\x00'"
	if err == nil || err.Error() != want {
		t.Fatalf("error %v", err)
	}
}

func TestAFileThatIsNoFontIsUnreadable(t *testing.T) {
	// fontconfig takes a font that is later replaced on disk by junk.
	dir := t.TempDir()
	path := filepath.Join(dir, "scroll.ttf")
	data, err := os.ReadFile(font("NotoSans-Regular.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := text.Isolated(rules, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a font at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = text.New(c, "sans-serif")
	want := "the font scroll " + path + " (face 0) is unreadable"
	if err == nil || err.Error() != want {
		t.Fatalf("error %v, want %q", err, want)
	}
}

func TestTheSystemConfigurationResolvesTheDefaultFamily(t *testing.T) {
	f, err := text.Resolve(text.System, text.DefaultFamily).Wait()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if len(f.Loaded()) != 1 || f.Family() == "" {
		t.Errorf("loaded %v, family %q", f.Loaded(), f.Family())
	}
}

func TestFontsResolvedOnTheirOwnGoroutineBehaveLikeInlineOnes(t *testing.T) {
	f, err := text.Resolve(pinnedConfig, "sans-serif").Wait()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Family() != "Noto Sans" || !reflect.DeepEqual(loaded(f), []string{"NotoSans-Regular.ttf"}) {
		t.Fatalf("family %q, loaded %v", f.Family(), loaded(f))
	}
	f.Ensure("機械の精霊")
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf"}) {
		t.Errorf("loaded %v", got)
	}
}

func TestAFontGoroutineReportsWhyItFailed(t *testing.T) {
	_, err := text.Resolve(func() (*fc.Config, error) { return nil, &text.ConfigError{Why: "no rules"} }, "sans-serif").Wait()
	if err == nil || err.Error() != "the font litanies are corrupt: no rules" {
		t.Errorf("error %v", err)
	}
	_, err = text.Resolve(pinnedConfig, "\x00").Wait()
	var none *text.NoFaceError
	if !errors.As(err, &none) || none.Family != "\x00" {
		t.Errorf("error %v", err)
	}
}

func TestAFontGoroutineThatDiesIsAnErrorNotAPanic(t *testing.T) {
	_, err := text.Resolve(func() (*fc.Config, error) { panic("the font scrolls burn") }, "sans-serif").Wait()
	if err == nil || !strings.Contains(err.Error(), "font") {
		t.Fatalf("error %v", err)
	}
}

// tests/faces.rs: the faces a theme asks for.
func forgeFaces() []text.Face {
	return []text.Face{
		{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 400},
		{Families: []string{"Cinzel", "serif"}, Weight: 600},
		{Families: []string{"IBM Plex Mono", "monospace"}, Weight: 600},
	}
}

func identity(f *text.Fonts, i int) string {
	family, weight := f.Face(i)
	return family + "@" + strconv.Itoa(weight)
}

func TestMissingFamiliesFallThroughToTheGenericOne(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces(forgeFaces()); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"Noto Sans Mono@400", "Noto Serif@600", "Noto Sans Mono@600"} {
		if got := identity(f, i); got != want {
			t.Errorf("face %d: %s, want %s", i, got, want)
		}
	}
	got := strings.Join(loaded(f), " ")
	for _, file := range []string{"NotoSansMono-Regular.ttf", "NotoSerif-SemiBold.ttf", "NotoSansMono-SemiBold.ttf"} {
		if !strings.Contains(got, file) {
			t.Errorf("%s not loaded: %s", file, got)
		}
	}
}

func TestAnInstalledFirstChoiceWinsAndUnknownFacesUseTheFirst(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces([]text.Face{{Families: []string{"Noto Serif", "monospace"}, Weight: 600}}); err != nil {
		t.Fatal(err)
	}
	if got := identity(f, 0); got != "Noto Serif@600" {
		t.Errorf("face 0: %s", got)
	}
	if got := identity(f, 7); got != "Noto Serif@600" {
		t.Errorf("out of range: %s, want the first", got)
	}
}

func TestWithoutFacesEveryIndexIsTheDefaultFamily(t *testing.T) {
	f := pinned(t, "sans-serif")
	for _, i := range []int{0, 3} {
		if got := identity(f, i); got != "Noto Sans@400" {
			t.Errorf("face %d: %s", i, got)
		}
	}
}

func TestAFaceThatResolvesToNothingIsTheDefaultFamilyAndTheFirstError(t *testing.T) {
	f := pinned(t, "sans-serif")
	err := f.SetFaces([]text.Face{{Families: []string{"\x00"}, Weight: 400}, {Families: []string{"serif"}, Weight: 600}})
	var none *text.NoFaceError
	if !errors.As(err, &none) {
		t.Fatalf("error %v", err)
	}
	if got := identity(f, 0); got != "Noto Sans@400" {
		t.Errorf("face 0: %s", got)
	}
	if got := identity(f, 1); got != "Noto Serif@600" {
		t.Errorf("face 1: %s", got)
	}
}

func TestAFaceLoadedTwiceIsLoadedOnce(t *testing.T) {
	f := pinned(t, "sans-serif")
	if err := f.SetFaces([]text.Face{{Families: []string{"sans-serif"}, Weight: 400}, {Families: []string{"Noto Sans"}, Weight: 400}}); err != nil {
		t.Fatal(err)
	}
	if got := loaded(f); !reflect.DeepEqual(got, []string{"NotoSans-Regular.ttf"}) {
		t.Errorf("loaded %v", got)
	}
}
