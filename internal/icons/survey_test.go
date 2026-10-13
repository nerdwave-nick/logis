package icons

import (
	"bufio"
	"fmt"
	"io/fs"
	"maps"
	"os"
	osexec "os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// TestSurveyInstalledIcons (LOGIS_ICON_SURVEY=1) rasterises the icons
// this machine's launcher would show and every SVG in the configured
// theme chain's apps directories, read-only, and reports what renders,
// what degrades and why, and how far SVGs stray from the resvg oracle
// when it is installed. With LOGIS_RENDER_DUMP set it writes survey.tsv
// there.
func TestSurveyInstalledIcons(t *testing.T) {
	if os.Getenv("LOGIS_ICON_SURVEY") == "" {
		t.Skip("LOGIS_ICON_SURVEY not set")
	}
	cfg, err := config.NewSource("").Load()
	if err != nil {
		t.Fatal(err)
	}
	paths := FromEnv()
	themes := LoadThemes(paths, cfg.IconTheme)
	t.Logf("icon theme %q: chain %q over %q", cfg.IconTheme, themes.Names(), paths.IconDirs)
	_, lookErr := osexec.LookPath("resvg")
	oracle := lookErr == nil

	var rows []string
	tally := func(set string, files map[string]bool, pixels int) {
		var ok, degraded, failed, off int
		features := map[string]int{}
		start := time.Now()
		for _, f := range slices.Sorted(maps.Keys(files)) {
			_, report, err := rasteriseReport(f, pixels)
			row := fmt.Sprintf("%s\t%d\t%s", set, pixels, f)
			switch {
			case err != nil:
				failed++
				rows = append(rows, row+"\tfailed\t"+err.Error())
				continue
			case len(report) > 0:
				degraded++
				for _, r := range report {
					features[r]++
				}
				row += "\tdegraded\t" + strings.Join(report, ",")
			default:
				ok++
				row += "\tok\t"
			}
			if oracle && strings.HasSuffix(f, ".svg") {
				e, _, err1 := compareTruth(f, pixels)
				r, err2 := compareResvg(f, pixels)
				if err1 == nil && err2 == nil {
					row += fmt.Sprintf("\t%.1f\t%.2f\t%.1f\t%.2f", e.worst, e.mean, r.worst, r.mean)
					// Off: clearly further from the oracle than resvg's own
					// render at this size.
					if e.mean > r.mean+1 || e.worst > r.worst+32 {
						off++
						row += "\toff"
					}
				}
			}
			rows = append(rows, row)
		}
		t.Logf("%s at %d px: %d files in %v: %d render, %d degrade, %d fail; %d SVGs clearly further from the oracle than resvg (mean +1 or worst +32 levels)",
			set, pixels, len(files), time.Since(start).Round(time.Millisecond), ok, degraded, failed, off)
		for _, k := range slices.Sorted(maps.Keys(features)) {
			t.Logf("  degraded by %s: %d", k, features[k])
		}
	}

	// The launcher's icons: desktop entries' Icon= at 32 px (1.0) and 20
	// logical at the owner's 1.3 (26 px).
	names := desktopIcons(t)
	for _, size := range []struct {
		logical uint32
		s       scale.Scale
	}{{32, scale.One}, {20, scale.From120ths(156)}} {
		files := map[string]bool{}
		missing := 0
		for name := range names {
			if f, ok := themes.Find(name, size.logical, size.s); ok {
				files[f] = true
			} else {
				missing++
			}
		}
		pixels := int(size.s.Physical(scale.Size{W: size.logical, H: size.logical}).W)
		t.Logf("launcher: %d distinct Icon= names, %d not found (no PNG/SVG in the chain or pixmaps)", len(names), missing)
		tally("launcher", files, pixels)
	}

	// Every SVG in the chain's apps directories.
	svgs := map[string]bool{}
	for _, th := range themes.chain {
		for _, d := range th.dirs {
			if !strings.Contains(d.dir.Path, "apps") {
				continue
			}
			for _, dir := range d.copies {
				entries, _ := os.ReadDir(dir)
				for _, e := range entries {
					if strings.HasSuffix(e.Name(), ".svg") {
						svgs[filepath.Join(dir, e.Name())] = true
					}
				}
			}
		}
	}
	tally("theme-apps-svg", svgs, 32)

	if dir := os.Getenv("LOGIS_RENDER_DUMP"); dir != "" {
		out := "set\tpixels\tfile\tverdict\tdetail\tworst\tmean\tresvg worst\tresvg mean\n" + strings.Join(rows, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "survey.tsv"), []byte(out), 0o644); err != nil {
			t.Error(err)
		}
	}
}

// desktopIcons are the Icon= values of the desktop entries in the XDG
// data dirs' applications directories.
func desktopIcons(t *testing.T) map[string]bool {
	dirs := []string{filepath.Join(os.Getenv("HOME"), ".local/share")}
	if d := os.Getenv("XDG_DATA_DIRS"); d != "" {
		dirs = append(dirs, filepath.SplitList(d)...)
	} else {
		dirs = append(dirs, "/usr/local/share", "/usr/share")
	}
	names := map[string]bool{}
	for _, d := range dirs {
		root := filepath.Join(d, "applications")
		_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(p, ".desktop") {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer func() { _ = f.Close() }()
			inEntry := false
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if strings.HasPrefix(line, "[") {
					inEntry = line == "[Desktop Entry]"
				} else if v, ok := strings.CutPrefix(line, "Icon="); ok && inEntry && v != "" {
					names[v] = true
				}
			}
			return nil
		})
	}
	t.Logf("%d desktop entry icons under %q", len(names), dirs)
	return names
}
