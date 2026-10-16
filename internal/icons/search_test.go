package icons

import (
	"slices"
	"testing"
)

// The icon search path: the XDG data directories and pixmaps.

func TestFromVarsOrdersHomeDataHomeThenDataDirs(t *testing.T) {
	got := FromVars("/home/adept", "/home/adept/data", "/opt/share:/usr/share")
	wantIcons := []string{"/home/adept/.icons", "/home/adept/data/icons", "/opt/share/icons", "/usr/share/icons"}
	wantPixmaps := []string{"/opt/share/pixmaps", "/usr/share/pixmaps"}
	if !slices.Equal(got.IconDirs, wantIcons) {
		t.Errorf("icon dirs %q, want %q", got.IconDirs, wantIcons)
	}
	if !slices.Equal(got.PixmapDirs, wantPixmaps) {
		t.Errorf("pixmap dirs %q, want %q", got.PixmapDirs, wantPixmaps)
	}
}

func TestFromVarsUsesTheXDGDefaultsWhenUnsetOrEmpty(t *testing.T) {
	got := FromVars("/home/adept", "", "")
	wantIcons := []string{"/home/adept/.icons", "/home/adept/.local/share/icons", "/usr/local/share/icons", "/usr/share/icons"}
	wantPixmaps := []string{"/usr/local/share/pixmaps", "/usr/share/pixmaps"}
	if !slices.Equal(got.IconDirs, wantIcons) {
		t.Errorf("icon dirs %q, want %q", got.IconDirs, wantIcons)
	}
	if !slices.Equal(got.PixmapDirs, wantPixmaps) {
		t.Errorf("pixmap dirs %q, want %q", got.PixmapDirs, wantPixmaps)
	}
}

func TestFromVarsIgnoresRelativePathsAndAMissingHome(t *testing.T) {
	got := FromVars("", "relative", "also/relative:/srv/share")
	if want := []string{"/srv/share/icons"}; !slices.Equal(got.IconDirs, want) {
		t.Errorf("icon dirs %q, want %q", got.IconDirs, want)
	}
	if want := []string{"/srv/share/pixmaps", "/usr/share/pixmaps"}; !slices.Equal(got.PixmapDirs, want) {
		t.Errorf("pixmap dirs %q, want %q", got.PixmapDirs, want)
	}
}

func TestFromEnvReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("HOME", "/home/adept")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_DATA_DIRS", "/srv/share")
	got := FromEnv()
	want := []string{"/home/adept/.icons", "/home/adept/.local/share/icons", "/srv/share/icons"}
	if !slices.Equal(got.IconDirs, want) {
		t.Errorf("icon dirs %q, want %q", got.IconDirs, want)
	}
}
