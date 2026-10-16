package render

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/egl"
)

// The CRT pass takes one named uniform per control of effects.Controls,
// and the regions' (v1's packed u_a and u_b are gone): each is active in
// the compiled program, so none is silently dropped.
func TestEveryControlIsANamedUniformOfTheCRTPass(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := egl.NewSurfaceless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatal(err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	defer ctx.Close()
	p, err := newCRTPass()
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	for i, k := range effects.Controls {
		if p.controls[i] < 0 {
			t.Errorf("%s.%s: no uniform %s", k.Group, k.Key, k.Uniform)
		}
	}
	for i := range MaxRegions {
		if p.region[i] < 0 || p.regionRadius[i] < 0 {
			t.Errorf("region %d has no uniform", i)
		}
	}
	if p.count < 0 || strings.Contains(crtFragment, "u_a;") || strings.Contains(crtFragment, "u_b;") {
		t.Error("the region count, or v1's packed strengths")
	}
	if !strings.HasPrefix(crtFragment, "#version 300 es\n// Original implementation for logis from a behavioural specification (clean\n// room), extending logis's post shader. MIT, the logis project.") {
		t.Error("the clean room's header")
	}
}
