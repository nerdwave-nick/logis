package wlproto

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"
)

// floor is the oldest libwayland logis builds and runs against (README,
// "The Go build"): Ubuntu 24.04's, which CI builds on. floorInterfaces
// are the core interface tables (wl_NAME_interface) libwayland-client
// 1.22.0 exports, at the versions they describe: the interfaces of its
// protocol/wayland.xml.
const floor = "1.22.0"

var floorInterfaces = map[string]int{
	"wl_display": 1, "wl_registry": 1, "wl_callback": 1, "wl_compositor": 6,
	"wl_shm_pool": 1, "wl_shm": 1, "wl_buffer": 1, "wl_data_offer": 3,
	"wl_data_source": 3, "wl_data_device": 3, "wl_data_device_manager": 3,
	"wl_shell": 1, "wl_shell_surface": 1, "wl_surface": 6, "wl_seat": 9,
	"wl_pointer": 9, "wl_keyboard": 9, "wl_touch": 9, "wl_output": 4,
	"wl_region": 1, "wl_subcompositor": 1, "wl_subsurface": 1,
}

// The bindings need nothing of libwayland-client that the floor lacks.
// Every interface table they refer to but do not define themselves (the
// wayland-scanner glue defines the extension protocols' tables) is one
// the floor exports: a newer one, such as wl_fixes (1.24), does not
// compile against the floor's headers. And no Go wrapper of such a table
// claims a version beyond the floor's: the window binds globals up to
// Interface.Version, and the floor's libwayland-client rejects events its
// tables do not describe, which ends the connection.
func TestBindingsNeedNoLibwaylandBeyondTheFloor(t *testing.T) {
	defined := map[string]bool{}
	type ref struct {
		file    string
		version int // 0 in the C glue
	}
	refs := map[string][]ref{}

	glue, err := filepath.Glob("*.c")
	if err != nil {
		t.Fatal(err)
	}
	definition := regexp.MustCompile(`(?m)^(?:WL_PRIVATE )?const struct wl_interface (\w+)_interface = \{`)
	external := regexp.MustCompile(`(?m)^extern const struct wl_interface (\w+)_interface;`)
	for _, f := range glue {
		src := read(t, f)
		for _, m := range definition.FindAllStringSubmatch(src, -1) {
			defined[m[1]] = true
		}
		for _, m := range external.FindAllStringSubmatch(src, -1) {
			refs[m[1]] = append(refs[m[1]], ref{f, 0})
		}
	}

	wrappers, err := filepath.Glob("*_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := regexp.MustCompile(`Name: "(\w+)", Version: (\d+), c: &C\.(\w+)_interface`)
	for _, f := range wrappers {
		for _, m := range wrapper.FindAllStringSubmatch(read(t, f), -1) {
			if m[1] != m[3] {
				t.Errorf("%s: %s refers to the table of %s", f, m[1], m[3])
			}
			v, err := strconv.Atoi(m[2])
			if err != nil {
				t.Fatal(err)
			}
			refs[m[3]] = append(refs[m[3]], ref{f, v})
		}
	}
	if len(refs) == 0 {
		t.Fatal("found no interface table the bindings refer to")
	}

	for _, name := range slices.Sorted(maps.Keys(refs)) {
		if defined[name] {
			continue
		}
		most, ok := floorInterfaces[name]
		for _, r := range refs[name] {
			switch {
			case !ok:
				t.Errorf("%s refers to %s_interface, which libwayland-client %s lacks", r.file, name, floor)
			case r.version > most:
				t.Errorf("%s wraps %s at version %d; libwayland-client %s describes version %d",
					r.file, name, r.version, floor, most)
			}
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
