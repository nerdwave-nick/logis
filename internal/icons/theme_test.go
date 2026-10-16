package icons

import (
	"reflect"
	"testing"
)

// Ported from Rust src/icons/theme.rs.

const index = `# A comment
[Icon Theme]
Name=Forge
Name[de]=Schmiede
Inherits=parent-a, parent-b,
Directories=16x16/apps,48x48/apps,scalable/apps,orphan,sizeless
ScaledDirectories=16x16@2/apps

[16x16/apps]
Size=16
Type=Fixed

[16x16@2/apps]
Size = 16
Scale = 2
Type = Fixed

[48x48/apps]
Size=48

[scalable/apps]
Size=64
MinSize=8
MaxSize=256
Type=Scalable

[sizeless]
Type=Fixed

[unlisted]
Size=22
`

func fixed(path string, size, scale uint32) themeDir {
	return themeDir{Path: path, Size: size, Scale: scale, Kind: kindFixed}
}

func scalable(path string, size, scale, lo, hi uint32) themeDir {
	return themeDir{Path: path, Size: size, Scale: scale, Kind: kindScalable, Min: lo, Max: hi}
}

func threshold(path string, size, scale, t uint32) themeDir {
	return themeDir{Path: path, Size: size, Scale: scale, Kind: kindThreshold, Threshold: t}
}

func TestParseReadsParentsAndListedDirectoriesInOrder(t *testing.T) {
	got := parseIndex(index)
	if want := []string{"parent-a", "parent-b"}; !reflect.DeepEqual(got.Inherits, want) {
		t.Errorf("inherits %q, want %q", got.Inherits, want)
	}
	want := []themeDir{
		fixed("16x16/apps", 16, 1),
		threshold("48x48/apps", 48, 1, 2),
		scalable("scalable/apps", 64, 1, 8, 256),
		fixed("16x16@2/apps", 16, 2),
	}
	if !reflect.DeepEqual(got.Dirs, want) {
		t.Errorf("dirs\n%+v, want\n%+v", got.Dirs, want)
	}
}

func TestParseDefaultsMinAndMaxToSizeAndSkipsBadNumbers(t *testing.T) {
	got := parseIndex("[Icon Theme]\nDirectories=a,b,c\n" +
		"[a]\nSize=32\nType=Scalable\n" +
		"[b]\nSize=32\nScale=two\n" +
		"[c]\nSize=24\nScale=0\nThreshold=5\n")
	want := []themeDir{scalable("a", 32, 1, 32, 32), threshold("c", 24, 1, 5)}
	if !reflect.DeepEqual(got.Dirs, want) {
		t.Errorf("dirs %+v, want %+v", got.Dirs, want)
	}
}

func TestParseWithoutAHeaderYieldsNothing(t *testing.T) {
	if got := parseIndex("[16x16]\nSize=16\n"); len(got.Dirs) != 0 || len(got.Inherits) != 0 {
		t.Errorf("%+v, want nothing", got)
	}
}

func TestMatchesFollowTypeSizeAndScale(t *testing.T) {
	f := fixed("f", 32, 1)
	s := scalable("s", 64, 2, 8, 256)
	th := threshold("t", 48, 1, 4)
	for _, tc := range []struct {
		dir         themeDir
		size, scale uint32
		want        bool
	}{
		{f, 32, 1, true}, {f, 31, 1, false}, {f, 32, 2, false},
		{s, 8, 2, true}, {s, 256, 2, true}, {s, 257, 2, false}, {s, 64, 1, false},
		{th, 44, 1, true}, {th, 52, 1, true}, {th, 43, 1, false}, {th, 53, 1, false},
	} {
		if got := tc.dir.matches(tc.size, tc.scale); got != tc.want {
			t.Errorf("%s matches(%d, %d) = %v", tc.dir.Path, tc.size, tc.scale, got)
		}
	}
}

func TestDistanceComparesPixelSizes(t *testing.T) {
	f := fixed("f", 32, 2) // 64 px
	s := scalable("s", 64, 1, 16, 128)
	th := threshold("t", 48, 1, 4) // 44..=52
	for _, tc := range []struct {
		dir               themeDir
		size, scale, want uint32
	}{
		{f, 32, 2, 0}, {f, 48, 1, 16}, {f, 40, 2, 16},
		{s, 8, 1, 8}, {s, 64, 2, 0}, {s, 100, 2, 72},
		{th, 40, 1, 4}, {th, 50, 1, 0}, {th, 32, 2, 12},
	} {
		if got := tc.dir.distance(tc.size, tc.scale); got != tc.want {
			t.Errorf("%s distance(%d, %d) = %d, want %d", tc.dir.Path, tc.size, tc.scale, got, tc.want)
		}
	}
}
