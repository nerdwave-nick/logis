package icons

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// The icon lookup over tests/fixtures/icons.

// fixtures is the repository's tests/fixtures/icons (go test runs in the
// package directory).
func fixtures(t testing.TB) string {
	t.Helper()
	dir, err := filepath.Abs("../../tests/fixtures/icons")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fixturePaths are a home and a share base dir, and pixmaps.
func fixturePaths(t testing.TB) SearchPaths {
	root := fixtures(t)
	return SearchPaths{
		IconDirs:   []string{filepath.Join(root, "home-icons"), filepath.Join(root, "share/icons")},
		PixmapDirs: []string{filepath.Join(root, "pixmaps")},
	}
}

func forge(t testing.TB) *Themes { return LoadThemes(fixturePaths(t), "logis-forge") }

// find is the found file relative to the fixture root, or "" for none.
func find(t *testing.T, themes *Themes, name string, size, n120 uint32) string {
	t.Helper()
	path, ok := themes.Find(name, size, scale.From120ths(n120))
	if !ok {
		return ""
	}
	rel, err := filepath.Rel(fixtures(t), path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

const forgeDir = "share/icons/logis-forge"

type lookupCase struct {
	name        string
	size, scale uint32
	want        string
}

func checkFinds(t *testing.T, themes *Themes, cases []lookupCase) {
	t.Helper()
	for _, c := range cases {
		if got := find(t, themes, c.name, c.size, c.scale); got != c.want {
			t.Errorf("find(%q, %d, %d/120) = %q, want %q", c.name, c.size, c.scale, got, c.want)
		}
	}
}

func TestLoadFollowsInheritsOnceThenHicolor(t *testing.T) {
	if got, want := forge(t).Names(), []string{"logis-forge", "logis-cogitator", "hicolor"}; !slices.Equal(got, want) {
		t.Errorf("chain %q, want %q", got, want)
	}
}

func TestFindPrefersTheExactlyMatchingSize(t *testing.T) {
	checkFinds(t, forge(t), []lookupCase{
		{"forge-cog", 16, 120, forgeDir + "/16x16/apps/forge-cog.png"},
		{"forge-cog", 32, 120, forgeDir + "/32x32/apps/forge-cog.png"},
		// Only the scalable directory spans 24.
		{"forge-cog", 24, 120, forgeDir + "/scalable/apps/forge-cog.svg"},
	})
}

func TestFindUsesTheNextIntegerScaleForFractionalOutputs(t *testing.T) {
	doubled := forgeDir + "/32x32@2/apps/forge-cog.png"
	checkFinds(t, forge(t), []lookupCase{
		{"forge-cog", 32, 240, doubled},
		{"forge-cog", 32, 156, doubled},
		// 16 at 1.3 → scale 2, no 16@2: the closest pixels are 32x32's.
		{"forge-cog", 16, 156, forgeDir + "/32x32/apps/forge-cog.png"},
	})
}

func TestFindFallsBackToTheClosestSize(t *testing.T) {
	relic := forgeDir + "/48x48/apps/forge-relic.png"
	checkFinds(t, forge(t), []lookupCase{
		{"forge-relic", 50, 120, relic},  // threshold
		{"forge-relic", 100, 120, relic}, // closest
		// Beyond every range, scalable's 256 is closer than any fixed size.
		{"forge-cog", 300, 120, forgeDir + "/scalable/apps/forge-cog.svg"},
	})
}

func TestFindPrefersEarlierBaseDirsAndPNGOverSVG(t *testing.T) {
	checkFinds(t, forge(t), []lookupCase{
		{"home-sigil", 32, 120, "home-icons/logis-forge/32x32/apps/home-sigil.png"},
		{"both-formats", 32, 120, forgeDir + "/32x32/apps/both-formats.png"},
	})
}

func TestFindSearchesParentsHicolorThenPixmaps(t *testing.T) {
	checkFinds(t, forge(t), []lookupCase{
		{"cogitator-skull", 32, 120, "share/icons/logis-cogitator/scalable/apps/cogitator-skull.svg"},
		{"hicolor-only", 32, 120, "share/icons/hicolor/32x32/apps/hicolor-only.png"},
		{"pixmap-only", 32, 120, "pixmaps/pixmap-only.png"},
	})
}

func TestFindMissesUnsupportedUnrelatedAndUnknownIcons(t *testing.T) {
	checkFinds(t, forge(t), []lookupCase{
		{"xpm-only", 32, 120, ""},
		{"stranger-only", 32, 120, ""},
		{"no-such-icon", 32, 120, ""},
	})
}

func TestAnUnknownOrInvalidThemeStillFindsHicolor(t *testing.T) {
	for _, theme := range []string{"no-such-theme", "../icons", "", ".", ".."} {
		themes := LoadThemes(fixturePaths(t), theme)
		if got := themes.Names(); !slices.Equal(got, []string{Hicolor}) {
			t.Errorf("%q: chain %q, want only hicolor", theme, got)
		}
		if got := find(t, themes, "forge-cog", 32, 120); got != "" {
			t.Errorf("%q: found %q", theme, got)
		}
		if got := find(t, themes, "hicolor-only", 32, 120); got == "" {
			t.Errorf("%q: hicolor-only not found", theme)
		}
	}
}

func TestNamesMayBeAbsolutePathsOrCarryAnExtension(t *testing.T) {
	themes := forge(t)
	file := filepath.Join(fixtures(t), "pixmaps/pixmap-only.png")
	if got, ok := themes.Find(file, 32, scale.One); !ok || got != file {
		t.Errorf("absolute PNG: %q %v", got, ok)
	}
	for _, missing := range []string{"pixmaps/xpm-only.xpm", "pixmaps/missing.png", "pixmaps"} {
		if got, ok := themes.Find(filepath.Join(fixtures(t), missing), 32, scale.One); ok {
			t.Errorf("absolute %s: found %q", missing, got)
		}
	}
	checkFinds(t, themes, []lookupCase{
		{"forge-cog.png", 32, 120, forgeDir + "/32x32/apps/forge-cog.png"},
		{"forge-cog.svg", 32, 120, forgeDir + "/32x32/apps/forge-cog.png"},
		{"apps/forge-cog", 32, 120, ""},
		{".hidden", 32, 120, ""},
		{"", 32, 120, ""},
	})
}
